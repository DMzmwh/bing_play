package torrentmgr

import (
	"changeme/internal/container"
	"changeme/internal/db/models"
	"changeme/internal/logger"
	"changeme/internal/utils/ip"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
	"golang.org/x/sync/semaphore"
)

// 下载管理器
type TorrentManager struct {
	//读写分离锁 读写分离锁，读读不互斥
	mu      sync.RWMutex
	jobsMap map[string]*TorrentJob
	client  *torrent.Client
	sem     *semaphore.Weighted
}

func NewManager(cn *container.Container) (*TorrentManager, error) {
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
		jobsMap: make(map[string]*TorrentJob),
		client:  client,
		sem:     semaphore.NewWeighted(5),
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
	if job == nil {
		return fmt.Errorf("任务不存在: %s", hashInfo)
	}
	m.mu.RUnlock()
	job.Pause()
	return nil
}

func (m *TorrentManager) CancelJob(hashInfo string) error {
	m.mu.Lock()
	job, ok := m.jobsMap[hashInfo]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在: %s", hashInfo)
	}
	if job == nil {
		return fmt.Errorf("任务不存在: %s", hashInfo)
	}
	delete(m.jobsMap, hashInfo)
	m.mu.Unlock()
	job.CancelJob()
	return nil
}

func (m *TorrentManager) startJob(job *TorrentJob) {
	if job.Status != models.JobStatusQueued {
		logger.Torrent.Error("还没有获取到mate信息", slog.Any("InfoHash", job.job.InfoHash))
		return
	}
	if !m.sem.TryAcquire(1) {
		return
	}

	m.mu.Lock()
	m.jobsMap[job.job.InfoHash] = job
	m.mu.Unlock()

	go job.StartDownload()
	go m.handleFinished(job)
}

/*
监听下载执行完成
*/
func (m *TorrentManager) handleFinished(job *TorrentJob) {
	select {
	case <-job.ctx.Done():
		//取消 或者 下载完成等
		m.downloadNextJob()
		return
	}
}

// 下载下一个
func (m *TorrentManager) downloadNextJob() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobsMap {
		if job.Status == models.JobStatusQueued {
			go job.StartDownload()
			return
		}
	}
	m.sem.Release(1)
}
