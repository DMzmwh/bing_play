package torrentmgr

import (
	"changeme/internal/db/models"
	"context"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

// 下载任务
type TorrentJob struct {
	torrent       *torrent.Torrent // anacrolix/torrent内存种子句柄，管理peer、分片、下载运行状态，仅内存有效
	job           models.Resource  // 数据库持久化任务模型，保存任务配置、infohash、保存路径等落库信息
	retries       int              // 任务失败重试计数，到达阈值标记任务失败
	priorityIndex int              // 流式下载目标文件索引；>=0优先下载指定序号文件用于边下边播
	lastBytes     int64            // 上一次测速采样时刻的累计已下载字节，速度计算基准快照
	lastUpload    int64            // 上一次测速采样时刻的累计已上传字节，上传速度计算基准快照
	lastSpeedAt   time.Time        // 上一次执行速度计算的时间戳，配合字节快照计算时间差
	downloadSpeed int64            // 瞬时下载速度，单位B/s，业务层采样计算得出
	uploadSpeed   int64            // 瞬时上传做种速度，单位B/s，业务层采样计算得出
	Status        JobStatus
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.RWMutex
}

func (job *TorrentJob) StartDownload() {
	defer job.cancel()

	//开始下载后
	job.mu.Lock()
	//下载逻辑
	job.Status = StatusDownloading
	job.mu.Unlock()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			job.monitorProgress()
		case <-job.ctx.Done():
			return
		case <-job.torrent.Complete().On():
			job.Status = StatusCompleted
			return
		}
	}
}

func (job *TorrentJob) monitorProgress() {

}

func (job *TorrentJob) Pause() {
	job.cancel()
}
