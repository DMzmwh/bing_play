package torrentmgr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/filetype"
	"github.com/bing/bing-play/internal/logger"
	"github.com/bing/bing-play/internal/torrentmgr/models/vo"

	"github.com/anacrolix/torrent"
)

// videoExtensions 可识别的视频扩展名，用于挑选主文件与资源类型判定。
var videoExtensions = map[string]struct{}{
	".mp4":  {},
	".mkv":  {},
	".avi":  {},
	".mov":  {},
	".webm": {},
	".flv":  {},
}

// isVideoPath 判断文件是否为视频文件。
func isVideoPath(path string) bool {
	_, ok := videoExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

// 下载任务
type TorrentJob struct {
	torrent       *torrent.Torrent // anacrolix/torrent内存种子句柄，管理peer、分片、下载运行状态，仅内存有效
	job           *models.Resource // 数据库持久化任务模型，保存任务配置、infohash、保存路径等落库信息
	retries       int              // 任务失败重试计数，到达阈值标记任务失败
	priorityIndex int              // 流式下载目标文件索引；>=0优先下载指定序号文件用于边下边播
	magnet        string           // 原始磁力链接，用于失败后重新拉取元信息
	torrentPath   string           // 本地 .torrent 文件路径，与 magnet 二选一
	errorKey      string           // 最近一次失败原因的 i18n key

	lastBytes     int64     // 上一次测速采样时刻的累计已下载字节，速度计算基准快照
	lastUpload    int64     // 上一次测速采样时刻的累计已上传字节，上传速度计算基准快照
	lastCompleted int64     // 上一次采样时已完成字节，用于判断下载是否停滞
	lastSpeedAt   time.Time // 上一次执行速度计算的时间戳，配合字节快照计算时间差
	lastProgress  time.Time // 上一次"有新增数据"的时间戳，用于停滞检测
	lastPersistAt time.Time // 上一次进度落库时间

	downloadSpeed int64   // 瞬时下载速度，单位B/s，业务层采样计算得出
	uploadSpeed   int64   // 瞬时上传做种速度，单位B/s，业务层采样计算得出
	progress      float64 // 下载进度，范围 0-100
	downloaded    int64   // 已下载字节数
	etaSeconds    int64   // 预计剩余秒数，-1 表示无法估算

	progressInterval time.Duration // 进度采样间隔
	persistInterval  time.Duration // 进度落库间隔
	stallTimeout     time.Duration // 无进展判定超时

	notify  func(*vo.TorrentJobVO) // 进度 / 完成时推送事件
	persist func(*models.Resource) // 把进度快照写入数据库

	Status models.JobStatus
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// bind 注入运行参数与回调，由管理人在创建任务时调用。
func (job *TorrentJob) bind(cfg DownloadConfig, notify func(*vo.TorrentJobVO), persist func(*models.Resource)) {
	job.mu.Lock()
	defer job.mu.Unlock()

	job.progressInterval = cfg.ProgressInterval
	job.persistInterval = cfg.PersistInterval
	job.stallTimeout = cfg.StallTimeout
	job.notify = notify
	job.persist = persist
}

// StartDownload 执行下载并阻塞到任务结束，返回结束原因。
//
// 结束原因：
//   - outcomeCompleted：下载完成
//   - outcomePaused：用户暂停
//   - outcomeCancelled：用户取消
//   - outcomeStalled：长时间无下载进展，需要上层自动重试
func (job *TorrentJob) StartDownload() jobOutcome {
	job.mu.Lock()
	status := job.Status
	tt := job.torrent
	if status != models.JobStatusQueued && status != models.JobStatusDownloading {
		job.mu.Unlock()
		if status == models.JobStatusPaused {
			return outcomePaused
		}
		return outcomeCancelled
	}
	if tt == nil || tt.Info() == nil {
		job.mu.Unlock()
		return outcomeStalled
	}

	now := time.Now()
	job.Status = models.JobStatusDownloading
	job.job.DownloadStatus = models.JobStatusDownloading
	job.lastProgress = now
	job.lastSpeedAt = now
	job.lastPersistAt = now
	job.lastCompleted = tt.BytesCompleted()
	tt.AllowDataDownload()
	tt.DownloadAll()

	// 记录资源类型，供播放页与资源库使用
	files := tt.Files()
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path())
	}
	job.job.ResourceType = filetype.DetectResourceType(paths)
	interval := job.progressInterval
	job.mu.Unlock()
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if job.monitorProgress() {
				return outcomeStalled
			}
		case <-job.ctx.Done():
			job.mu.RLock()
			status := job.Status
			job.mu.RUnlock()
			if status == models.JobStatusPaused {
				return outcomePaused
			}
			return outcomeCancelled
		case <-tt.Complete().On():
			job.markCompleted(time.Now())
			return outcomeCompleted
		}
	}
}

// monitorProgress 采样一次进度并推送事件，返回 true 表示任务已停滞需要重试。
func (job *TorrentJob) monitorProgress() bool {
	job.mu.Lock()

	tt := job.torrent
	if tt == nil {
		job.mu.Unlock()
		return false
	}
	info := tt.Info()
	if info == nil {
		job.mu.Unlock()
		return false
	}

	total := info.TotalLength()
	if total <= 0 {
		job.mu.Unlock()
		return false
	}
	completed := tt.BytesCompleted()
	stats := tt.Stats()
	now := time.Now()

	// 速度按两次采样的差值计算，第一次只记录基准值
	if !job.lastSpeedAt.IsZero() {
		elapsed := now.Sub(job.lastSpeedAt).Seconds()
		if elapsed > 0 {
			job.downloadSpeed = int64(float64(stats.PeerConns.BytesReadData.Int64()-job.lastBytes) / elapsed)
			job.uploadSpeed = int64(float64(stats.PeerConns.BytesWrittenData.Int64()-job.lastUpload) / elapsed)
			if job.downloadSpeed < 0 {
				job.downloadSpeed = 0
			}
			if job.uploadSpeed < 0 {
				job.uploadSpeed = 0
			}
		}
	}
	job.lastBytes = stats.PeerConns.BytesReadData.Int64()
	job.lastUpload = stats.PeerConns.BytesWrittenData.Int64()
	job.lastSpeedAt = now

	job.downloaded = completed
	job.progress = float64(completed) / float64(total) * 100
	if job.progress > 100 {
		job.progress = 100
	}
	job.etaSeconds = estimateSeconds(total-completed, job.downloadSpeed)

	job.job.TotalSize = &total
	job.job.DownloadedSize = &completed
	job.job.UpdateTime = &now

	if completed > job.lastCompleted {
		job.lastCompleted = completed
		job.lastProgress = now
	}
	stalled := completed < total && job.stallTimeout > 0 && now.Sub(job.lastProgress) >= job.stallTimeout

	infoHash := job.job.InfoHash
	progress := job.progress
	speed := job.downloadSpeed
	upload := job.uploadSpeed
	peers := stats.ActivePeers

	snapshot := job.persistSnapshotLocked(now)
	payload := job.voLocked(now)
	job.mu.Unlock()

	if snapshot != nil {
		job.saveSnapshot(snapshot)
	}
	job.push(payload)

	logger.Torrent.Debug(fmt.Sprintf(
		infoHash+"进度: %.2f%% | 已完成: %.2f MB | 下载速度: %.2f MB/s | 上传速度: %.2f MB/s | Peer: %d\n",
		progress,
		float64(completed)/1024/1024,
		float64(speed)/1024/1024,
		float64(upload)/1024/1024,
		peers,
	))

	return stalled
}

// markCompleted 标记任务完成：校验文件完整性、持久化并推送最终进度。
// 分片哈希在下载过程中已由 torrent 引擎校验，这里再确认文件确实完整落盘。
func (job *TorrentJob) markCompleted(now time.Time) {
	job.mu.Lock()
	if job.Status == models.JobStatusCancelled || job.Status == models.JobStatusPaused {
		job.mu.Unlock()
		return
	}

	total := int64(0)
	completed := int64(0)
	if job.torrent != nil {
		if info := job.torrent.Info(); info != nil {
			total = info.TotalLength()
		}
		completed = job.torrent.BytesCompleted()
	}
	if total <= 0 && job.job.TotalSize != nil {
		total = *job.job.TotalSize
	}
	if completed < total {
		completed = total
	}

	baseDir := job.job.LocalPath
	check := verifyCompletedFiles(job.torrent, baseDir)

	status := models.JobStatusCompleted
	localStatus := models.LocalStatusExists
	errorKey := ""
	switch {
	case check.missing:
		status = models.JobStatusFailed
		localStatus = models.LocalStatusMissing
		errorKey = ErrKeyResourceGone
	case check.mismatch:
		status = models.JobStatusFailed
		localStatus = models.LocalStatusCorrupted
		errorKey = ErrKeyResourceGone
	}

	job.Status = status
	job.progress = 100
	job.etaSeconds = 0
	job.downloaded = completed
	job.errorKey = errorKey
	job.job.DownloadStatus = status
	job.job.DownloadedSize = &completed
	job.job.LocalStatus = localStatus
	job.job.UpdateTime = &now

	if check.fileName != "" && job.job.FileName == nil {
		job.job.FileName = &check.fileName
	}
	if check.fileSize > 0 {
		job.job.FileSize = &check.fileSize
	}

	if errorKey != "" {
		job.job.ErrorMessage = &errorKey
	} else {
		job.job.ErrorMessage = nil
		job.job.CompletedTime = &now
	}

	snapshot := job.snapshotResourceLocked()
	payload := job.voLocked(now)
	job.mu.Unlock()

	job.saveSnapshot(snapshot)
	job.push(payload)
}

// completionCheck 下载完成后的落盘校验结果。
type completionCheck struct {
	fileName string
	fileSize int64
	// missing 有文件不存在
	missing bool
	// mismatch 有文件大小与元信息不一致
	mismatch bool
}

// verifyCompletedFiles 校验 Torrent 文件是否完整落盘，并挑选体积最大的视频作为主文件。
// 分片内容由 torrent 引擎完成校验，这里只做文件级存在性与大小检查，开销很小。
func verifyCompletedFiles(tt *torrent.Torrent, baseDir string) completionCheck {
	var check completionCheck
	if tt == nil || baseDir == "" {
		return check
	}

	for _, file := range tt.Files() {
		path := filepath.Join(baseDir, file.Path())
		info, err := os.Stat(path)
		if err != nil {
			check.missing = true
			continue
		}
		if info.IsDir() {
			continue
		}
		if info.Size() != file.Length() {
			check.mismatch = true
			continue
		}
		if isVideoPath(file.Path()) && file.Length() > check.fileSize {
			check.fileName = filepath.Base(file.Path())
			check.fileSize = file.Length()
		}
	}

	return check
}

// Pause 暂停任务：停止数据下载并结束当前下载 goroutine，但保留 Torrent 句柄用于继续。
func (job *TorrentJob) Pause() {
	job.mu.Lock()
	switch job.Status {
	case models.JobStatusDownloadMetaInfo, models.JobStatusQueued, models.JobStatusDownloading:
	default:
		job.mu.Unlock()
		return
	}

	job.Status = models.JobStatusPaused
	job.job.DownloadStatus = models.JobStatusPaused
	now := time.Now()
	job.job.UpdateTime = &now
	tt := job.torrent
	cancel := job.cancel
	job.mu.Unlock()

	if tt != nil {
		tt.DisallowDataDownload()
	}
	if cancel != nil {
		cancel()
	}
}

// Cancel 取消任务：标记取消、终止 goroutine 并释放 Torrent。
func (job *TorrentJob) Cancel() {
	job.mu.Lock()
	if job.Status == models.JobStatusCompleted {
		job.mu.Unlock()
		return
	}
	job.Status = models.JobStatusCancelled
	job.job.DownloadStatus = models.JobStatusCancelled
	now := time.Now()
	job.job.UpdateTime = &now
	tt := job.torrent
	cancel := job.cancel
	job.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if tt != nil {
		tt.Drop()
	}
}

// shutdown 应用退出时终止任务 goroutine，不做状态落库。
func (job *TorrentJob) shutdown() {
	job.mu.RLock()
	cancel := job.cancel
	job.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// resetContext 为继续 / 重试创建新的生命周期上下文。
func (job *TorrentJob) resetContext() context.Context {
	job.mu.Lock()
	defer job.mu.Unlock()

	if job.cancel != nil {
		job.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.ctx = ctx
	job.cancel = cancel
	return ctx
}

// setTorrent 替换底层 Torrent 句柄。
func (job *TorrentJob) setTorrent(tt *torrent.Torrent) {
	job.mu.Lock()
	job.torrent = tt
	job.mu.Unlock()
}

// getTorrent 返回当前 Torrent 句柄。
func (job *TorrentJob) getTorrent() *torrent.Torrent {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.torrent
}

// getContext 返回当前生命周期上下文。
func (job *TorrentJob) getContext() context.Context {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.ctx
}

// setStatus 更新任务状态与持久化状态。
func (job *TorrentJob) setStatus(status models.JobStatus) {
	job.mu.Lock()
	job.Status = status
	job.job.DownloadStatus = status
	now := time.Now()
	job.job.UpdateTime = &now
	job.mu.Unlock()
}

// resetForRetry 把任务重置为"重新获取元信息"状态，并清空错误信息与重试次数。
func (job *TorrentJob) resetForRetry() {
	job.mu.Lock()
	defer job.mu.Unlock()

	job.retries = 0
	retry := 0
	job.errorKey = ""
	job.job.RetryCount = &retry
	job.job.ErrorMessage = nil
	job.Status = models.JobStatusDownloadMetaInfo
	job.job.DownloadStatus = models.JobStatusDownloadMetaInfo
	now := time.Now()
	job.job.UpdateTime = &now
}

// setFailureState 在重试 / 失败时记录错误原因与次数。
func (job *TorrentJob) setFailureState(status models.JobStatus, errorKey string, retries int) {
	job.mu.Lock()
	job.Status = status
	job.job.DownloadStatus = status
	job.retries = retries
	job.errorKey = errorKey
	job.job.RetryCount = &retries
	job.job.ErrorMessage = &errorKey
	now := time.Now()
	job.job.UpdateTime = &now
	job.mu.Unlock()
}

// setPriority 更新任务优先级，返回是否发生变化。
func (job *TorrentJob) setPriority(priority int) bool {
	job.mu.Lock()
	defer job.mu.Unlock()

	current := 0
	if job.job.Priority != nil {
		current = *job.job.Priority
	}
	if priority <= current {
		return false
	}
	job.job.Priority = &priority
	now := time.Now()
	job.job.UpdateTime = &now
	return true
}

// priorityValue 返回任务优先级，空值按 0 处理。
func (job *TorrentJob) priorityValue() int {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.job.Priority == nil {
		return 0
	}
	return *job.job.Priority
}

// forcePriority 直接设置任务优先级，允许提高或降低，返回是否发生变化。
func (job *TorrentJob) forcePriority(priority int) bool {
	job.mu.Lock()
	defer job.mu.Unlock()

	if job.job.Priority != nil && *job.job.Priority == priority {
		return false
	}
	job.job.Priority = &priority
	now := time.Now()
	job.job.UpdateTime = &now
	return true
}

// createTimeValue 返回任务创建时间，空值按零值处理。
func (job *TorrentJob) createTimeValue() time.Time {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.job.CreateTime == nil {
		return time.Time{}
	}
	return *job.job.CreateTime
}

// currentStatus 返回当前状态，供调度器读取。
func (job *TorrentJob) currentStatus() models.JobStatus {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.Status
}

// CurrentStatus 返回任务当前状态，供外部（如视频播放）判断是否可读取数据。
func (job *TorrentJob) CurrentStatus() models.JobStatus {
	return job.currentStatus()
}

// persistSnapshotLocked 到达落库间隔时返回资源快照；调用方需持有写锁。
func (job *TorrentJob) persistSnapshotLocked(now time.Time) *models.Resource {
	if job.persist == nil || job.persistInterval <= 0 {
		return nil
	}
	if now.Sub(job.lastPersistAt) < job.persistInterval {
		return nil
	}
	job.lastPersistAt = now
	return job.snapshotResourceLocked()
}

// snapshotResourceLocked 复制一份资源快照，避免落库时持有任务锁；调用方需持有读/写锁。
func (job *TorrentJob) snapshotResourceLocked() *models.Resource {
	if job.job == nil {
		return nil
	}
	cp := *job.job
	return &cp
}

// snapshotResource 对外提供资源快照。
func (job *TorrentJob) snapshotResource() *models.Resource {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.snapshotResourceLocked()
}

// saveSnapshot 把快照写入数据库，失败只记录日志，不阻断下载。
func (job *TorrentJob) saveSnapshot(resource *models.Resource) {
	if resource == nil || job.persist == nil {
		return
	}
	job.persist(resource)
}

// push 推送任务快照事件。
func (job *TorrentJob) push(payload *vo.TorrentJobVO) {
	if payload == nil || job.notify == nil {
		return
	}
	job.notify(payload)
}

// ToVO 返回任务快照，供列表与事件使用。
func (job *TorrentJob) ToVO() *vo.TorrentJobVO {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.voLocked(time.Now())
}

// voLocked 生成任务快照；调用方需持有读/写锁。
func (job *TorrentJob) voLocked(now time.Time) *vo.TorrentJobVO {
	payload := &vo.TorrentJobVO{
		Status:        job.Status,
		Progress:      job.progress,
		Downloaded:    job.downloaded,
		DownloadSpeed: job.downloadSpeed,
		UploadSpeed:   job.uploadSpeed,
		EtaSeconds:    job.etaSeconds,
		Priority:      job.priorityValueLocked(),
		RetryCount:    job.retries,
		ErrorMessage:  job.errorKey,
	}

	if job.job != nil {
		payload.InfoHash = job.job.InfoHash
		payload.SavePath = job.job.LocalPath
		if job.job.ResourceName != nil {
			payload.Name = *job.job.ResourceName
		}
		if job.job.FileName != nil {
			payload.FileName = *job.job.FileName
		}
		if job.job.RetryCount != nil {
			payload.RetryCount = *job.job.RetryCount
		}
		if job.job.ErrorMessage != nil && payload.ErrorMessage == "" {
			payload.ErrorMessage = *job.job.ErrorMessage
		}
		if job.job.TotalSize != nil {
			payload.TotalSize = *job.job.TotalSize
		}
		if job.job.DownloadedSize != nil && payload.Downloaded <= 0 {
			payload.Downloaded = *job.job.DownloadedSize
		}
		if job.job.CreateTime != nil {
			payload.CreatedTime = job.job.CreateTime.Format(time.DateTime)
		}
		if job.job.CompletedTime != nil {
			payload.CompletedTime = job.job.CompletedTime.Format(time.DateTime)
		}
	}

	if job.torrent != nil {
		if info := job.torrent.Info(); info != nil {
			payload.TotalSize = info.TotalLength()
		}
		payload.Downloaded = job.torrent.BytesCompleted()
		payload.Peers = job.torrent.Stats().ActivePeers
	}

	if payload.TotalSize > 0 {
		payload.Progress = float64(payload.Downloaded) / float64(payload.TotalSize) * 100
		if payload.Progress > 100 {
			payload.Progress = 100
		}
		if payload.Downloaded >= payload.TotalSize {
			payload.Progress = 100
			payload.EtaSeconds = 0
		} else if payload.Progress <= 0 && job.progress > 0 {
			payload.Progress = job.progress
		}
	}

	if payload.EtaSeconds == 0 && payload.Downloaded < payload.TotalSize {
		payload.EtaSeconds = estimateSeconds(payload.TotalSize-payload.Downloaded, payload.DownloadSpeed)
	}

	if payload.Status == models.JobStatusCompleted {
		payload.Progress = 100
		payload.EtaSeconds = 0
	}

	return payload
}

// priorityValueLocked 读取优先级，调用方需持有锁。
func (job *TorrentJob) priorityValueLocked() int {
	if job.job == nil || job.job.Priority == nil {
		return 0
	}
	return *job.job.Priority
}

// estimateSeconds 根据剩余字节与速度估算剩余秒数，无法估算时返回 -1。
func estimateSeconds(remaining, speed int64) int64 {
	if remaining <= 0 {
		return 0
	}
	if speed <= 0 {
		return -1
	}
	seconds := remaining / speed
	if remaining%speed != 0 {
		seconds++
	}
	return seconds
}

// GetTorrent 导出方法，外部包调用
func (j *TorrentJob) GetTorrent() *torrent.Torrent {
	return j.getTorrent()
}

// GetFile 根据文件路径查找 Torrent 内的文件；path 为空时返回第一个文件。
func (j *TorrentJob) GetFile(path string) *torrent.File {
	t := j.getTorrent()
	if t == nil {
		return nil
	}
	files := t.Files()
	if len(files) == 0 {
		return nil
	}
	if path == "" {
		return files[0]
	}
	for _, f := range files {
		if f.Path() == path {
			return f
		}
	}
	// 调用方通常只持有文件名（如资源记录的 FileName），这里按基础名再匹配一次
	for _, f := range files {
		if filepath.Base(f.Path()) == path {
			return f
		}
	}
	return nil
}
