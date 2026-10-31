package torrentmgr

import (
	"strings"
	"time"

	"github.com/bing/bing-play/internal/torrentmgr/models/vo"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// 下载任务状态机（models.TaskState）流转说明：
//
//	discovered -> preparing -> queued -> downloading -> verifying -> completed
//	                 |           ^          |
//	                 |           |          +-> paused -> queued（继续）
//	                 |           |          +-> retry_wait（自动重试）-> preparing
//	                 |           |          +-> failed（重试次数耗尽后）
//	                 +-> failed（来源不可解析 / 元信息拉取失败耗尽重试）
//	任意活跃状态 -> cancelled（用户取消，任务保留在数据库中）
//
// 事件说明：
//   - EventDownloadProgress：下载中每秒推送一次任务进度、速度与剩余时间。
//   - EventDownloadStateChanged：状态发生变更（入队、开始、暂停、继续、失败、完成、取消）时推送。
//
// 两个事件都使用 *vo.TorrentJobVO 作为载荷，前端按 infoHash 更新对应行。
const (
	// EventDownloadProgress 下载进度事件名，需与前端保持一致。
	EventDownloadProgress = "download:progress"
	// EventDownloadStateChanged 下载状态变更事件名，需与前端保持一致。
	EventDownloadStateChanged = "download:state-changed"
)

// 下载模块返回给前端的错误 / 提示文案 key，统一由语言包翻译。
const (
	ErrKeyTaskNotFound    = "download.error.task_not_found"
	ErrKeyMagnetEmpty     = "download.error.magnet_empty"
	ErrKeyInvalidMagnet   = "download.error.invalid_magnet"
	ErrKeyNoSource        = "download.error.no_source"
	ErrKeyMetadataTimeout = "download.error.metadata_timeout"
	ErrKeyStalled         = "download.error.stalled"
	ErrKeyDownloadFailed  = "download.error.download_failed"
	ErrKeyPermission      = "download.error.permission"
	ErrKeyNoSpace         = "download.error.no_space"
	ErrKeyResourceGone    = "download.error.resource_gone"
	ErrKeyNetwork         = "download.error.network"
	// ErrKeyIntegrity 下载完成后的落盘校验未通过。
	ErrKeyIntegrity = "download.error.integrity"
	// ErrKeyCancelTooLate 已完成校验，无法再取消。
	ErrKeyCancelTooLate = "download.error.cancel_too_late"
	// ErrKeyInvalidTransition 当前状态不允许执行该操作。
	ErrKeyInvalidTransition = "download.error.invalid_transition"
	// ErrKeyRepairNotNeeded 本地文件完好，无需修复。
	ErrKeyRepairNotNeeded = "download.error.repair_not_needed"
	// ErrKeyDeleteNotAllowed 当前状态不允许删除本地文件。
	ErrKeyDeleteNotAllowed = "download.error.delete_not_allowed"
)

// DownloadConfig 下载模块运行参数，集中定义便于后续扩展为设置项。
//
// 字段说明：
//   - MaxConcurrent：同时下载的任务数，超出部分按优先级排队。
//   - MaxRetries：失败后自动重试的最大次数，超过后任务标记为 failed。
//   - MetadataTimeout：单个任务获取 Torrent 元信息的最长等待时间。
//   - RetryDelay：自动重试前的等待时间，避免网络抖动时立即重连。
//   - ProgressInterval：进度采样与事件推送间隔。
//   - PersistInterval：下载进度落库间隔，降低 SQLite 写入频率。
//   - StallTimeout：下载中长时间没有新增数据时判定为卡住并触发重试。
type DownloadConfig struct {
	MaxConcurrent    int           `json:"maxConcurrent"`
	MaxRetries       int           `json:"maxRetries"`
	MetadataTimeout  time.Duration `json:"metadataTimeout"`
	RetryDelay       time.Duration `json:"retryDelay"`
	ProgressInterval time.Duration `json:"progressInterval"`
	PersistInterval  time.Duration `json:"persistInterval"`
	StallTimeout     time.Duration `json:"stallTimeout"`
}

// DefaultDownloadConfig 返回默认下载参数。
func DefaultDownloadConfig() DownloadConfig {
	return DownloadConfig{
		MaxConcurrent:    5,
		MaxRetries:       3,
		MetadataTimeout:  10 * time.Minute,
		RetryDelay:       30 * time.Second,
		ProgressInterval: time.Second,
		PersistInterval:  10 * time.Second,
		StallTimeout:     3 * time.Minute,
	}
}

// BatchDownloadResult 批量入队结果，供前端提示新增/跳过/失败数量。
type BatchDownloadResult struct {
	// Added 成功加入下载队列的任务数。
	Added int `json:"added"`
	// Skipped 已存在、已完成或重复提交而跳过的任务数。
	Skipped int `json:"skipped"`
	// Failed 入队失败的任务数。
	Failed int `json:"failed"`
	// Errors 失败原因（i18n key），便于前端展示与排查。
	Errors []string `json:"errors"`
}

// addBatchError 记录一条失败原因。
func (r *BatchDownloadResult) addBatchError(key string) {
	r.Failed++
	if key == "" {
		key = ErrKeyDownloadFailed
	}
	for _, item := range r.Errors {
		if item == key {
			return
		}
	}
	r.Errors = append(r.Errors, key)
}

// jobOutcome 下载 goroutine 的退出原因。
type jobOutcome int

const (
	// outcomeCompleted 下载完成。
	outcomeCompleted jobOutcome = iota
	// outcomePaused 用户暂停导致退出。
	outcomePaused
	// outcomeCancelled 用户取消导致退出。
	outcomeCancelled
	// outcomeStalled 长时间无进展，需要自动重试。
	outcomeStalled
)

// emitDownloadEvent 向所有窗口广播下载事件。
// 应用尚未创建（例如单元测试）时静默跳过，不影响业务逻辑。
func emitDownloadEvent(name string, payload *vo.TorrentJobVO) {
	if payload == nil {
		return
	}
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(name, payload)
}

// classifyTorrentError 把底层错误归类为语言包 key，便于前端给出可读提示。
func classifyTorrentError(err error) string {
	if err == nil {
		return ""
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "permission"),
		strings.Contains(message, "access is denied"),
		strings.Contains(message, "operation not permitted"):
		return ErrKeyPermission
	case strings.Contains(message, "no space"),
		strings.Contains(message, "disk full"),
		strings.Contains(message, "not enough space"):
		return ErrKeyNoSpace
	case strings.Contains(message, "no such file"),
		strings.Contains(message, "not found"),
		strings.Contains(message, "does not exist"):
		return ErrKeyResourceGone
	case strings.Contains(message, "timeout"),
		strings.Contains(message, "connection"),
		strings.Contains(message, "network"),
		strings.Contains(message, "unreachable"):
		return ErrKeyNetwork
	default:
		return ErrKeyDownloadFailed
	}
}
