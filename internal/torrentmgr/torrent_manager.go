package torrentmgr

import (
	"changeme/internal/config"
	"changeme/internal/db/dao"
	"changeme/internal/db/models"
	"changeme/internal/i18n"
	"changeme/internal/logger"
	"changeme/internal/utils/ip"
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
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
}

// 命名规范：{构造名}Params
type WailsAppParams struct {
	fx.In // 必须内嵌 fx.In，告诉fx这是参数结构体

	DB          *gorm.DB
	I18n        *i18n.I18nService
	ResourceDao *dao.ResourceDAO
	Cfg         *config.Config
}

func NewManager(cn WailsAppParams) (*TorrentManager, error) {
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
		return ""
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

	client, err := torrent.NewClient(c)
	if err != nil {
		panic(err)
	}
	manager := &TorrentManager{
		jobsMap:     make(map[string]*TorrentJob),
		client:      client,
		ResourceDao: cn.ResourceDao,
		cfg:         cn.Cfg,
		sem:         semaphore.NewWeighted(5),
	}
	return manager, nil
}

func (m *TorrentManager) Shutdown() error {
	if m.client != nil {
		m.client.Close()
	}
	return nil
}

func (m *TorrentManager) PauseJob(hashInfo string) error {
	m.mu.RLock()
	job := m.jobsMap[hashInfo]
	m.mu.RUnlock()
	if job == nil {
		return fmt.Errorf("任务不存在: %s", hashInfo)
	}
	job.Pause()
	return nil
}

func (m *TorrentManager) CancelJob(hashInfo string) error {
	m.mu.RLock()
	job, ok := m.jobsMap[hashInfo]
	m.mu.RUnlock()
	if !ok || job == nil {
		return fmt.Errorf("任务不存在: %s", hashInfo)
	}

	// 先触发取消
	job.CancelJob()

	m.mu.Lock()
	delete(m.jobsMap, hashInfo)
	m.mu.Unlock()
	return nil
}

// startJob 外部调用，尝试启动一个排队任务；仅当拿到信号量且状态为Queued才启动
// 【注意】调用startJob前，job必须已经存入jobsMap
func (m *TorrentManager) startJob(job *TorrentJob) {
	m.mu.RLock()
	status := job.Status
	if status != models.JobStatusQueued {
		logger.Torrent.Error("任务非排队状态，跳过启动", slog.Any("InfoHash", job.job.InfoHash), slog.String("status", string(status)))
		return
	}

	// 获取信号量，拿不到直接返回，等待现有任务结束后downloadNextJob调度
	if !m.sem.TryAcquire(1) {
		return
	}
	m.mu.RUnlock()

	// 单goroutine：下载 + 完成监听，不再开第二个handleFinished协程
	go func(j *TorrentJob) {
		// 任务结束无论成功失败取消，必须释放信号量
		defer func() {
			m.sem.Release(1)
			// 任务结束，调度下一个排队任务
			m.downloadNextJob()
		}()

		// 阻塞执行下载
		j.StartDownload()
		resource := j.job
		err := m.ResourceDao.Save(resource)
		if err != nil {
			logger.Torrent.Error("更新数据异常", slog.Any("err", err))
			return
		}
	}(job)
}

// downloadNextJob 寻找下一个Queued任务，调用startJob尝试启动
// 只负责找任务，不直接go StartDownload，统一交给startJob做信号量校验
func (m *TorrentManager) downloadNextJob() {
	m.mu.RLock()
	var nextJob *TorrentJob
	for _, j := range m.jobsMap {
		if j.Status == models.JobStatusQueued {
			nextJob = j
			break
		}
	}
	m.mu.RUnlock()

	if nextJob == nil {
		return
	}
	// 尝试启动，startJob内部会做TryAcquire信号量，拿不到直接返回
	m.startJob(nextJob)
}

func (m *TorrentManager) AddMagnet(magnet string) error {
	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil {
		return err
	}

	return m.newTorrent(spec, &magnet, nil)
}

func (m *TorrentManager) AddTorrent(filePath string) error {
	mi, err := metainfo.LoadFromFile(filePath)
	if err != nil {
		return err
	}
	spec := torrent.TorrentSpecFromMetaInfo(mi)
	return m.newTorrent(spec, nil, &filePath)
}

func (m *TorrentManager) newTorrent(spec *torrent.TorrentSpec, magnet *string, filePath *string) error {
	infoHash := spec.InfoHash.AsString()
	now := time.Now()

	jobResource := &models.Resource{
		InfoHash:       infoHash,
		SourceURL:      magnet,
		TorrentPath:    filePath,
		LocalStatus:    models.LocalStatusNotDownloaded,
		DownloadStatus: models.JobStatus0,
		LocalPath:      m.cfg.DownloadDir,
		CreateTime:     &now,
		UpdateTime:     &now,
	}
	err := m.ResourceDao.Save(jobResource)
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &TorrentJob{
		torrent:       nil, // 后续加入torrent客户端后赋值
		job:           jobResource,
		retries:       0,
		priorityIndex: -1, // -1代表不指定特定文件，全部下载；>=0才指定文件索引
		lastBytes:     0,
		lastUpload:    0,
		lastSpeedAt:   time.Now(), // 测速初始时间戳设为当前
		downloadSpeed: 0,
		uploadSpeed:   0,
		Status:        jobResource.DownloadStatus, // 从数据库读取任务状态
		ctx:           ctx,
		cancel:        cancel,
		// mu:  sync.RWMutex零值，无需赋值
	}
	m.jobsMap[infoHash] = job
	baseDir := m.cfg.DownloadDir
	pc, err := storage.NewDefaultPieceCompletionForDir(baseDir)
	if err != nil {
		panic(err)
	}
	spec.Storage = storage.NewFileWithCompletion(baseDir, pc)
	tt, _, err := m.client.AddTorrentSpec(spec)
	if err != nil {
		return err
	}
	go func() {
		select {
		case <-job.ctx.Done():
			return
		case <-tt.GotInfo():
			// ✅拿到元信息，填充数据库模型
			mi := tt.Info()
			job.mu.Lock()
			{
				// 种子名称
				job.job.ResourceName = &mi.Name
				// 总大小
				total := mi.TotalLength()
				job.job.TotalSize = &total
				// 切换任务状态：获取元信息完成，进入排队/下载
				queuedStatus := models.JobStatusQueued
				job.Status = queuedStatus
				now := time.Now()
				job.job.UpdateTime = &now
				// ResourceName
				if job.job.ResourceName == nil {
					name := mi.Name
					job.job.ResourceName = &name
					job.job.UpdateTime = &now
				}
				// FileName
				if job.job.FileName == nil {
					for _, file := range tt.Files() {
						path := file.DisplayPath()
						ext := strings.ToLower(filepath.Ext(path))

						switch ext {
						case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv":
							fileName := filepath.Base(path)
							job.job.FileName = &fileName
							job.job.UpdateTime = &now
							break
						}
					}
				}

			}
			job.mu.Unlock()

			// 落库保存元信息
			_ = m.ResourceDao.Save(job.job)
			m.startJob(job)
			return
		case <-time.After(60 * 10 * time.Second):
			tt.Drop()
			return
		}
	}()
	return nil
}

func (m *TorrentManager) GetJob(info string) (*TorrentJob, error) {
	m.mu.RLock()
	job := m.jobsMap[info]
	m.mu.RUnlock()
	if job == nil {
		return nil, fmt.Errorf("任务不存在: %s", info)
	}
	return job, nil
}
