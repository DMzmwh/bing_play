package torrentmgr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/i18n"
	"github.com/bing/bing-play/internal/logger"
	"github.com/bing/bing-play/internal/torrentmgr/models/vo"
	"github.com/bing/bing-play/internal/utils/ip"

	"github.com/anacrolix/dht/v2"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"go.uber.org/fx"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

// 下载管理器
type TorrentManager struct {
	//读写分离锁 读写分离锁，读读不互斥
	mu          sync.RWMutex
	jobsMap     map[string]*TorrentJob
	client      *torrent.Client
	ResourceDao *dao.ResourceDAO
	cfg         *config.Config
	sem         *semaphore.Weighted
	downloadCfg DownloadConfig

	// 下载目录可由系统设置运行时修改，读写需加锁；仅对之后新建的任务生效
	downloadMu  sync.RWMutex
	downloadDir string

	// 同一目录内复用同一个分片完成记录句柄。
	// CGO 关闭时底层使用 BoltDB，重复打开同一文件会因文件锁返回 timeout，
	// 若每次任务都重新打开，后续任务会加入失败并被误判为网络异常。
	piecesMu sync.Mutex
	pieces   map[string]storage.PieceCompletion

	// closed 标记管理器已进入关闭流程。
	// 关闭后不再调度新任务，Shutdown 也可以被重复调用（Wails 与 Fx 都会触发）。
	closed atomic.Bool
}

// 命名规范：{构造名}Params
type WailsAppParams struct {
	fx.In // 必须内嵌 fx.In，告诉fx这是参数结构体

	DB          *gorm.DB
	I18n        *i18n.I18nService
	ResourceDao *dao.ResourceDAO
	Cfg         *config.Config
}

func NewManager(lc fx.Lifecycle, cn WailsAppParams) (*TorrentManager, error) {
	// 1. 基础目录（必须）
	c := torrent.NewDefaultClientConfig()
	//打开全量调试日志，会打印 DHT、PEX、Tracker、Peer 连接、协议交互详情。排查网络 / 连不上 peer 的时候打开，生产务必关闭，日志量巨大。
	//c.Debug = true
	c.DataDir = cn.Cfg.DownloadDir
	//c.DefaultStorage = storage.NewFile(c.DataDir)
	// 2. 开启 DHT（非常关键）
	c.NoDHT = false
	// 3. 开启PEX  Peer Exchange（节点交换协议）
	c.DisablePEX = false
	// 4. 开启 uTP + TCP（默认一般是开的，但建议显式确认）
	c.DisableUTP = false
	// 5. 开启 tracker 有些种子元信息里面记录的服务器，向 torrent 文件里面的 tracker 发起请求获取 peer。
	//`true`：完全不访问任何 tracker，只靠 DHT+PEX 找节点。
	c.DisableTrackers = false
	//Tracker 分 HTTP、UDP Tracker。
	//c.TrackerListenPacket = func(network, addr string) (net.PacketConn, error) {
	//	return net.ListenPacket("udp4", addr)
	//}
	//透传 network，支持 udp4 / udp6 tracker
	c.TrackerListenPacket = func(network, addr string) (net.PacketConn, error) {
		return net.ListenPacket(network, addr)
	}
	// 6. 强制监听端口（避免随机导致被防火墙拦）
	c.ListenPort = 51413
	c.ListenHost = func(network string) string {
		switch network {
		case "tcp4", "udp4":
			return "0.0.0.0"

		case "tcp6", "udp6":
			return "::"

		default:
			return ""
		}
	}
	// 7. 允许 IPv4 + IPv6（有些网络 IPv6 会坑）
	c.DisableIPv6 = ip.ShouldDisableIPv6()
	// 8. 每个种子已经完成三次握手的活跃 Peer 最大数量 连接数（提高成功率）
	c.EstablishedConnsPerTorrent = 150
	//**半开连接数**：正在进行 TCP 握手尚未完成连接的并发数量。
	c.HalfOpenConnsPerTorrent = 50
	// 9. 种子启动时不要太“保守”
	//是否作为做种模式启动 false下载模式 true只上传（做种）
	//`Seed = true`：**纯做种模式，不会下载任何数据，只对外上传**。
	//`Seed = false`：普通模式，下载，下载完成后继续做种上传。
	// 注意：Seed=true 不会阻止写入磁盘，但不会请求块。如果你需要下载文件，必须保持 false
	c.Seed = false
	// 10. 可选：如果网络在国内，建议关掉一些奇怪限制
	c.DisableAcceptRateLimiting = true

	// 11. 放宽内部吞吐限制。默认值偏保守，高带宽 / 热门资源时会先撞到这些上限，
	// 表现为 peer 很多但速度上不去，或连上后长时间等在哈希校验。
	// 全局半开连接数上限（默认 100），多个任务并发时不够用
	c.TotalHalfOpenConns = 400
	// 每个种子保留的候选 peer 地址上限（默认 500），热门资源可多留候选
	c.TorrentPeersHighWater = 1500
	// 每秒新建 peer 连接数（默认 10/s），peer 多时建连速度会成为瓶颈
	c.DialRateLimiter = rate.NewLimiter(50, 50)
	// 每个种子的哈希校验协程数（默认 2），CPU 富余时可减少“下载等校验”
	c.PieceHashersPerTorrent = 4
	// 全客户端未校验数据上限（默认 64MiB），提高后高带宽下不易被校验阻塞
	c.MaxUnverifiedBytes = 256 << 20

	// 12. 减少高延迟链路下的无效传输。默认 0 允许请求在同一个 RTT 内被反复抢走，
	// 每次抢占都会产生 Cancel 与重复分片；保留 1 秒让持有者完成一次尝试。
	c.StealRequestGrace = time.Second
	// 同一 Peer ID 的多个地址只保留一条连接，避免占用有限的连接名额。
	c.DropDuplicatePeerIds = true

	fmt.Println("goos:", c.DataDir)
	c.DhtStartingNodes = func(network string) dht.StartingNodesGetter {
		return func() ([]dht.Addr, error) {
			global := dht.DefaultGlobalBootstrapHostPorts
			return dht.ResolveHostPorts(global)
		}
	}

	// 每个任务都会通过 spec.Storage 显式指定存储，客户端默认存储不会被使用。
	// 这里给默认存储使用内存分片记录，避免 NewClient 启动时抢先打开下载目录的
	// BoltDB 文件锁，导致后续任务打开同目录的共享分片记录时返回 timeout。
	c.DefaultStorage = storage.NewFileWithCompletion(c.DataDir, storage.NewMapPieceCompletion())

	client, err := torrent.NewClient(c)
	if err != nil {
		panic(err)
	}

	downloadCfg := DefaultDownloadConfig()
	manager := &TorrentManager{
		jobsMap:     make(map[string]*TorrentJob),
		client:      client,
		ResourceDao: cn.ResourceDao,
		cfg:         cn.Cfg,
		sem:         semaphore.NewWeighted(int64(downloadCfg.MaxConcurrent)),
		downloadCfg: downloadCfg,
		downloadDir: cn.Cfg.DownloadDir,
		pieces:      make(map[string]storage.PieceCompletion),
	}

	// 启动时按状态机恢复规则恢复未完成任务。
	// 退出时由 Fx 兜底关闭下载引擎；Wails 的 ServiceShutdown 会先调用一次，
	// Shutdown 内部做了幂等处理，不会重复释放。
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				// 先把历史「已取消」任务归一化为未下载，再恢复异常中断的任务。
				manager.normalizeCancelledTasks()
				manager.RecoverPendingJobs()
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			return manager.Shutdown()
		},
	})

	return manager, nil
}

// GetDownloadConfig 返回当前下载参数。
func (m *TorrentManager) GetDownloadConfig() DownloadConfig {
	return m.downloadCfg
}

// SetDownloadDir 更新运行时下载目录，后续新建任务使用新目录，已有任务不迁移。
func (m *TorrentManager) SetDownloadDir(dir string) {
	m.downloadMu.Lock()
	m.downloadDir = dir
	m.downloadMu.Unlock()
}

// currentDownloadDir 返回当前生效的下载目录。
func (m *TorrentManager) currentDownloadDir() string {
	m.downloadMu.RLock()
	defer m.downloadMu.RUnlock()
	return m.downloadDir
}

// Shutdown 关闭下载引擎并释放所有任务占用的资源。
//
// Wails 的 ServiceShutdown 与 Fx 的 OnStop 都会调用这里，
// 通过 closed 标记保证真正的释放流程只执行一次。
func (m *TorrentManager) Shutdown() error {
	if !m.closed.CompareAndSwap(false, true) {
		return nil
	}

	m.mu.RLock()
	jobs := make([]*TorrentJob, 0, len(m.jobsMap))
	for _, job := range m.jobsMap {
		jobs = append(jobs, job)
	}
	m.mu.RUnlock()

	for _, job := range jobs {
		job.shutdown()
	}

	if m.client != nil {
		m.client.Close()
	}

	m.piecesMu.Lock()
	pieces := m.pieces
	m.pieces = make(map[string]storage.PieceCompletion)
	m.piecesMu.Unlock()
	for _, pc := range pieces {
		_ = pc.Close()
	}
	return nil
}

// GetJob 按 InfoHash 获取内存中的任务。
func (m *TorrentManager) GetJob(infoHash string) (*TorrentJob, error) {
	m.mu.RLock()
	job := m.jobsMap[infoHash]
	m.mu.RUnlock()
	if job == nil {
		return nil, errors.New(ErrKeyTaskNotFound)
	}
	return job, nil
}

// GetJobVO 获取单个任务的展示快照。
func (m *TorrentManager) GetJobVO(infoHash string) (*vo.TorrentJobVO, error) {
	if job, err := m.GetJob(infoHash); err == nil {
		return job.ToVO(), nil
	}

	resource, err := m.ResourceDao.GetByInfoHash(infoHash)
	if err != nil {
		return nil, err
	}
	if resource == nil {
		return nil, errors.New(ErrKeyTaskNotFound)
	}
	return voFromResource(resource), nil
}

// ListJobs 返回全部下载任务快照。
// 以数据库记录为准合并内存运行时数据，应用重启后已完成 / 暂停的任务同样可见。
func (m *TorrentManager) ListJobs() []*vo.TorrentJobVO {
	rows, err := m.ResourceDao.ListDownloadTasks()
	if err != nil {
		logger.Torrent.Error("查询下载任务失败", slog.Any("err", err))
		rows = nil
	}

	m.mu.RLock()
	inMemory := make(map[string]*TorrentJob, len(m.jobsMap))
	for hash, job := range m.jobsMap {
		inMemory[hash] = job
	}
	m.mu.RUnlock()

	result := make([]*vo.TorrentJobVO, 0, len(rows)+len(inMemory))
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		hash := rows[i].InfoHash
		seen[hash] = struct{}{}
		if job := inMemory[hash]; job != nil {
			result = append(result, job.ToVO())
			continue
		}
		result = append(result, voFromResource(&rows[i]))
	}
	for hash, job := range inMemory {
		if _, ok := seen[hash]; ok {
			continue
		}
		result = append(result, job.ToVO())
	}

	sortDownloadVOs(result)
	return result
}

// sortDownloadVOs 下载任务排序：状态权重 → 优先级 → 创建时间。
func sortDownloadVOs(result []*vo.TorrentJobVO) {
	sort.SliceStable(result, func(i, j int) bool {
		left, right := taskStateRank(result[i].TaskState), taskStateRank(result[j].TaskState)
		if left != right {
			return left < right
		}
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].CreatedTime > result[j].CreatedTime
	})
}

// ListActiveJobs 返回下载页「待下载」列表快照。
//
// 数据库是待下载队列的唯一事实源：内存中有运行时数据的任务使用实时快照，
// 其余未完成任务（重启后自动恢复中的任务、等待重试的任务、暂停 / 失败的任务）
// 从数据库补齐，保证用户触发过的任务只要未完成就不会从列表中消失。
// 取消的任务会恢复为未下载状态，不属于待下载队列。
func (m *TorrentManager) ListActiveJobs() []*vo.TorrentJobVO {
	m.mu.RLock()
	jobs := make([]*TorrentJob, 0, len(m.jobsMap))
	inMemory := make(map[string]struct{}, len(m.jobsMap))
	for hash, job := range m.jobsMap {
		jobs = append(jobs, job)
		inMemory[hash] = struct{}{}
	}
	m.mu.RUnlock()

	result := make([]*vo.TorrentJobVO, 0, len(jobs))
	for _, job := range jobs {
		payload := job.ToVO()
		// 已完成任务统一从数据库查询，避免与完成记录重复展示
		if payload.TaskState == models.TaskCompleted.String() {
			continue
		}
		result = append(result, payload)
	}

	pendingRows, err := m.ResourceDao.ListPendingTasks()
	if err != nil {
		logger.Torrent.Error("查询待下载任务失败", slog.Any("err", err))
	} else {
		for i := range pendingRows {
			if _, ok := inMemory[pendingRows[i].InfoHash]; ok {
				continue
			}
			result = append(result, voFromResource(&pendingRows[i]))
		}
	}

	sortDownloadVOs(result)
	return result
}

// ListCompletedJobs 从数据库读取已完成任务，仅在打开下载页时调用一次。
func (m *TorrentManager) ListCompletedJobs() ([]*vo.TorrentJobVO, error) {
	rows, err := m.ResourceDao.ListCompletedTasks()
	if err != nil {
		return nil, err
	}

	result := make([]*vo.TorrentJobVO, 0, len(rows))
	for i := range rows {
		result = append(result, voFromResource(&rows[i]))
	}

	sortDownloadVOs(result)
	return result, nil
}

// taskStateRank 下载页排序权重：进行中的任务靠前，已完成 / 取消靠后。
func taskStateRank(state string) int {
	switch state {
	case models.TaskDownloading.String():
		return 0
	case models.TaskVerifying.String():
		return 1
	case models.TaskPreparing.String():
		return 2
	case models.TaskQueued.String():
		return 3
	case models.TaskRetryWait.String():
		return 4
	case models.TaskPaused.String():
		return 5
	case models.TaskFailed.String():
		return 6
	case models.TaskCompleted.String():
		return 7
	case models.TaskCancelled.String():
		return 8
	default:
		return 9
	}
}

// voFromResource 把数据库记录转换成任务快照（无内存运行时数据时使用）。
func voFromResource(resource *models.Resource) *vo.TorrentJobVO {
	payload := &vo.TorrentJobVO{
		InfoHash:     resource.InfoHash,
		TaskState:    resource.TaskState.String(),
		LocalState:   resource.LocalState.String(),
		DisplayState: resource.Display().String(),
		SavePath:     resource.LocalPath,
		Attempt:      int(resource.Attempt),
		MaxAttempts:  int(resource.MaxAttempts),
		ErrorCode:    resource.ErrorCode,
		EtaSeconds:   -1,
	}
	if resource.ResourceName != nil {
		payload.Name = *resource.ResourceName
	}
	if resource.FileName != nil {
		payload.FileName = *resource.FileName
	}
	if resource.TotalSize != nil {
		payload.TotalSize = *resource.TotalSize
	}
	if resource.DownloadedSize != nil {
		payload.Downloaded = *resource.DownloadedSize
	}
	if resource.Priority != nil {
		payload.Priority = *resource.Priority
	}
	if resource.CreateTime != nil {
		payload.CreatedTime = resource.CreateTime.Format(time.DateTime)
	}
	if resource.VerifiedAt != nil {
		payload.VerifiedAt = resource.VerifiedAt.Format(time.DateTime)
	}
	if resource.NextRetryAt != nil {
		payload.NextRetryAt = resource.NextRetryAt.Format(time.DateTime)
	}
	if payload.TotalSize > 0 {
		payload.Progress = float64(payload.Downloaded) / float64(payload.TotalSize) * 100
		if payload.Progress > 100 {
			payload.Progress = 100
		}
	}
	if resource.TaskState == models.TaskCompleted {
		payload.Progress = 100
		payload.EtaSeconds = 0
	}
	return payload
}

// PauseJob 暂停任务。
func (m *TorrentManager) PauseJob(infoHash string) error {
	if job, err := m.GetJob(infoHash); err == nil {
		return job.Pause()
	}
	return m.applyResourceEvent(infoHash, models.EvPause, models.EventPayload{Now: time.Now()})
}

// ResumeJob 继续暂停的任务；内存中没有任务时从数据库重新入队。
func (m *TorrentManager) ResumeJob(infoHash string) error {
	job, err := m.GetJob(infoHash)
	if err == nil {
		return m.resumeJob(job)
	}

	resource, dbErr := m.ResourceDao.GetByInfoHash(infoHash)
	if dbErr != nil {
		return dbErr
	}
	if resource == nil {
		return errors.New(ErrKeyTaskNotFound)
	}
	_, _, err = m.enqueueResource(resource, 0, models.EvResume)
	return err
}

// RetryJob 重试失败 / 取消的任务，重新拉取元信息。
func (m *TorrentManager) RetryJob(infoHash string) error {
	job, err := m.GetJob(infoHash)
	if err == nil {
		return m.retryJob(job, 0)
	}

	resource, dbErr := m.ResourceDao.GetByInfoHash(infoHash)
	if dbErr != nil {
		return dbErr
	}
	if resource == nil {
		return errors.New(ErrKeyTaskNotFound)
	}
	_, _, err = m.enqueueResource(resource, 0, 0)
	return err
}

// resumeJob 根据任务当前状态继续下载。
func (m *TorrentManager) resumeJob(job *TorrentJob) error {
	res, err := job.Apply(models.EvResume, models.EventPayload{
		Now:           time.Now(),
		MetadataReady: job.metadataReady(),
	})
	if err != nil {
		return err
	}

	switch res.Task {
	case models.TaskQueued:
		job.resetContext()
		go m.schedule()
	case models.TaskPreparing:
		job.resetContext()
		if tt := job.getTorrent(); tt != nil {
			tt.Drop()
		}
		if err := m.restartTorrent(job); err != nil {
			m.failOrRetry(job, classifyTorrentError(err))
			return nil
		}
		go m.waitMetadata(job)
	}
	return nil
}

// retryJob 按失败 / 取消状态执行手动重试或重新下载。
func (m *TorrentManager) retryJob(job *TorrentJob, priority int) error {
	state := job.currentState()
	var e models.Event
	switch state {
	case models.TaskFailed:
		e = models.EvManualRetry
	case models.TaskCancelled:
		e = models.EvRedownload
	case models.TaskCompleted:
		return errors.New(ErrKeyRepairNotNeeded)
	default:
		return nil
	}

	res, err := job.Apply(e, models.EventPayload{
		Now:             time.Now(),
		SourceAvailable: job.sourceAvailable(),
	})
	if err != nil {
		return err
	}
	if res.Task != models.TaskPreparing {
		return nil
	}

	job.resetContext()
	if tt := job.getTorrent(); tt != nil {
		tt.Drop()
	}
	if err := m.restartTorrent(job); err != nil {
		m.failOrRetry(job, classifyTorrentError(err))
		return nil
	}
	go m.waitMetadata(job)
	return nil
}

// redownloadCompletedJob 让内存中的已完成任务重新下载本地缺失的数据。
//
// 返回 true 表示确实发起了重新下载；本地数据完整或任务不处于已完成状态时返回 false。
func (m *TorrentManager) redownloadCompletedJob(job *TorrentJob) (bool, error) {
	if job == nil || job.currentState() != models.TaskCompleted {
		return false, nil
	}
	job.mu.RLock()
	resource := job.job
	job.mu.RUnlock()
	if resource == nil || !localDataMissing(resource) {
		return false, nil
	}

	now := time.Now()
	job.mu.RLock()
	localState := resource.LocalState
	job.mu.RUnlock()
	if localState == models.LocalPresent {
		// 数据库仍认为是完整文件，先记录为丢失，满足 EvRepair 的守卫条件。
		if _, err := job.Apply(models.EvFileLost, models.EventPayload{
			Now:        now,
			LossReason: models.LossNotFound,
		}); err != nil {
			return false, err
		}
	}

	res, err := job.Apply(models.EvRepair, models.EventPayload{
		Now:             now,
		SourceAvailable: job.sourceAvailable(),
	})
	if err != nil {
		return false, err
	}
	if res.Task != models.TaskPreparing {
		return false, nil
	}

	job.resetContext()
	if tt := job.getTorrent(); tt != nil {
		tt.Drop()
	}
	if err := m.restartTorrent(job); err != nil {
		m.failOrRetry(job, classifyTorrentError(err))
		return true, nil
	}
	go m.waitMetadata(job)
	return true, nil
}

// RemoveFromPlaylist 从播放列表批量移除资源。
//
// 先一次查询全部资源，再按来源分类处理：本地视频批量删除数据库记录；种子资源
// 批量重置为「未下载」。内存中仍有下载任务的种子需要逐个停止，磁盘文件均保留。
func (m *TorrentManager) RemoveFromPlaylist(ids []int64) error {
	ids = uniqueResourceIDs(ids)
	if len(ids) == 0 {
		return nil
	}

	rows, err := m.ResourceDao.GetLocalFilesByIDs(ids)
	if err != nil || len(rows) == 0 {
		return err
	}

	now := time.Now()
	localIDs := make([]int64, 0, len(rows))
	resetHashes := make([]string, 0, len(rows))
	resetSet := make(map[string]struct{}, len(rows))
	var stopped bool
	var firstErr error

	for i := range rows {
		resource := &rows[i]
		if resource.Kind == models.KindLocal {
			localIDs = append(localIDs, resource.ID)
			continue
		}

		// 内存中仍有任务时逐个停止：取消上下文并释放 Torrent 句柄。
		m.mu.RLock()
		job := m.jobsMap[resource.InfoHash]
		m.mu.RUnlock()
		if job != nil {
			if _, err := job.Apply(models.EvRemovePlaylist, models.EventPayload{Now: now}); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			m.mu.Lock()
			if m.jobsMap[resource.InfoHash] == job {
				delete(m.jobsMap, resource.InfoHash)
			}
			m.mu.Unlock()
			stopped = true
			continue
		}

		resetHashes = append(resetHashes, resource.InfoHash)
		resetSet[resource.InfoHash] = struct{}{}
	}

	if len(localIDs) > 0 {
		if err := m.ResourceDao.DeleteLocalFilesByIDs(localIDs); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if len(resetHashes) > 0 {
		if err := m.ResourceDao.ResetToUndownloadedByHashes(resetHashes, now); err != nil && firstErr == nil {
			firstErr = err
		}
		// 批量更新完成后，用内存快照应用同一事件生成推送数据，不再逐条落库。
		for i := range rows {
			resource := &rows[i]
			if _, ok := resetSet[resource.InfoHash]; !ok {
				continue
			}
			if _, err := resource.ApplyEvent(models.EvRemovePlaylist, models.EventPayload{Now: now}); err != nil {
				continue
			}
			resource.UpdateTime = &now
			emitDownloadEvent(EventDownloadStateChanged, voFromResource(resource))
		}
	}

	if stopped {
		go m.schedule()
	}
	return firstErr
}

// uniqueResourceIDs 过滤非法 ID 并去重，避免批量查询与更新时重复处理。
func uniqueResourceIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// CancelJob 取消任务：停止下载、释放 Torrent、清理磁盘痕迹、删除数据库记录。
// 只删 download_tasks + local_files，resources 原始爬虫目录保留不动。
func (m *TorrentManager) CancelJob(infoHash string) error {
	job, err := m.GetJob(infoHash)
	if err != nil {
		return m.cancelResourceByHash(infoHash)
	}

	// 先取出清理磁盘所需的元信息与下载路径，再停下载、删记录。
	var (
		info     *metainfo.Info
		baseDir  string
		fileName string
	)
	job.mu.Lock()
	if job.job != nil {
		baseDir = job.job.LocalPath
		if job.job.FileName != nil {
			fileName = *job.job.FileName
		}
	}
	job.cancel() // 停止下载上下文
	tt := job.torrent
	job.torrent = nil
	job.mu.Unlock()

	if tt != nil {
		info = tt.Info()
		tt.Drop()
	}
	if info == nil {
		info = readDownloadInfo(baseDir, infoHash)
	}

	m.mu.Lock()
	delete(m.jobsMap, infoHash)
	m.mu.Unlock()

	// 1. 清磁盘下载痕迹
	m.cleanDownload(baseDir, infoHash, info, fileName)
	// 2. 清分片完成记录
	var numPieces int
	if info != nil {
		numPieces = info.NumPieces()
	}
	m.clearPieceRecords(baseDir, infoHash, numPieces)
	// 3. 删数据库记录（download_tasks + local_files，resources 保留）
	if err := m.ResourceDao.DeleteTaskByHash(infoHash); err != nil {
		logger.Torrent.Warn("删除下载任务记录失败", slog.String("info_hash", infoHash), slog.Any("err", err))
	}

	emitDownloadEvent(EventDownloadStateChanged, &vo.TorrentJobVO{InfoHash: infoHash})
	go m.schedule()
	return nil
}

// cancelResourceByHash 对内存中不存在的任务执行取消。
func (m *TorrentManager) cancelResourceByHash(infoHash string) error {
	resource, err := m.ResourceDao.GetByInfoHash(infoHash)
	if err != nil {
		return err
	}
	if resource == nil {
		return errors.New(ErrKeyTaskNotFound)
	}
	return m.cancelResource(resource)
}

// cancelResource 把数据库记录（download_tasks + local_files）真实删除，清理磁盘下载痕迹。
func (m *TorrentManager) cancelResource(resource *models.Resource) error {
	if resource == nil || resource.Kind != models.KindTorrent {
		return errors.New(ErrKeyTaskNotFound)
	}

	baseDir := resource.LocalPath
	if baseDir == "" {
		baseDir = m.currentDownloadDir()
	}
	var fileName string
	if resource.FileName != nil {
		fileName = *resource.FileName
	}

	// 清磁盘下载痕迹
	m.cleanDownload(baseDir, resource.InfoHash, readDownloadInfo(baseDir, resource.InfoHash), fileName)

	// 删数据库记录（resources 保留不动）
	if err := m.ResourceDao.DeleteTaskByHash(resource.InfoHash); err != nil {
		return err
	}

	emitDownloadEvent(EventDownloadStateChanged, &vo.TorrentJobVO{InfoHash: resource.InfoHash})
	return nil
}

// normalizeCancelledTasks 把历史遗留的「已取消」任务恢复为未下载状态并清理磁盘痕迹。
//
// 新流程取消后不再停留在 Cancelled 状态，升级时需要对旧数据做一次归一化。
func (m *TorrentManager) normalizeCancelledTasks() {
	if m.closed.Load() {
		return
	}
	rows, err := m.ResourceDao.ListByTaskStates([]models.TaskState{models.TaskCancelled})
	if err != nil {
		logger.Torrent.Warn("归一化已取消任务失败", slog.Any("err", err))
		return
	}
	for i := range rows {
		if err := m.cancelResource(&rows[i]); err != nil {
			logger.Torrent.Warn("归一化已取消任务失败",
				slog.String("info_hash", rows[i].InfoHash),
				slog.Any("err", err),
			)
		}
	}
	if len(rows) > 0 {
		logger.Torrent.Info("已归一化取消任务", slog.Int("count", len(rows)))
	}
}

// cleanDownload 清理任务在磁盘上的下载痕迹：数据文件、元信息缓存与分片完成记录。
func (m *TorrentManager) cleanDownload(baseDir, infoHash string, info *metainfo.Info, fileName string) {
	removeDownloadFiles(baseDir, infoHash, info, fileName)
	if info != nil {
		m.clearPieceRecords(baseDir, infoHash, info.NumPieces())
	}
}

// clearPieceRecords 清除任务的分片完成记录。
// 数据文件删除后必须同步清除完成标记，否则重新下载时会沿用过期的完成记录。
func (m *TorrentManager) clearPieceRecords(baseDir, infoHash string, numPieces int) {
	if baseDir == "" || infoHash == "" || numPieces <= 0 {
		return
	}
	pc, err := m.pieceCompletionForDir(baseDir)
	if err != nil {
		logger.Torrent.Warn("打开分片完成记录失败", slog.String("info_hash", infoHash), slog.Any("err", err))
		return
	}

	hash := metainfo.NewHashFromHex(infoHash)
	for i := 0; i < numPieces; i++ {
		key := metainfo.PieceKey{InfoHash: hash, Index: i}
		if err := pc.Set(key, g.None[bool]()); err != nil {
			logger.Torrent.Warn("清理分片完成记录失败",
				slog.String("info_hash", infoHash),
				slog.Int("piece", i),
				slog.Any("err", err),
			)
			return
		}
	}
}

// removeDownloadFiles 删除任务的数据文件与元信息缓存，并清理因此变空的子目录。
func removeDownloadFiles(baseDir, infoHash string, info *metainfo.Info, fileName string) {
	if baseDir == "" {
		return
	}
	base := filepath.Clean(baseDir)

	if info != nil {
		for _, file := range info.UpvertedFiles() {
			path := torrentFilePath(base, info, file)
			// 异常路径（含 .. / 空路径）一律跳过，绝不删除下载根目录之外的文件。
			if path == base || !pathWithin(base, path) {
				continue
			}
			removeTorrentDataFile(path)
			removeEmptyDirs(filepath.Dir(path), base)
		}
	} else if fileName != "" {
		path := filepath.Join(base, fileName)
		if pathWithin(base, path) {
			removeTorrentDataFile(path)
			removeEmptyDirs(filepath.Dir(path), base)
		}
	}

	if cache := metadataCachePath(base, infoHash); cache != "" {
		removeFile(cache)
	}
	// .meta 目录只有在为空时才会被删除，不影响其他任务的元信息缓存。
	_ = os.Remove(filepath.Join(base, metadataCacheDir))
}

// torrentFilePath 返回种子内单个文件的实际落盘路径。
//
// torrent 引擎把所有文件都放在 Info.Name 之下：单文件种子的 Name 即文件名，
// 多文件种子的 FileInfo.Path 不含根目录名，因此统一拼接 Info.BestName()。
func torrentFilePath(base string, info *metainfo.Info, file metainfo.FileInfo) string {
	parts := append([]string{info.BestName()}, file.BestPath()...)
	return filepath.Join(append([]string{base}, parts...)...)
}

// localDataMissing 判断已完成任务的本地数据是否仍与种子一致。
//
// 优先用下载目录的元信息缓存枚举全部文件（覆盖多文件种子），缓存缺失时退化为主文件检查，
// 避免文件被外部删除后仍被当作「已完成」而跳过下载。
func localDataMissing(resource *models.Resource) bool {
	if resource == nil {
		return true
	}
	base := resource.LocalPath
	if base == "" {
		return true
	}
	if info := readDownloadInfo(base, resource.InfoHash); info != nil {
		base = filepath.Clean(base)
		for _, file := range info.UpvertedFiles() {
			// 跳过零长度文件，避免把不落盘的占位文件误判为缺失。
			if file.Length <= 0 {
				continue
			}
			path := torrentFilePath(base, info, file)
			stat, err := os.Stat(path)
			if err != nil || stat.IsDir() || stat.Size() != file.Length {
				return true
			}
		}
		return false
	}
	if resource.FileName == nil || strings.TrimSpace(*resource.FileName) == "" {
		// 没有可校验的路径信息时不主动重建任务。
		return false
	}
	path := filepath.Join(base, *resource.FileName)
	stat, err := os.Stat(path)
	if err != nil || stat.IsDir() {
		return true
	}
	return resource.FileSize != nil && stat.Size() != *resource.FileSize
}

// torrentPartSuffix 分片文件模式下未完成数据使用的临时后缀。
// anacrolix 的文件存储默认开启 part files，下载中的数据写在 <文件>.part，
// 取消时必须连同正式文件一起删除，否则会残留已下载分片并在下次下载时被复用。
const torrentPartSuffix = ".part"

// removeTorrentDataFile 删除种子数据文件及其 part 临时文件。
func removeTorrentDataFile(path string) {
	removeFile(path)
	removeFile(path + torrentPartSuffix)
}

// removeFile 删除文件，文件不存在时忽略。
func removeFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.Torrent.Warn("清理下载文件失败", slog.String("path", path), slog.Any("err", err))
	}
}

// removeEmptyDirs 自 dir 起逐级向上删除空目录，遇到 base 或非空目录时停止。
func removeEmptyDirs(dir, base string) {
	dir = filepath.Clean(dir)
	for dir != base && pathWithin(base, dir) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// pathWithin 判断 path 是否位于 base 目录内，防止种子中的异常路径删除目录外文件。
func pathWithin(base, path string) bool {
	rel, err := filepath.Rel(base, filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readDownloadInfo 从下载目录的元信息缓存解析文件列表，缓存缺失或损坏时返回 nil。
func readDownloadInfo(baseDir, infoHash string) *metainfo.Info {
	path := metadataCachePath(baseDir, infoHash)
	if path == "" {
		return nil
	}
	mi, err := metainfo.LoadFromFile(path)
	if err != nil {
		return nil
	}
	// 只有缓存与目标 InfoHash 一致时才使用，避免清理到其他任务的文件。
	if mi.HashInfoBytes().HexString() != strings.ToLower(strings.TrimSpace(infoHash)) {
		return nil
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil
	}
	return &info
}

// SetPriority 调整任务优先级，数值越大越先下载。
func (m *TorrentManager) SetPriority(infoHash string, priority int) error {
	job, err := m.GetJob(infoHash)
	if err == nil {
		if job.forcePriority(priority) {
			if hash := job.infoHash(); hash != "" {
				if err := m.ResourceDao.UpdatePriority(hash, priority); err != nil {
					logger.Torrent.Error("更新任务优先级失败", slog.String("info_hash", hash), slog.Any("err", err))
				}
			}
			m.emitState(job)
			go m.schedule()
		}
		return nil
	}

	resource, dbErr := m.ResourceDao.GetByInfoHash(infoHash)
	if dbErr != nil {
		return dbErr
	}
	if resource == nil {
		return errors.New(ErrKeyTaskNotFound)
	}
	return m.ResourceDao.UpdatePriority(infoHash, priority)
}

// applyResourceEvent 对内存中不存在的任务直接执行状态机流转并落库。
func (m *TorrentManager) applyResourceEvent(infoHash string, e models.Event, p models.EventPayload) error {
	resource, err := m.ResourceDao.GetByInfoHash(infoHash)
	if err != nil {
		return err
	}
	if resource == nil {
		return errors.New(ErrKeyTaskNotFound)
	}
	if p.Now.IsZero() {
		p.Now = time.Now()
	}
	if !p.SourceAvailable {
		p.SourceAvailable = sourceAvailable(resource)
	}

	fromVersion := resource.StateVersion
	res, err := resource.ApplyEvent(e, p)
	if err != nil {
		return err
	}
	if res.VersionDelta == 0 {
		return nil
	}

	now := p.Now
	resource.UpdateTime = &now
	if err := m.persistState(resource, fromVersion); err != nil {
		return err
	}
	emitDownloadEvent(EventDownloadStateChanged, voFromResource(resource))
	return nil
}

// statePersistAttempts 状态写入的 CAS 冲突重试次数。
const statePersistAttempts = 3

// persistState 以 CAS 方式写回任务的完整状态，冲突时基于数据库最新版本重试。
//
// 状态迁移（入队、恢复、失败、取消）必须落库成功，避免异常场景下内存与数据库
// 版本不一致导致任务被误判为未触发下载而从待下载列表丢失。
func (m *TorrentManager) persistState(resource *models.Resource, fromVersion uint64) error {
	if resource == nil || resource.InfoHash == "" {
		return nil
	}

	version := fromVersion
	for attempt := 0; attempt < statePersistAttempts; attempt++ {
		ok, err := m.ResourceDao.UpdateState(resource, version)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		latest, err := m.ResourceDao.GetByInfoHash(resource.InfoHash)
		if err != nil {
			return err
		}
		if latest == nil {
			return errors.New(ErrKeyTaskNotFound)
		}
		// 保留本次要写入的目标状态，仅同步到数据库最新版本后重试。
		version = latest.StateVersion
		resource.StateVersion = version + 1
	}

	logger.Torrent.Warn("下载任务状态写入版本冲突，已放弃本次重试",
		slog.String("info_hash", resource.InfoHash),
		slog.Uint64("version", resource.StateVersion),
	)
	return fmt.Errorf("%s: %s", ErrKeyInvalidTransition, resource.InfoHash)
}

// persistSnapshot 以 CAS 方式写入资源快照，返回是否写入成功。
//
// allowRetry 为 true 时代表状态迁移，冲突时基于数据库最新版本重试，
// 保证任何异常场景下状态变更都能原子落库；为 false 时代表周期进度快照，
// 冲突说明存在更新的写入，直接丢弃陈旧快照。
func (m *TorrentManager) persistSnapshot(resource *models.Resource, fromVersion uint64, allowRetry bool) bool {
	if resource == nil {
		return false
	}

	if allowRetry {
		if err := m.persistState(resource, fromVersion); err != nil {
			logger.Torrent.Error("保存下载任务状态失败",
				slog.String("info_hash", resource.InfoHash),
				slog.Any("err", err),
			)
			m.syncJobVersion(resource.InfoHash)
			return false
		}
		return true
	}

	ok, err := m.ResourceDao.UpdateState(resource, fromVersion)
	if err != nil {
		logger.Torrent.Error("保存下载任务失败", slog.String("info_hash", resource.InfoHash), slog.Any("err", err))
		return false
	}
	if !ok {
		logger.Torrent.Debug("下载任务状态版本已变化，丢弃陈旧快照",
			slog.String("info_hash", resource.InfoHash),
			slog.Uint64("expected", fromVersion),
			slog.Uint64("actual", resource.StateVersion),
		)
		m.syncJobVersion(resource.InfoHash)
	}
	return ok
}

// syncJobVersion 同步内存任务的最新落库版本，避免后续 CAS 持续失败。
func (m *TorrentManager) syncJobVersion(infoHash string) {
	resource, err := m.ResourceDao.GetByInfoHash(infoHash)
	if err != nil || resource == nil {
		return
	}
	job, err := m.GetJob(infoHash)
	if err != nil {
		return
	}
	job.syncPersistedVersion(resource.StateVersion)
}

// emitState 推送任务状态变更事件。
func (m *TorrentManager) emitState(job *TorrentJob) {
	if job == nil {
		return
	}
	emitDownloadEvent(EventDownloadStateChanged, job.ToVO())
}

// schedule 持续领取排队任务，直到信号量用尽或队列为空。
// 队列按优先级倒序、创建时间正序选取，保证高优先级任务先下载。
func (m *TorrentManager) schedule() {
	// 关闭后不再领取排队任务，避免关停过程中重新挂载种子。
	if m.closed.Load() {
		return
	}
	for {
		if !m.sem.TryAcquire(1) {
			return
		}
		if m.closed.Load() {
			m.sem.Release(1)
			return
		}
		job := m.claimNextQueued()
		if job == nil {
			m.sem.Release(1)
			return
		}
		go m.runJob(job)
	}
}

// claimNextQueued 选取优先级最高的排队任务并流转为下载中，避免同一任务被重复调度。
func (m *TorrentManager) claimNextQueued() *TorrentJob {
	m.mu.RLock()
	candidates := make([]*TorrentJob, 0, len(m.jobsMap))
	for _, job := range m.jobsMap {
		if job.currentState() == models.TaskQueued {
			candidates = append(candidates, job)
		}
	}
	m.mu.RUnlock()
	if len(candidates) == 0 {
		return nil
	}

	// 优先级倒序 → 创建时间正序 → InfoHash 稳定排序。
	sort.SliceStable(candidates, func(i, j int) bool {
		pi, pj := candidates[i].priorityValue(), candidates[j].priorityValue()
		if pi != pj {
			return pi > pj
		}
		ti, tj := candidates[i].createTimeValue(), candidates[j].createTimeValue()
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return candidates[i].infoHash() < candidates[j].infoHash()
	})

	for _, job := range candidates {
		res, err := job.Apply(models.EvSlotGranted, models.EventPayload{Now: time.Now()})
		if err != nil {
			continue
		}
		if res.Task == models.TaskDownloading {
			return job
		}
	}
	return nil
}

// runJob 执行下载；暂停 / 取消 / 完成的状态流转由对应事件完成。
func (m *TorrentManager) runJob(job *TorrentJob) {
	defer func() {
		m.sem.Release(1)
		go m.schedule()
	}()

	outcome := job.StartDownload()
	switch outcome {
	case outcomeStalled:
		m.failOrRetry(job, ErrKeyStalled)
	case outcomeDeadSeed:
		// 死种观察窗口过了仍无进展：种子本身已失效，直接标记 failed，不走重试。
		m.markDeadSeedFailed(job)
	}
}

// markDeadSeedFailed 判定死种后直接标记任务为 failed，不再进入自动重试流程。
// 使用 EvDownloadFatal 直达 TaskFailed：不消耗重试次数，也不会在前端显示「已重试 N 次」。
func (m *TorrentManager) markDeadSeedFailed(job *TorrentJob) {
	if job == nil {
		return
	}
	if state := job.currentState(); state != models.TaskDownloading {
		return
	}

	now := time.Now()
	res, err := job.Apply(models.EvDownloadFatal, models.EventPayload{
		Now:       now,
		ErrorCode: ErrKeyDeadSeed,
	})
	if err != nil {
		logger.Torrent.Warn("死种流转被拒绝", slog.String("info_hash", job.infoHash()), slog.Any("err", err))
		return
	}
	if res.Task == models.TaskFailed {
		logger.Torrent.Warn("判定为死种，任务标记失败", slog.String("info_hash", job.infoHash()))
	}
}

// failOrRetry 记录失败原因并按尝试次数决定进入重试等待或标记失败。
func (m *TorrentManager) failOrRetry(job *TorrentJob, errorKey string) {
	if job == nil {
		return
	}
	state := job.currentState()
	if state != models.TaskDownloading && state != models.TaskPreparing {
		return
	}

	now := time.Now()
	attempt := job.attempt() + 1
	var p = models.EventPayload{
		Now:       now,
		Attempt:   attempt,
		ErrorCode: errorKey,
	}
	if attempt < job.maxAttempts() {
		next := now.Add(m.downloadCfg.RetryDelay)
		p.NextRetryAt = &next
	}

	e := models.EvStalled
	if state == models.TaskPreparing {
		e = models.EvMetaTimeout
	}

	res, err := job.Apply(e, p)
	if err != nil {
		logger.Torrent.Warn("下载任务失败流转被拒绝", slog.String("info_hash", job.infoHash()), slog.Any("err", err))
		return
	}

	if res.Task == models.TaskFailed {
		logger.Torrent.Error("下载任务重试次数耗尽",
			slog.String("info_hash", job.infoHash()),
			slog.String("error", errorKey),
			slog.Int("attempt", int(attempt)),
		)
		return
	}
	if res.Task != models.TaskRetryWait {
		return
	}

	ctx := job.getContext()
	delay := m.downloadCfg.RetryDelay
	infoHash := job.infoHash()
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		// 等待期间可能被暂停 / 取消，需要再次确认状态
		if job.currentState() != models.TaskRetryWait {
			return
		}
		if _, err := job.Apply(models.EvRetryDelayElapsed, models.EventPayload{Now: time.Now()}); err != nil {
			return
		}
		job.resetContext()
		if err := m.restartTorrent(job); err != nil {
			m.failOrRetry(job, classifyTorrentError(err))
			return
		}
		go m.waitMetadata(job)
	}()

	logger.Torrent.Info("下载任务将在稍后自动重试",
		slog.String("info_hash", infoHash),
		slog.String("error", errorKey),
		slog.Int("attempt", int(attempt)),
		slog.Duration("delay", delay),
	)
}

// waitMetadata 等待 Torrent 元信息；超时后触发自动重试。
func (m *TorrentManager) waitMetadata(job *TorrentJob) {
	tt := job.getTorrent()
	ctx := job.getContext()
	if tt == nil {
		m.failOrRetry(job, ErrKeyDownloadFailed)
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-tt.GotInfo():
		m.onMetadataReady(job, tt)
	case <-time.After(m.downloadCfg.MetadataTimeout):
		if ctx.Err() != nil {
			return
		}
		if job.currentState() != models.TaskPreparing {
			return
		}
		m.failOrRetry(job, ErrKeyMetadataTimeout)
	}
}

// onMetadataReady 元信息就绪后填充资源信息并加入下载队列。
func (m *TorrentManager) onMetadataReady(job *TorrentJob, tt *torrent.Torrent) {
	info := tt.Info()
	if info == nil {
		return
	}

	name := info.Name
	total := info.TotalLength()
	now := time.Now()

	job.mu.Lock()
	if job.job.TaskState != models.TaskPreparing {
		job.mu.Unlock()
		return
	}
	job.job.ResourceName = &name
	job.job.TotalSize = &total
	job.job.UpdateTime = &now

	if job.job.FileName == nil {
		var bestName string
		var bestSize int64
		for _, file := range tt.Files() {
			if !isVideoPath(file.Path()) {
				continue
			}
			// 多文件种子取体积最大的视频作为主文件
			if size := file.Length(); size > bestSize {
				bestName = filepath.Base(file.Path())
				bestSize = size
			}
		}
		if bestName != "" {
			job.job.FileName = &bestName
		}
	}
	baseDir := job.job.LocalPath
	infoHash := job.job.InfoHash
	job.mu.Unlock()

	// 元信息只获取一次，缓存到下载目录后重启可跳过 DHT 拉取，直接续传。
	m.cacheMetadata(baseDir, infoHash, tt)

	if _, err := job.Apply(models.EvMetaReady, models.EventPayload{Now: now}); err != nil {
		logger.Torrent.Warn("元信息就绪流转失败", slog.String("info_hash", infoHash), slog.Any("err", err))
		return
	}
	m.schedule()
}

// cacheMetadata 把任务的种子元信息缓存到下载目录，供应用重启后直接恢复任务。
func (m *TorrentManager) cacheMetadata(baseDir, infoHash string, tt *torrent.Torrent) {
	if tt == nil || tt.Info() == nil {
		return
	}
	if baseDir == "" {
		baseDir = m.currentDownloadDir()
	}

	mi := tt.Metainfo()
	if err := writeMetadataCache(baseDir, infoHash, &mi); err != nil {
		logger.Torrent.Warn("缓存种子元信息失败",
			slog.String("info_hash", infoHash),
			slog.Any("err", err),
		)
	}
}

// recoverTaskStates 应用启动时需要自动恢复的任务状态。
//
// Paused（用户暂停）属于用户明确的意图，不自动恢复，但仍通过数据库查询保留在
// 「待下载」列表。Failed 属于异常中断（元信息超时、挂载失败、网络中断、应用强制
// 退出等），需要自动重新入队。取消的任务已恢复为未下载状态，不会进入恢复流程。
var recoverTaskStates = []models.TaskState{
	models.TaskPreparing,
	models.TaskQueued,
	models.TaskDownloading,
	models.TaskVerifying,
	models.TaskRetryWait,
	models.TaskFailed,
}

// RecoverPendingJobs 应用启动后扫描待下载队列，自动恢复所有异常中断的任务。
//
// 数据来源为数据库中的完整任务状态，因此元信息获取失败、网络中断、
// 应用强制退出、系统重启等场景都能在重启后自动继续下载，无需用户手动触发。
func (m *TorrentManager) RecoverPendingJobs() {
	// 启动恢复是异步执行的，应用可能已经进入退出流程。
	if m.closed.Load() {
		return
	}
	rows, err := m.ResourceDao.ListByTaskStates(recoverTaskStates)
	if err != nil {
		logger.Torrent.Error("恢复未完成下载任务失败", slog.Any("err", err))
		return
	}

	for i := range rows {
		m.recoverResource(&rows[i])
	}

	m.checkCompletedLocalFiles()
	logger.Torrent.Info("已恢复未完成下载任务", slog.Int("count", len(rows)))
	m.schedule()
}

// withMetadataCache 优先复用下载目录中缓存的种子元信息。
//
// 元信息只在首次就绪时写入缓存；任务在拉取元信息阶段异常退出后重启，
// 直接使用缓存即可跳过 DHT / peer 拉取，避免再次等待元信息而卡在准备阶段。
func (m *TorrentManager) withMetadataCache(baseDir, infoHash string, spec *torrent.TorrentSpec) *torrent.TorrentSpec {
	if spec == nil || len(spec.InfoBytes) > 0 {
		return spec
	}
	if baseDir == "" {
		baseDir = m.currentDownloadDir()
	}
	cached := readMetadataCache(baseDir, infoHash)
	if cached == nil {
		return spec
	}
	cached.PeerAddrs = append(cached.PeerAddrs, spec.PeerAddrs...)
	cached.Sources = append(cached.Sources, spec.Sources...)
	cached.Webseeds = append(cached.Webseeds, spec.Webseeds...)
	return cached
}

// recoverResource 按 §7 恢复规则恢复单个任务。
//
// Downloading 中断后回到 Preparing 重新挂载；Verifying 重新执行落盘校验；
// RetryWait 未到重试时间则等待；Failed 属于异常中断，自动重新入队；
// Paused / Cancelled 属于用户意图，不自动恢复，但保留在待下载列表。
func (m *TorrentManager) recoverResource(resource *models.Resource) {
	if resource == nil || resource.Kind != models.KindTorrent {
		return
	}
	if resource.InfoHash == "" && resource.SourceURL == nil && resource.TorrentPath == nil {
		return
	}
	// 内存中已有同一任务时不重复挂载，避免同一 InfoHash 被创建多个句柄。
	if _, err := m.GetJob(resource.InfoHash); err == nil {
		return
	}

	now := time.Now()
	switch resource.TaskState {
	case models.TaskDownloading:
		m.rewriteState(resource, models.TaskPreparing)
	case models.TaskFailed:
		// 失败可能来自元信息超时、挂载失败、网络中断或应用强制退出，
		// 均不属于用户取消；启动时自动重新入队并重置尝试次数。
		fromVersion := resource.StateVersion
		if _, err := resource.ApplyEvent(models.EvManualRetry, models.EventPayload{
			Now:             now,
			SourceAvailable: sourceAvailable(resource),
		}); err != nil {
			logger.Torrent.Warn("自动恢复失败任务被拒绝",
				slog.String("info_hash", resource.InfoHash),
				slog.Any("err", err),
			)
			return
		}
		resource.UpdateTime = &now
		if err := m.persistState(resource, fromVersion); err != nil {
			logger.Torrent.Warn("自动恢复失败任务状态落库失败",
				slog.String("info_hash", resource.InfoHash),
				slog.Any("err", err),
			)
		}
	case models.TaskVerifying:
		// 保持 Verifying，挂载后重新校验
	case models.TaskRetryWait:
		if resource.NextRetryAt != nil && resource.NextRetryAt.After(now) {
			go m.waitRetryDelay(resource)
			return
		}
		m.rewriteState(resource, models.TaskPreparing)
	case models.TaskPreparing, models.TaskQueued:
		// 保持不变
	default:
		return
	}

	spec, magnet, torrentPath, err := m.specFromResource(resource)
	if err != nil {
		// 来源不可解析时先退回准备阶段，才能按状态机迁移到 Failed 并保留错误信息。
		if resource.TaskState != models.TaskPreparing {
			m.rewriteState(resource, models.TaskPreparing)
		}
		m.failResource(resource, ErrKeyNoSource)
		return
	}
	// 元信息缓存能直接恢复任务，避免重启后再次等待 DHT / peer 拉取。
	spec = m.withMetadataCache(resource.LocalPath, resource.InfoHash, spec)

	// Queued / Verifying 依赖元信息；缓存缺失时退回准备阶段重新拉取。
	if resource.TaskState == models.TaskQueued || resource.TaskState == models.TaskVerifying {
		if len(spec.InfoBytes) == 0 {
			m.rewriteState(resource, models.TaskPreparing)
		}
	}

	job := m.newJob(resource, magnet, torrentPath)
	// 先登记到内存任务表再挂载：挂载失败进入重试等待后，重试流程依赖调度器
	// 能从 jobsMap 中取到该任务，否则会在 Queued 状态永久滞留。
	m.mu.Lock()
	m.jobsMap[resource.InfoHash] = job
	m.mu.Unlock()
	if err := m.attachTorrent(job, spec); err != nil {
		m.markJobAttachFailed(job, classifyTorrentError(err))
		return
	}

	switch resource.TaskState {
	case models.TaskVerifying:
		go m.reverifyJob(job)
	case models.TaskQueued:
		go m.schedule()
	default:
		go m.waitMetadata(job)
	}
}

// rewriteState 启动恢复时直接改写任务状态并落库（Downloading / RetryWait → Preparing）。
//
// 落库采用 CAS 冲突重试：版本冲突时不能静默丢弃，否则数据库会停留在旧状态，
// 下次重启无法按预期恢复。
func (m *TorrentManager) rewriteState(resource *models.Resource, to models.TaskState) error {
	fromVersion := resource.StateVersion
	now := time.Now()
	resource.TaskState = to
	if resource.LocalState == models.LocalPresent {
		resource.LocalState = models.LocalPartial
	}
	resource.StateVersion++
	resource.StateChangedAt = &now
	resource.UpdateTime = &now
	resource.NextRetryAt = nil
	if err := m.persistState(resource, fromVersion); err != nil {
		logger.Torrent.Warn("恢复任务状态失败",
			slog.String("info_hash", resource.InfoHash),
			slog.Any("err", err),
		)
		return err
	}
	return nil
}

// waitRetryDelay 恢复 RetryWait 任务：到点后重新挂载并等待元信息。
func (m *TorrentManager) waitRetryDelay(resource *models.Resource) {
	delay := time.Until(*resource.NextRetryAt)
	if delay < 0 {
		delay = 0
	}
	time.Sleep(delay)

	row, err := m.ResourceDao.GetByInfoHash(resource.InfoHash)
	if err != nil || row == nil || row.TaskState != models.TaskRetryWait {
		return
	}
	spec, magnet, torrentPath, err := m.specFromResource(row)
	if err != nil {
		m.rewriteState(row, models.TaskPreparing)
		m.failResource(row, ErrKeyNoSource)
		return
	}
	spec = m.withMetadataCache(row.LocalPath, row.InfoHash, spec)

	job := m.newJob(row, magnet, torrentPath)
	if _, err := job.Apply(models.EvRetryDelayElapsed, models.EventPayload{Now: time.Now()}); err != nil {
		return
	}
	job.resetContext()
	m.mu.Lock()
	m.jobsMap[row.InfoHash] = job
	m.mu.Unlock()
	if err := m.attachTorrent(job, spec); err != nil {
		m.markJobAttachFailed(job, classifyTorrentError(err))
		return
	}
	go m.waitMetadata(job)
}

// reverifyJob 恢复 Verifying 任务：挂载后重新执行落盘校验。
func (m *TorrentManager) reverifyJob(job *TorrentJob) {
	tt := job.getTorrent()
	if tt == nil || tt.Info() == nil {
		return
	}
	job.mu.RLock()
	baseDir := job.job.LocalPath
	job.mu.RUnlock()

	check := verifyCompletedFiles(tt, baseDir)
	now := time.Now()
	switch {
	case check.missing:
		job.Apply(models.EvVerifyMissing, models.EventPayload{Now: now, ErrorCode: ErrKeyIntegrity, LossReason: models.LossNotFound})
	case check.mismatch:
		job.Apply(models.EvVerifyCorrupted, models.EventPayload{Now: now, ErrorCode: ErrKeyIntegrity, LossReason: models.LossSizeMismatch})
	default:
		job.Apply(models.EvVerifyOK, models.EventPayload{Now: now})
	}
}

// checkCompletedLocalFiles 启动时检查已完成任务的主文件是否仍然存在。
func (m *TorrentManager) checkCompletedLocalFiles() {
	rows, err := m.ResourceDao.ListCompletedTasks()
	if err != nil {
		logger.Torrent.Warn("检查已完成任务本地文件失败", slog.Any("err", err))
		return
	}
	for i := range rows {
		resource := &rows[i]
		if resource.LocalState != models.LocalPresent || resource.LocalPath == "" {
			continue
		}
		if !localDataMissing(resource) {
			continue
		}
		m.applyResourceEvent(resource.InfoHash, models.EvFileLost, models.EventPayload{
			Now:        time.Now(),
			LossReason: models.LossNotFound,
		})
	}
}

// failResource 对内存中不存在的任务直接标记失败。
func (m *TorrentManager) failResource(resource *models.Resource, errorKey string) {
	if resource == nil {
		return
	}
	m.applyResourceEvent(resource.InfoHash, models.EvSourceUnresolved, models.EventPayload{
		Now:       time.Now(),
		ErrorCode: errorKey,
	})
}

// AddMagnet 添加单个 Magnet。
func (m *TorrentManager) AddMagnet(magnet string) error {
	_, _, err := m.enqueueMagnet(magnet, 0)
	return err
}

// AddTorrent 添加单个 torrent 文件。
func (m *TorrentManager) AddTorrent(filePath string) error {
	_, _, err := m.enqueueTorrentFile(filePath, 0)
	return err
}

// AddMagnets 批量添加磁力链接，返回新增 / 跳过 / 失败统计。
func (m *TorrentManager) AddMagnets(magnets []string, priority int) *BatchDownloadResult {
	result := &BatchDownloadResult{}
	for _, magnet := range magnets {
		if _, added, err := m.enqueueMagnet(magnet, priority); err != nil {
			result.addBatchError(errorKeyOf(err))
		} else if added {
			result.Added++
		} else {
			result.Skipped++
		}
	}
	m.schedule()
	return result
}

// DownloadResources 按 InfoHash 批量加入下载队列，用于种子资源页的多选下载。
func (m *TorrentManager) DownloadResources(infoHashes []string, priority int) (*BatchDownloadResult, error) {
	result := &BatchDownloadResult{}
	if len(infoHashes) == 0 {
		return result, nil
	}

	rows, err := m.ResourceDao.GetByInfoHashes(infoHashes)
	if err != nil {
		return nil, err
	}

	for i := range rows {
		_, added, enqueueErr := m.enqueueResource(&rows[i], priority, 0)
		if enqueueErr != nil {
			result.addBatchError(errorKeyOf(enqueueErr))
		} else if added {
			result.Added++
		} else {
			result.Skipped++
		}
	}
	m.schedule()
	return result, nil
}

// enqueueMagnet 解析磁力链接并创建任务。
func (m *TorrentManager) enqueueMagnet(magnet string, priority int) (string, bool, error) {
	magnet = strings.TrimSpace(magnet)
	if magnet == "" {
		return "", false, errors.New(ErrKeyMagnetEmpty)
	}

	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil {
		return "", false, errors.New(ErrKeyInvalidMagnet)
	}
	return m.enqueueSpec(spec, magnet, "", priority, models.EvAddDownload)
}

// enqueueTorrentFile 解析本地 torrent 文件并创建任务。
func (m *TorrentManager) enqueueTorrentFile(filePath string, priority int) (string, bool, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return "", false, errors.New(ErrKeyNoSource)
	}

	mi, err := metainfo.LoadFromFile(filePath)
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", ErrKeyResourceGone, err)
	}
	spec := torrent.TorrentSpecFromMetaInfo(mi)
	return m.enqueueSpec(spec, "", filePath, priority, models.EvAddDownload)
}

// specFromResource 从数据库记录构建 TorrentSpec，缺失来源时返回 ErrKeyNoSource。
func (m *TorrentManager) specFromResource(resource *models.Resource) (*torrent.TorrentSpec, string, string, error) {
	magnet := ""
	if resource.SourceURL != nil {
		magnet = strings.TrimSpace(*resource.SourceURL)
	}
	torrentPath := ""
	if resource.TorrentPath != nil {
		torrentPath = strings.TrimSpace(*resource.TorrentPath)
	}

	if torrentPath != "" {
		if mi, err := metainfo.LoadFromFile(torrentPath); err == nil {
			return torrent.TorrentSpecFromMetaInfo(mi), magnet, torrentPath, nil
		}
	}

	if magnet == "" {
		if resource.InfoHash == "" {
			return nil, "", "", errors.New(ErrKeyNoSource)
		}
		// 爬虫记录通常带有磁力链接；仅当缺失时按 InfoHash 构造最小磁力链接。
		magnet = "magnet:?xt=urn:btih:" + resource.InfoHash
	}

	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil {
		return nil, "", "", errors.New(ErrKeyInvalidMagnet)
	}
	return spec, magnet, torrentPath, nil
}

// enqueueResource 从数据库记录重新入队（继续 / 恢复 / 重试 / 已完成文件缺失后重新下载）。
//
// e 为 0 时按当前状态自动选择事件：Failed → 手动重试、Cancelled → 重新下载、其余 → 新增下载。
func (m *TorrentManager) enqueueResource(resource *models.Resource, priority int, e models.Event) (string, bool, error) {
	if resource == nil {
		return "", false, errors.New(ErrKeyTaskNotFound)
	}
	if resource.Kind == models.KindLocal {
		return "", false, nil
	}
	// 内存中已有同一任务：已完成且本地文件被删除时重新下载，其余情况保持幂等。
	if job, jobErr := m.GetJob(resource.InfoHash); jobErr == nil {
		restarted, restartErr := m.redownloadCompletedJob(job)
		return resource.InfoHash, restarted, restartErr
	}
	if resource.TaskState.Active() {
		return "", false, nil
	}
	// 已完成任务：本地数据仍在时跳过；文件被外部删除时重新下载。
	if resource.TaskState == models.TaskCompleted {
		if resource.LocalState == models.LocalPresent {
			if !localDataMissing(resource) {
				return "", false, nil
			}
			// 先记录为文件丢失（Completed -> Completed/Missing），再由 EvRepair 重新下载。
			if _, err := resource.ApplyEvent(models.EvFileLost, models.EventPayload{
				Now:        time.Now(),
				LossReason: models.LossNotFound,
			}); err != nil {
				return "", false, err
			}
		}
		e = models.EvRepair
	}
	if e == 0 {
		switch resource.TaskState {
		case models.TaskFailed:
			e = models.EvManualRetry
		case models.TaskCancelled:
			e = models.EvRedownload
		case models.TaskDiscovered:
			e = models.EvAddDownload
		default:
			return "", false, nil
		}
	}

	spec, magnet, torrentPath, err := m.specFromResource(resource)
	if err != nil {
		return "", false, err
	}
	// 重启后继续 / 重试同样优先复用元信息缓存。
	baseDir := m.currentDownloadDir()
	if resource.LocalPath != "" {
		baseDir = resource.LocalPath
	}
	spec = m.withMetadataCache(baseDir, resource.InfoHash, spec)
	return m.startJob(resource, spec, magnet, torrentPath, priority, e)
}

// enqueueSpec 把 TorrentSpec 转换成下载任务；已存在或已完成时直接跳过。
func (m *TorrentManager) enqueueSpec(spec *torrent.TorrentSpec, magnet, torrentPath string, priority int, e models.Event) (string, bool, error) {
	if spec == nil || spec.InfoHash.IsZero() {
		return "", false, errors.New(ErrKeyInvalidMagnet)
	}
	// 统一使用十六进制字符串，与爬虫记录和磁力链接中的 InfoHash 保持一致。
	infoHash := spec.InfoHash.HexString()

	// 内存中已有同一任务：只提升优先级，避免重复添加；已完成任务的文件被删除时重新下载。
	m.mu.Lock()
	if existing := m.jobsMap[infoHash]; existing != nil {
		m.mu.Unlock()
		if existing.setPriority(priority) {
			if hash := existing.infoHash(); hash != "" {
				if err := m.ResourceDao.UpdatePriority(hash, priority); err != nil {
					logger.Torrent.Error("更新任务优先级失败", slog.String("info_hash", hash), slog.Any("err", err))
				}
			}
			m.emitState(existing)
		}
		restarted, restartErr := m.redownloadCompletedJob(existing)
		if restartErr != nil {
			return infoHash, false, restartErr
		}
		return infoHash, restarted, nil
	}
	m.mu.Unlock()

	resource, err := m.ResourceDao.GetByInfoHash(infoHash)
	if err != nil {
		return infoHash, false, err
	}

	now := time.Now()
	if resource == nil {
		resource = &models.Resource{
			InfoHash:     infoHash,
			Kind:         models.KindTorrent,
			TaskState:    models.TaskDiscovered,
			LocalState:   models.LocalNone,
			LocalPath:    m.currentDownloadDir(),
			MaxAttempts:  uint16(m.downloadCfg.MaxRetries),
			CreateTime:   &now,
			UpdateTime:   &now,
			StateVersion: 0,
		}
	} else if resource.Kind == models.KindLocal {
		// 本地视频不进入下载流程
		return infoHash, false, nil
	} else if resource.TaskState == models.TaskCompleted {
		// 已完成但本地文件已被删除：重新下载；文件仍在时跳过。
		if resource.LocalState == models.LocalPresent {
			if !localDataMissing(resource) {
				return infoHash, false, nil
			}
			if _, err := resource.ApplyEvent(models.EvFileLost, models.EventPayload{
				Now:        now,
				LossReason: models.LossNotFound,
			}); err != nil {
				return infoHash, false, err
			}
		}
		e = models.EvRepair
	} else if resource.TaskState.Active() {
		// 已有任务在活跃流程中，避免重复挂载
		return infoHash, false, nil
	} else if e == models.EvAddDownload {
		switch resource.TaskState {
		case models.TaskFailed:
			e = models.EvManualRetry
		case models.TaskCancelled:
			e = models.EvRedownload
		case models.TaskDiscovered:
		default:
			return infoHash, false, nil
		}
	}

	// 有元信息缓存时直接按缓存恢复任务，重启后无需重新等待 DHT / peer 拉取元信息。
	baseDir := m.currentDownloadDir()
	if resource.LocalPath != "" {
		baseDir = resource.LocalPath
	}
	spec = m.withMetadataCache(baseDir, infoHash, spec)

	return m.startJob(resource, spec, magnet, torrentPath, priority, e)
}

// startJob 在资源记录上应用入队事件、落库并挂载 Torrent。
func (m *TorrentManager) startJob(resource *models.Resource, spec *torrent.TorrentSpec, magnet, torrentPath string, priority int, e models.Event) (string, bool, error) {
	if resource == nil || spec == nil {
		return "", false, errors.New(ErrKeyInvalidMagnet)
	}
	now := time.Now()

	resource.Kind = models.KindTorrent
	if resource.MaxAttempts == 0 {
		resource.MaxAttempts = uint16(m.downloadCfg.MaxRetries)
	}
	if resource.CreateTime == nil {
		resource.CreateTime = &now
	}
	if resource.LocalPath == "" {
		resource.LocalPath = m.currentDownloadDir()
	}
	if resource.Priority == nil || *resource.Priority < priority {
		resource.Priority = &priority
	}
	if magnet != "" && (resource.SourceURL == nil || strings.TrimSpace(*resource.SourceURL) == "") {
		resource.SourceURL = &magnet
	}
	if torrentPath != "" {
		resource.TorrentPath = &torrentPath
	}

	fromVersion := resource.StateVersion
	if _, err := resource.ApplyEvent(e, models.EventPayload{Now: now, SourceAvailable: true}); err != nil {
		return resource.InfoHash, false, err
	}
	if resource.InfoHash == "" {
		resource.InfoHash = spec.InfoHash.HexString()
	}
	resource.UpdateTime = &now

	if resource.ID == 0 {
		// 新任务在挂载 Torrent 之前先落库，保证应用在拉取元信息阶段崩溃后
		// 重启仍能按 Preparing 状态自动恢复下载。
		if err := m.ResourceDao.Save(resource); err != nil {
			return resource.InfoHash, false, err
		}
	} else {
		// 已存在的爬虫记录必须在挂载前完成状态迁移落库，
		// 冲突时重试，避免数据库停留在 Discovered 导致重启后不恢复。
		if err := m.persistState(resource, fromVersion); err != nil {
			return resource.InfoHash, false, err
		}
	}

	job := m.newJob(resource, magnet, torrentPath)
	m.mu.Lock()
	m.jobsMap[resource.InfoHash] = job
	m.mu.Unlock()
	if err := m.attachTorrent(job, spec); err != nil {
		m.markJobAttachFailed(job, classifyTorrentError(err))
		return resource.InfoHash, false, fmt.Errorf("%s: %w", classifyTorrentError(err), err)
	}
	m.emitState(job)
	go m.waitMetadata(job)
	return resource.InfoHash, true, nil
}

// newJob 创建并绑定任务运行参数。
func (m *TorrentManager) newJob(resource *models.Resource, magnet, torrentPath string) *TorrentJob {
	ctx, cancel := context.WithCancel(context.Background())
	job := &TorrentJob{
		job:           resource,
		priorityIndex: -1, // -1代表不指定特定文件，全部下载；>=0才指定文件索引
		magnet:        magnet,
		torrentPath:   torrentPath,
		ctx:           ctx,
		cancel:        cancel,
	}
	job.bind(m.downloadCfg, func(payload *vo.TorrentJobVO) {
		emitDownloadEvent(EventDownloadProgress, payload)
	}, m.persistSnapshot, m.clearPieces)
	return job
}

// markJobAttachFailed 挂载失败时按状态决定进入重试等待或标记失败。
func (m *TorrentManager) markJobAttachFailed(job *TorrentJob, errorKey string) {
	if job == nil {
		return
	}
	state := job.currentState()
	if state != models.TaskPreparing {
		return
	}
	now := time.Now()
	attempt := job.attempt() + 1
	var p = models.EventPayload{
		Now:       now,
		Attempt:   attempt,
		ErrorCode: errorKey,
	}
	if attempt < job.maxAttempts() {
		next := now.Add(m.downloadCfg.RetryDelay)
		p.NextRetryAt = &next
	}
	if _, err := job.Apply(models.EvAttachFail, p); err != nil {
		logger.Torrent.Warn("挂载失败流转被拒绝", slog.String("info_hash", job.infoHash()), slog.Any("err", err))
	}
	if job.currentState() == models.TaskRetryWait {
		m.waitRetryAfterAttachFail(job)
	}
}

// waitRetryAfterAttachFail 挂载失败后的等待重试。
func (m *TorrentManager) waitRetryAfterAttachFail(job *TorrentJob) {
	ctx := job.getContext()
	delay := m.downloadCfg.RetryDelay
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if job.currentState() != models.TaskRetryWait {
			return
		}
		if _, err := job.Apply(models.EvRetryDelayElapsed, models.EventPayload{Now: time.Now()}); err != nil {
			return
		}
		job.resetContext()
		if err := m.restartTorrent(job); err != nil {
			m.markJobAttachFailed(job, classifyTorrentError(err))
			return
		}
		go m.waitMetadata(job)
	}()
}

// clearPieces 清理任务的分片完成记录，避免校验失败后重试直接沿用过期的完成标记。
func (m *TorrentManager) clearPieces(job *TorrentJob) {
	if job == nil {
		return
	}
	tt := job.getTorrent()
	if tt == nil || tt.Info() == nil {
		return
	}
	baseDir := job.saveDir()
	if baseDir == "" {
		baseDir = m.currentDownloadDir()
	}
	pc, err := m.pieceCompletionForDir(baseDir)
	if err != nil {
		logger.Torrent.Warn("打开分片完成记录失败", slog.String("info_hash", job.infoHash()), slog.Any("err", err))
		return
	}

	info := tt.Info()
	infoHash := tt.InfoHash()
	for i := 0; i < info.NumPieces(); i++ {
		key := metainfo.PieceKey{InfoHash: infoHash, Index: i}
		if err := pc.Set(key, g.None[bool]()); err != nil {
			logger.Torrent.Warn("清理分片完成记录失败",
				slog.String("info_hash", job.infoHash()),
				slog.Int("piece", i),
				slog.Any("err", err),
			)
			return
		}
	}
}

// metadataCacheDir 下载目录下缓存种子元信息的子目录名。
const metadataCacheDir = ".meta"

// metadataCachePath 返回元信息缓存文件路径；下载目录或 InfoHash 为空时返回空串。
func metadataCachePath(baseDir, infoHash string) string {
	infoHash = strings.ToLower(strings.TrimSpace(infoHash))
	if baseDir == "" || infoHash == "" {
		return ""
	}
	return filepath.Join(baseDir, metadataCacheDir, infoHash+".torrent")
}

// writeMetadataCache 把元信息写入下载目录缓存。
// 使用临时文件 + 重命名，避免写入中途退出留下损坏文件。
func writeMetadataCache(baseDir, infoHash string, mi *metainfo.MetaInfo) error {
	path := metadataCachePath(baseDir, infoHash)
	if path == "" || mi == nil {
		return errors.New(ErrKeyNoSource)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	temp := path + ".tmp"
	file, err := os.Create(temp)
	if err != nil {
		return err
	}
	if err := mi.Write(file); err != nil {
		file.Close()
		os.Remove(temp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return err
	}
	return nil
}

// readMetadataCache 读取并校验缓存的元信息；缺失、损坏或 InfoHash 不匹配时返回 nil。
func readMetadataCache(baseDir, infoHash string) *torrent.TorrentSpec {
	path := metadataCachePath(baseDir, infoHash)
	if path == "" {
		return nil
	}

	mi, err := metainfo.LoadFromFile(path)
	if err != nil {
		return nil
	}
	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil || spec == nil || spec.InfoHash.HexString() != strings.ToLower(infoHash) {
		return nil
	}
	return spec
}

// pieceCompletionForDir 返回下载目录共享的分片完成记录，首次使用时创建并缓存。
// 复用句柄可避免 CGO 关闭时 BoltDB 被重复打开而触发文件锁超时。
func (m *TorrentManager) pieceCompletionForDir(dir string) (storage.PieceCompletion, error) {
	key := filepath.Clean(dir)

	m.piecesMu.Lock()
	defer m.piecesMu.Unlock()

	if pc, ok := m.pieces[key]; ok {
		return pc, nil
	}

	pc, err := storage.NewDefaultPieceCompletionForDir(key)
	if err != nil {
		return nil, err
	}
	m.pieces[key] = pc
	return pc, nil
}

// publicTrackers 公共 tracker，用于补齐只带 DHT / x.pe 的磁力链接。
// 保持在 20 条左右：太少 peer 发现不足，太多会拖慢 announce 启动；
// 来源可参考 https://github.com/ngosang/trackerslist。
var publicTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
	"udp://explodie.org:6969/announce",
	"udp://tracker.dler.org:6969/announce",
	"udp://tracker.tiny-vps.com:6969/announce",
	"udp://opentracker.io:6969/announce",
	"udp://tracker1.bt.moack.co.kr:80/announce",
	"http://tracker.openbittorrent.com:80/announce",
	"udp://tracker.qu.ax:6969/announce",
	"udp://tracker.fnix.net:6969/announce",
	"udp://tracker.0x7c0.com:6969/announce",
	"udp://tracker.bittor.pw:1337/announce",
	"udp://tracker.gbitt.info:80/announce",
	"udp://tracker-udp.gbitt.info:80/announce",
	"udp://tracker.torrust-demo.com:6969/announce",
	"udp://tracker.zhuqiy.com:6969/announce",
	"udp://ipv6.tracker.harry.lu:80/announce",
	"https://tracker.tamersunion.org:443/announce",
}

// mergePublicTrackers 把公共 tracker 追加到 spec，已存在的不重复添加。
func mergePublicTrackers(spec *torrent.TorrentSpec) {
	seen := make(map[string]struct{}, len(publicTrackers))
	for _, tier := range spec.Trackers {
		for _, url := range tier {
			seen[url] = struct{}{}
		}
	}
	for _, url := range publicTrackers {
		if _, ok := seen[url]; ok {
			continue
		}
		spec.Trackers = append(spec.Trackers, []string{url})
		seen[url] = struct{}{}
	}
}

// attachTorrent 把 Torrent 加入客户端，使用任务自己的下载目录作为存储位置。
// piece completion 持久化在同一目录，重启后继续下载时可直接跳过已完成分片。
func (m *TorrentManager) attachTorrent(job *TorrentJob, spec *torrent.TorrentSpec) error {
	baseDir := job.saveDir()
	if baseDir == "" {
		baseDir = m.currentDownloadDir()
		job.mu.Lock()
		job.job.LocalPath = baseDir
		job.mu.Unlock()
	}

	pc, err := m.pieceCompletionForDir(baseDir)
	if err != nil {
		return err
	}
	spec.Storage = storage.NewFileWithCompletion(baseDir, pc)
	// 磁力通常只带 DHT 爬虫给出的 x.pe，补齐公共 tracker 才能拿到更多 peer
	mergePublicTrackers(spec)

	tt, _, err := m.client.AddTorrentSpec(spec)
	if err != nil {
		return err
	}
	job.setTorrent(tt)
	return nil
}

// restartTorrent 重新构建 TorrentSpec 并加入客户端，用于失败后的自动重试。
func (m *TorrentManager) restartTorrent(job *TorrentJob) error {
	job.mu.RLock()
	magnet := job.magnet
	torrentPath := job.torrentPath
	infoHash := ""
	baseDir := ""
	if job.job != nil {
		infoHash = job.job.InfoHash
		baseDir = job.job.LocalPath
	}
	job.mu.RUnlock()

	var spec *torrent.TorrentSpec
	var err error
	if torrentPath != "" {
		var mi *metainfo.MetaInfo
		mi, err = metainfo.LoadFromFile(torrentPath)
		if err == nil {
			spec = torrent.TorrentSpecFromMetaInfo(mi)
		}
	}
	if spec == nil && magnet != "" {
		spec, err = torrent.TorrentSpecFromMagnetUri(magnet)
	}
	if spec == nil {
		return errors.New(ErrKeyNoSource)
	}
	if err != nil {
		return err
	}

	// 元信息已缓存时直接复用，避免每次重试都重新等待 DHT 拉取。
	spec = m.withMetadataCache(baseDir, infoHash, spec)
	return m.attachTorrent(job, spec)
}

// saveDir 返回任务创建时确定的下载目录。
func (job *TorrentJob) saveDir() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.job == nil {
		return ""
	}
	return job.job.LocalPath
}

// attempt 返回当前连续失败次数。
func (job *TorrentJob) attempt() uint16 {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.job == nil {
		return 0
	}
	return job.job.Attempt
}

// maxAttempts 返回自动重试上限，缺省按 3 次。
func (job *TorrentJob) maxAttempts() uint16 {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.job == nil || job.job.MaxAttempts == 0 {
		return 3
	}
	return job.job.MaxAttempts
}

// sourceAvailable 判断任务是否具备可解析的下载来源。
func (job *TorrentJob) sourceAvailable() bool {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return sourceAvailable(job.job)
}

// sourceAvailable 判断资源记录是否具备可解析的下载来源。
func sourceAvailable(resource *models.Resource) bool {
	if resource == nil {
		return false
	}
	if resource.InfoHash != "" {
		return true
	}
	if resource.SourceURL != nil && strings.TrimSpace(*resource.SourceURL) != "" {
		return true
	}
	return resource.TorrentPath != nil && strings.TrimSpace(*resource.TorrentPath) != ""
}

// OpenDownloadDir 打开已完成任务的下载目录。
//
// local_path 统一保存下载目录，直接打开即可。
func (m *TorrentManager) OpenDownloadDir(infoHash string) error {
	infoHash = strings.TrimSpace(infoHash)
	if infoHash == "" {
		return errors.New(ErrKeyTaskNotFound)
	}

	resource, err := m.ResourceDao.GetByInfoHash(infoHash)
	if err != nil {
		logger.Torrent.Error("查询下载任务失败",
			slog.String("info_hash", infoHash),
			slog.Any("err", err),
		)
		return errors.New(ErrKeyOpenDirFailed)
	}
	if resource == nil {
		return errors.New(ErrKeyTaskNotFound)
	}

	dir := strings.TrimSpace(resource.LocalPath)
	if dir == "" {
		return errors.New(ErrKeyOpenDirFailed)
	}

	if err := openDirectory(dir); err != nil {
		logger.Torrent.Error("打开下载目录失败",
			slog.String("dir", dir),
			slog.Any("err", err),
		)
		return errors.New(ErrKeyOpenDirFailed)
	}
	return nil
}

// openDirectory 调用系统文件管理器打开指定目录。
func openDirectory(dir string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", dir).Start()
	case "darwin":
		return exec.Command("open", dir).Start()
	default:
		return exec.Command("xdg-open", dir).Start()
	}
}

// ErrorKey 提取错误中的语言包 key，供上层（Service / 前端）翻译错误提示。
func ErrorKey(err error) string {
	return errorKeyOf(err)
}

// errorKeyOf 提取错误中的语言包 key；未知错误统一归类。
func errorKeyOf(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, key := range []string{
		ErrKeyTaskNotFound,
		ErrKeyMagnetEmpty,
		ErrKeyInvalidMagnet,
		ErrKeyNoSource,
		ErrKeyMetadataTimeout,
		ErrKeyStalled,
		ErrKeyDownloadFailed,
		ErrKeyPermission,
		ErrKeyNoSpace,
		ErrKeyResourceGone,
		ErrKeyNetwork,
		ErrKeyIntegrity,
		ErrKeyCancelTooLate,
		ErrKeyInvalidTransition,
		ErrKeyRepairNotNeeded,
		ErrKeyDeleteNotAllowed,
		ErrKeyOpenDirFailed,
	} {
		if strings.Contains(message, key) {
			return key
		}
	}
	return classifyTorrentError(err)
}
