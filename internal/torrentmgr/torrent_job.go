package torrentmgr

import (
	"context"
	"fmt"
	"log/slog"
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
	priorityIndex int              // 流式下载目标文件索引；>=0优先下载指定序号文件用于边下边播
	magnet        string           // 原始磁力链接，用于失败后重新拉取元信息
	torrentPath   string           // 本地 .torrent 文件路径，与 magnet 二选一

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

	notify      func(*vo.TorrentJobVO)              // 进度 / 完成时推送事件
	persist     func(*models.Resource, uint64) bool // CAS 落库快照，返回是否写入成功
	clearPieces func(*TorrentJob)                   // 清理该任务的分片完成记录

	// persistedVersion 最近一次成功写入数据库的 state_version。
	// 仅当 CAS 写入成功后才推进，保证陈旧快照不会覆盖更新的状态。
	persistedVersion uint64

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// bind 注入运行参数与回调，由管理人在创建任务时调用。
func (job *TorrentJob) bind(
	cfg DownloadConfig,
	notify func(*vo.TorrentJobVO),
	persist func(*models.Resource, uint64) bool,
	clearPieces func(*TorrentJob),
) {
	job.mu.Lock()
	defer job.mu.Unlock()

	job.progressInterval = cfg.ProgressInterval
	job.persistInterval = cfg.PersistInterval
	job.stallTimeout = cfg.StallTimeout
	job.notify = notify
	job.persist = persist
	job.clearPieces = clearPieces
	job.persistedVersion = job.job.StateVersion
}

// Apply 在任务上执行一次状态机流转，并按规则完成落库 / 推送 / 释放资源等副作用。
func (job *TorrentJob) Apply(e models.Event, p models.EventPayload) (models.Result, error) {
	return job.apply(e, p)
}

// apply 执行流转的内部实现；调用方不得持有任务锁。
func (job *TorrentJob) apply(e models.Event, p models.EventPayload) (models.Result, error) {
	job.mu.Lock()
	if job.job == nil {
		job.mu.Unlock()
		return models.Result{}, models.ErrInvalidTransition
	}
	if p.Now.IsZero() {
		p.Now = time.Now()
	}
	res, err := job.job.ApplyEvent(e, p)
	if err != nil {
		job.mu.Unlock()
		return res, err
	}

	now := p.Now
	job.job.UpdateTime = &now

	fromVersion := job.persistedVersion
	var snapshot *models.Resource
	if res.Effects&models.EffectPersist != 0 {
		snapshot = job.snapshotResourceLocked()
	}
	var payload *vo.TorrentJobVO
	if res.Effects&models.EffectEmit != 0 {
		payload = job.voLocked(now)
	}
	tt := job.torrent
	cancel := job.cancel
	job.mu.Unlock()

	if snapshot != nil {
		job.saveSnapshot(snapshot, fromVersion)
	}
	if res.Effects&models.EffectClearPieces != 0 && job.clearPieces != nil {
		job.clearPieces(job)
	}
	if res.Effects&models.EffectStopContext != 0 && cancel != nil {
		cancel()
	}
	if res.Effects&models.EffectDropTorrent != 0 && tt != nil {
		tt.Drop()
	}
	job.push(payload)
	return res, nil
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
	state := job.job.TaskState
	tt := job.torrent
	if state != models.TaskQueued && state != models.TaskDownloading {
		job.mu.Unlock()
		if state == models.TaskPaused {
			return outcomePaused
		}
		return outcomeCancelled
	}
	if tt == nil || tt.Info() == nil {
		job.mu.Unlock()
		return outcomeStalled
	}

	now := time.Now()
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
			if job.currentState() == models.TaskPaused {
				return outcomePaused
			}
			return outcomeCancelled
		case <-tt.Complete().On():
			job.markCompleted(time.Now())
			return outcomeCompleted
		}
	}
}

// monitorProgress 采样一次进度、推送事件并按落库间隔持久化，返回 true 表示任务已停滞需要重试。
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

	if completed > job.lastCompleted {
		job.lastCompleted = completed
		job.lastProgress = now
	}
	stalled := completed < total && job.stallTimeout > 0 && now.Sub(job.lastProgress) >= job.stallTimeout

	shouldPersist := job.persist != nil && job.persistInterval > 0 && now.Sub(job.lastPersistAt) >= job.persistInterval
	if shouldPersist {
		job.lastPersistAt = now
	}
	infoHash := job.job.InfoHash
	progress := job.progress
	speed := job.downloadSpeed
	upload := job.uploadSpeed
	peers := stats.ActivePeers
	job.mu.Unlock()

	if shouldPersist && !stalled {
		// 进度快照不推进状态版本；CAS 失败即视为陈旧快照丢弃。
		job.apply(models.EvProgress, models.EventPayload{Now: now})
	}

	job.push(job.ToVO())

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

// markCompleted 标记下载完成并校验落盘结果。
// 分片哈希在下载过程中已由 torrent 引擎校验，这里再确认文件确实完整落盘。
func (job *TorrentJob) markCompleted(now time.Time) {
	if job.currentState() != models.TaskDownloading {
		return
	}

	total := int64(0)
	completed := int64(0)
	if tt := job.getTorrent(); tt != nil {
		if info := tt.Info(); info != nil {
			total = info.TotalLength()
		}
		completed = tt.BytesCompleted()
	}
	if total <= 0 {
		job.mu.Lock()
		if job.job.TotalSize != nil {
			total = *job.job.TotalSize
		}
		job.mu.Unlock()
	}
	if completed < total {
		completed = total
	}

	job.mu.Lock()
	job.progress = 100
	job.etaSeconds = 0
	job.downloaded = completed
	if total > 0 {
		job.job.TotalSize = &total
	}
	job.job.DownloadedSize = &completed
	baseDir := job.job.LocalPath
	job.mu.Unlock()

	// 下载完成：Downloading -> Verifying。
	if _, err := job.apply(models.EvDownloadComplete, models.EventPayload{Now: now}); err != nil {
		logger.Torrent.Warn("下载完成流转失败",
			slog.String("info_hash", job.infoHash()),
			slog.Any("err", err),
		)
		return
	}

	check := verifyCompletedFiles(job.getTorrent(), baseDir)

	job.mu.Lock()
	if check.fileName != "" && job.job.FileName == nil {
		job.job.FileName = &check.fileName
	}
	if check.fileSize > 0 {
		job.job.FileSize = &check.fileSize
	}
	job.job.UpdateTime = &now
	job.mu.Unlock()

	// 校验结果：Verifying -> Completed / Failed。
	switch {
	case check.missing:
		job.apply(models.EvVerifyMissing, models.EventPayload{
			Now:        now,
			ErrorCode:  ErrKeyIntegrity,
			LossReason: models.LossNotFound,
		})
	case check.mismatch:
		job.apply(models.EvVerifyCorrupted, models.EventPayload{
			Now:        now,
			ErrorCode:  ErrKeyIntegrity,
			LossReason: models.LossSizeMismatch,
		})
	default:
		job.apply(models.EvVerifyOK, models.EventPayload{Now: now})
	}
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
			continue
		}
	}

	return check
}

// Pause 暂停任务：停止数据下载并结束当前下载 goroutine，但保留 Torrent 句柄用于继续。
func (job *TorrentJob) Pause() error {
	if _, err := job.apply(models.EvPause, models.EventPayload{Now: time.Now()}); err != nil {
		return err
	}
	if tt := job.getTorrent(); tt != nil {
		tt.DisallowDataDownload()
	}
	return nil
}

// Cancel 取消任务：标记取消、终止 goroutine 并释放 Torrent。
func (job *TorrentJob) Cancel() error {
	_, err := job.apply(models.EvCancel, models.EventPayload{Now: time.Now()})
	return err
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

// metadataReady 判断元信息是否已就绪。
func (job *TorrentJob) metadataReady() bool {
	tt := job.getTorrent()
	return tt != nil && tt.Info() != nil
}

// setPriority 提升任务优先级，返回是否发生变化；不允许降低。
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
	return job.priorityValueLocked()
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

// currentState 返回当前任务状态，供调度器读取。
func (job *TorrentJob) currentState() models.TaskState {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.job.TaskState
}

// CurrentState 返回任务当前状态，供外部（如视频播放）判断是否可读取数据。
func (job *TorrentJob) CurrentState() models.TaskState {
	return job.currentState()
}

// CurrentLocalState 返回本地文件可用性状态。
func (job *TorrentJob) CurrentLocalState() models.LocalState {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.job.LocalState
}

// infoHash 返回任务 InfoHash。
func (job *TorrentJob) infoHash() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.job == nil {
		return ""
	}
	return job.job.InfoHash
}

// snapshotResource 对外提供资源快照。
func (job *TorrentJob) snapshotResource() *models.Resource {
	job.mu.RLock()
	defer job.mu.RUnlock()
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

// saveSnapshot 以 CAS 方式写入数据库，失败只记录日志，不阻断下载。
func (job *TorrentJob) saveSnapshot(resource *models.Resource, fromVersion uint64) bool {
	if resource == nil || job.persist == nil {
		return false
	}
	ok := job.persist(resource, fromVersion)
	if ok {
		job.mu.Lock()
		if resource.StateVersion > job.persistedVersion {
			job.persistedVersion = resource.StateVersion
		}
		job.mu.Unlock()
	}
	return ok
}

// syncPersistedVersion 在 CAS 失败后同步数据库中的最新版本，避免后续写入持续冲突。
func (job *TorrentJob) syncPersistedVersion(version uint64) {
	job.mu.Lock()
	job.persistedVersion = version
	job.mu.Unlock()
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
		Progress:      job.progress,
		Downloaded:    job.downloaded,
		DownloadSpeed: job.downloadSpeed,
		UploadSpeed:   job.uploadSpeed,
		EtaSeconds:    job.etaSeconds,
		Priority:      job.priorityValueLocked(),
	}

	if job.job != nil {
		payload.InfoHash = job.job.InfoHash
		payload.TaskState = job.job.TaskState.String()
		payload.LocalState = job.job.LocalState.String()
		payload.DisplayState = job.job.Display().String()
		payload.SavePath = job.job.LocalPath
		payload.Attempt = int(job.job.Attempt)
		payload.MaxAttempts = int(job.job.MaxAttempts)
		payload.ErrorCode = job.job.ErrorCode
		if job.job.ResourceName != nil {
			payload.Name = *job.job.ResourceName
		}
		if job.job.FileName != nil {
			payload.FileName = *job.job.FileName
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
		if job.job.VerifiedAt != nil {
			payload.VerifiedAt = job.job.VerifiedAt.Format(time.DateTime)
		}
		if job.job.NextRetryAt != nil {
			payload.NextRetryAt = job.job.NextRetryAt.Format(time.DateTime)
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

	if job.job != nil && job.job.TaskState == models.TaskCompleted {
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
