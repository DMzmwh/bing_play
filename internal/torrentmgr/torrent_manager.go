package torrentmgr

import (
	"changeme/internal/container"
	"sync"

	"github.com/anacrolix/torrent"
	"golang.org/x/sync/semaphore"
)

// 下载管理器
type TorrentManager struct {
	//读写分离锁 读写分离锁，读读不互斥
	mu      sync.RWMutex
	jobsMap map[string]*TorrentJob
	client  *torrent.Client

	maxDownload int64
	maxGotInfo  int64
	sem         *semaphore.Weighted
}

func NewManager(cn *container.Container) (*TorrentManager, error) {
	manager := &TorrentManager{
		jobsMap:     make(map[string]*TorrentJob),
		maxDownload: 5,
	}
	return manager, nil
}

func (m *TorrentManager) startJob(job *TorrentJob) {
	if !m.sem.TryAcquire(1) {
		return
	}

	m.mu.Lock()
	m.jobsMap[job.job.ID] = job
	m.mu.Unlock()

	job.StartDownload()
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
		if job.Status == StatusQueued {
			job.StartDownload()
			return
		}
	}
}
