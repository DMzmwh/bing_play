package torrentmgr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go manager.RecoverPendingJobs()
			return nil
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

func (m *TorrentManager) Shutdown() error {
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

// ListActiveJobs 仅从内存读取未完成任务快照，不查询数据库。
// 供下载页高频轮询使用，避免数据库压力。
func (m *TorrentManager) ListActiveJobs() []*vo.TorrentJobVO {
	m.mu.RLock()
	jobs := make([]*TorrentJob, 0, len(m.jobsMap))
	for _, job := range m.jobsMap {
		jobs = append(jobs, job)
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

// RepairJob 修复本地文件缺失 / 损坏的已完成任务。
func (m *TorrentManager) RepairJob(infoHash string) error {
	if job, err := m.GetJob(infoHash); err == nil {
		res, applyErr := job.Apply(models.EvRepair, models.EventPayload{Now: time.Now(), SourceAvailable: job.sourceAvailable()})
		if applyErr != nil {
			return applyErr
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

	resource, dbErr := m.ResourceDao.GetByInfoHash(infoHash)
	if dbErr != nil {
		return dbErr
	}
	if resource == nil {
		return errors.New(ErrKeyTaskNotFound)
	}
	_, _, enqueueErr := m.enqueueResource(resource, 0, models.EvRepair)
	return enqueueErr
}

// CancelJob 取消任务，保留数据库记录用于下载页展示。
func (m *TorrentManager) CancelJob(infoHash string) error {
	job, err := m.GetJob(infoHash)
	if err != nil {
		return m.applyResourceEvent(infoHash, models.EvCancel, models.EventPayload{Now: time.Now()})
	}

	if cancelErr := job.Cancel(); cancelErr != nil {
		return cancelErr
	}

	m.mu.Lock()
	delete(m.jobsMap, infoHash)
	m.mu.Unlock()

	go m.schedule()
	return nil
}

// SetPriority 调整任务优先级，数值越大越先下载。
func (m *TorrentManager) SetPriority(infoHash string, priority int) error {
	job, err := m.GetJob(infoHash)
	if err == nil {
		if job.forcePriority(priority) {
			if id := job.id(); id != 0 {
				if err := m.ResourceDao.UpdatePriority(id, priority); err != nil {
					logger.Torrent.Error("更新任务优先级失败", slog.String("info_hash", infoHash), slog.Any("err", err))
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
	resource.Priority = &priority
	now := time.Now()
	resource.UpdateTime = &now
	return m.ResourceDao.Save(resource)
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
	if _, err := m.ResourceDao.UpdateState(resource, fromVersion); err != nil {
		return err
	}
	emitDownloadEvent(EventDownloadStateChanged, voFromResource(resource))
	return nil
}

// persistSnapshot 以 CAS 方式写入资源快照，返回是否写入成功。
// 失败通常意味着存在更新的写入，本次陈旧快照被丢弃。
func (m *TorrentManager) persistSnapshot(resource *models.Resource, fromVersion uint64) bool {
	if resource == nil {
		return false
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
	for {
		if !m.sem.TryAcquire(1) {
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

	if job.StartDownload() == outcomeStalled {
		m.failOrRetry(job, ErrKeyStalled)
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

// recoverTaskStates 启动时需要恢复的任务状态。
var recoverTaskStates = []models.TaskState{
	models.TaskPreparing,
	models.TaskQueued,
	models.TaskDownloading,
	models.TaskVerifying,
	models.TaskRetryWait,
}

// RecoverPendingJobs 应用启动后按状态机恢复规则恢复未完成的下载任务。
func (m *TorrentManager) RecoverPendingJobs() {
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

// recoverResource 按 §7 恢复规则恢复单个任务。
//
// Downloading 中断后回到 Preparing 重新挂载；Verifying 重新执行落盘校验；
// RetryWait 未到重试时间则等待；Paused 保持暂停，不进入内存。
func (m *TorrentManager) recoverResource(resource *models.Resource) {
	if resource == nil || resource.Kind != models.KindTorrent {
		return
	}
	if resource.InfoHash == "" && resource.SourceURL == nil && resource.TorrentPath == nil {
		return
	}

	now := time.Now()
	switch resource.TaskState {
	case models.TaskDownloading:
		m.rewriteState(resource, models.TaskPreparing)
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
		m.failResource(resource, ErrKeyNoSource)
		return
	}

	job := m.newJob(resource, magnet, torrentPath)
	if err := m.attachTorrent(job, spec); err != nil {
		m.markJobAttachFailed(job, classifyTorrentError(err))
		return
	}

	m.mu.Lock()
	m.jobsMap[resource.InfoHash] = job
	m.mu.Unlock()

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
func (m *TorrentManager) rewriteState(resource *models.Resource, to models.TaskState) {
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
	if _, err := m.ResourceDao.UpdateState(resource, fromVersion); err != nil {
		logger.Torrent.Warn("恢复任务状态失败", slog.String("info_hash", resource.InfoHash), slog.Any("err", err))
	}
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
		m.failResource(row, ErrKeyNoSource)
		return
	}

	job := m.newJob(row, magnet, torrentPath)
	if _, err := job.Apply(models.EvRetryDelayElapsed, models.EventPayload{Now: time.Now()}); err != nil {
		return
	}
	job.resetContext()
	if err := m.attachTorrent(job, spec); err != nil {
		m.markJobAttachFailed(job, classifyTorrentError(err))
		return
	}
	m.mu.Lock()
	m.jobsMap[row.InfoHash] = job
	m.mu.Unlock()
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
		if resource.LocalState != models.LocalPresent || resource.FileName == nil || resource.LocalPath == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(resource.LocalPath, *resource.FileName)); err != nil {
			m.applyResourceEvent(resource.InfoHash, models.EvFileLost, models.EventPayload{
				Now:        time.Now(),
				LossReason: models.LossNotFound,
			})
		}
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

// DownloadResources 按资源 ID 批量加入下载队列，用于种子资源页的多选下载。
func (m *TorrentManager) DownloadResources(ids []int64, priority int) (*BatchDownloadResult, error) {
	result := &BatchDownloadResult{}
	if len(ids) == 0 {
		return result, nil
	}

	rows, err := m.ResourceDao.GetByIDs(ids)
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

// enqueueResource 从数据库记录重新入队（继续 / 恢复 / 重试 / 修复）。
//
// e 为 0 时按当前状态自动选择事件：Failed → 手动重试、Cancelled → 重新下载、其余 → 新增下载。
func (m *TorrentManager) enqueueResource(resource *models.Resource, priority int, e models.Event) (string, bool, error) {
	if resource == nil {
		return "", false, errors.New(ErrKeyTaskNotFound)
	}
	if resource.Kind == models.KindLocal {
		return "", false, nil
	}
	if resource.TaskState == models.TaskCompleted && resource.LocalState == models.LocalPresent {
		return "", false, nil
	}
	if resource.TaskState.Active() {
		return "", false, nil
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
	return m.startJob(resource, spec, magnet, torrentPath, priority, e)
}

// enqueueSpec 把 TorrentSpec 转换成下载任务；已存在或已完成时直接跳过。
func (m *TorrentManager) enqueueSpec(spec *torrent.TorrentSpec, magnet, torrentPath string, priority int, e models.Event) (string, bool, error) {
	if spec == nil || spec.InfoHash.IsZero() {
		return "", false, errors.New(ErrKeyInvalidMagnet)
	}
	// 统一使用十六进制字符串，与爬虫记录和磁力链接中的 InfoHash 保持一致。
	infoHash := spec.InfoHash.HexString()

	// 内存中已有同一任务：只提升优先级，避免重复添加。
	m.mu.Lock()
	if existing := m.jobsMap[infoHash]; existing != nil {
		m.mu.Unlock()
		if existing.setPriority(priority) {
			if id := existing.id(); id != 0 {
				if err := m.ResourceDao.UpdatePriority(id, priority); err != nil {
					logger.Torrent.Error("更新任务优先级失败", slog.String("info_hash", infoHash), slog.Any("err", err))
				}
			}
			m.emitState(existing)
		}
		return infoHash, false, nil
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
	} else if resource.TaskState == models.TaskCompleted && resource.LocalState == models.LocalPresent {
		// 已完成且本地文件齐全，无需重复下载
		return infoHash, false, nil
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
	if len(spec.InfoBytes) == 0 {
		if cached := readMetadataCache(baseDir, infoHash); cached != nil {
			cached.PeerAddrs = append(cached.PeerAddrs, spec.PeerAddrs...)
			cached.Sources = append(cached.Sources, spec.Sources...)
			cached.Webseeds = append(cached.Webseeds, spec.Webseeds...)
			spec = cached
		}
	}

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
		if err := m.ResourceDao.Save(resource); err != nil {
			return resource.InfoHash, false, err
		}
	} else {
		if _, err := m.ResourceDao.UpdateState(resource, fromVersion); err != nil {
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
	if len(spec.InfoBytes) == 0 {
		if cached := readMetadataCache(baseDir, infoHash); cached != nil {
			cached.PeerAddrs = append(cached.PeerAddrs, spec.PeerAddrs...)
			cached.Sources = append(cached.Sources, spec.Sources...)
			cached.Webseeds = append(cached.Webseeds, spec.Webseeds...)
			spec = cached
		}
	}
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

// id 返回资源主键。
func (job *TorrentJob) id() int64 {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.job == nil {
		return 0
	}
	return job.job.ID
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
	} {
		if strings.Contains(message, key) {
			return key
		}
	}
	return classifyTorrentError(err)
}
