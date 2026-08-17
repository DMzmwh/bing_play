package torrentmgr

import (
	"changeme/internal/db/models"
	"changeme/internal/logger"
	"context"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

// 下载任务
type TorrentJob struct {
	torrent       *torrent.Torrent // anacrolix/torrent内存种子句柄，管理peer、分片、下载运行状态，仅内存有效
	job           *models.Resource // 数据库持久化任务模型，保存任务配置、infohash、保存路径等落库信息
	retries       int              // 任务失败重试计数，到达阈值标记任务失败
	priorityIndex int              // 流式下载目标文件索引；>=0优先下载指定序号文件用于边下边播
	lastBytes     int64            // 上一次测速采样时刻的累计已下载字节，速度计算基准快照
	lastUpload    int64            // 上一次测速采样时刻的累计已上传字节，上传速度计算基准快照
	lastSpeedAt   time.Time        // 上一次执行速度计算的时间戳，配合字节快照计算时间差
	downloadSpeed int64            // 瞬时下载速度，单位B/s，业务层采样计算得出
	uploadSpeed   int64            // 瞬时上传做种速度，单位B/s，业务层采样计算得出
	Status        models.JobStatus
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.RWMutex
}

func (job *TorrentJob) StartDownload() {
	defer job.cancel()

	job.mu.RLock()
	if job.Status != models.JobStatusQueued {
		return
	}
	job.mu.RUnlock()

	//下载逻辑
	if job.torrent.Info() != nil {
		//开始下载后
		job.mu.Lock()
		job.torrent.AllowDataDownload()
		job.torrent.DownloadAll()
		job.Status = models.JobStatusDownloading
		job.mu.Unlock()
	} else {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			job.monitorProgress()
		case <-job.ctx.Done():
			return
		case <-job.torrent.Complete().On():
			job.Status = models.JobStatusCompleted
			job.job.LocalStatus = models.LocalStatusExists
			return
		}
	}
}

func (job *TorrentJob) monitorProgress() {
	t := job.torrent
	if t == nil || t.Info() == nil {
		return
	}
	info := t.Info()
	total := info.TotalLength()
	completed := t.BytesCompleted()

	// 计算下载进度
	var progress float64
	if total > 0 {
		progress = float64(completed) / float64(total) * 100
	} else {
		return
	}

	stats := t.Stats()
	// 当前累计下载数据
	currentBytes := stats.PeerConns.BytesReadData.Int64()
	// 当前累计上传数据
	currentUpload := stats.PeerConns.BytesWrittenData.Int64()

	now := time.Now()

	// 第一次采样只记录基准值，不计算速度
	if !job.lastSpeedAt.IsZero() {
		elapsed := now.Sub(job.lastSpeedAt).Seconds()

		if elapsed > 0 {
			job.downloadSpeed = int64(
				float64(currentBytes-job.lastBytes) / elapsed,
			)

			job.uploadSpeed = int64(
				float64(currentUpload-job.lastUpload) / elapsed,
			)
		}
	}

	// 更新测速快照
	job.lastBytes = currentBytes
	job.lastUpload = currentUpload
	job.lastSpeedAt = now
	//job.job.TotalSize = total//静态数据，不会变化

	// 更新 Resource 运行时状态
	// =========================================================
	// 完成判断
	// =========================================================
	if completed >= total {
		job.Status = models.JobStatusCompleted
		job.job.DownloadStatus = models.JobStatusCompleted
		// 下载完成时间
		completedTime := now
		job.job.CompletedTime = &completedTime
		job.job.UpdateTime = &now
		// 下载完成后，本地文件认为已经存在
		job.job.LocalStatus = models.LocalStatusExists
	}

	// =========================================================
	// Resource 更新时间
	// =========================================================

	job.job.UpdateTime = &now

	logger.Torrent.Debug(
		job.job.InfoHash+"进度: %.2f%% | 已完成: %.2f MB | 下载速度: %.2f MB/s | 上传速度: %.2f MB/s | Peer: %d\n",
		progress,
		float64(completed)/1024/1024,
		float64(job.downloadSpeed)/1024/1024,
		float64(job.uploadSpeed)/1024/1024,
		stats.ActivePeers,
	)
}

func (job *TorrentJob) Pause() {
	job.mu.Lock()
	defer func() {
		job.cancel()
		job.mu.Unlock()
	}()
	job.torrent.DisallowDataDownload()
	job.Status = models.JobStatusPaused
}

func (job *TorrentJob) CancelJob() {
	job.mu.Lock()
	defer func() {
		job.cancel()
		job.mu.Unlock()
	}()
	job.Status = models.JobStatus0
	if job.torrent != nil {
		job.torrent.Drop()
	}
}
