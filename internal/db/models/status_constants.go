package models

import (
	"errors"
	"time"
)

// =========================================================
// 资源来源类型
// =========================================================

// Kind 资源来源类型。KindLocal 不参与下载状态机流转。
type Kind int8

const (
	// KindTorrent 种子资源。
	KindTorrent Kind = iota
	// KindLocal 用户拖入的本地视频。
	KindLocal
)

const kindCount = 2

var kindNames = [kindCount]string{"torrent", "local"}

func (k Kind) String() string {
	if k < 0 || int(k) >= kindCount {
		return "unknown"
	}
	return kindNames[k]
}

func (k Kind) Valid() bool { return k >= 0 && int(k) < kindCount }

// =========================================================
// 下载任务生命周期（TaskState）
// =========================================================

// TaskState 下载任务生命周期状态。
type TaskState int8

const (
	// TaskDiscovered 已发现，未产生下载意图。
	TaskDiscovered TaskState = iota
	// TaskPreparing 解析来源、挂载 Torrent、拉取元信息。
	TaskPreparing
	// TaskQueued 元信息就绪，等待并发槽位。
	TaskQueued
	// TaskDownloading 正在下载。
	TaskDownloading
	// TaskPaused 用户暂停。
	TaskPaused
	// TaskVerifying 下载完成，正在校验落盘。
	TaskVerifying
	// TaskCompleted 校验通过（历史事实）。
	TaskCompleted
	// TaskRetryWait 自动重试等待中。
	TaskRetryWait
	// TaskFailed 失败，可手动重试。
	TaskFailed
	// TaskCancelled 用户取消（历史状态）。
	// 新流程取消后直接回到 TaskDiscovered，该状态仅用于兼容旧数据。
	TaskCancelled
)

// TaskStateCount TaskState 枚举数量，供定长规则表使用。
const TaskStateCount = 10

var taskStateNames = [TaskStateCount]string{
	"discovered", "preparing", "queued", "downloading", "paused",
	"verifying", "completed", "retry_wait", "failed", "cancelled",
}

func (s TaskState) String() string {
	if !s.Valid() {
		return "unknown"
	}
	return taskStateNames[s]
}

func (s TaskState) Valid() bool { return s >= 0 && int(s) < TaskStateCount }

// activeMask 活跃状态位图：需要调度、定时器或上下文的状态。
const activeMask uint32 = 1<<uint(TaskPreparing) | 1<<uint(TaskQueued) |
	1<<uint(TaskDownloading) | 1<<uint(TaskVerifying) | 1<<uint(TaskRetryWait)

// Active 判断任务是否处于活跃状态，O(1) 位运算。
func (s TaskState) Active() bool {
	return s.Valid() && activeMask&(1<<uint(s)) != 0
}

// pendingMask 待下载列表状态位图：用户触发过下载、且尚未完成的任务。
//
// Cancelled 不在其中：取消会把任务恢复到 TaskDiscovered（等同从未下载），
// 因此取消的任务不会留在下载列表中。Cancelled 仅用于兼容历史数据。
const pendingMask uint32 = 1<<uint(TaskPreparing) | 1<<uint(TaskQueued) |
	1<<uint(TaskDownloading) | 1<<uint(TaskPaused) | 1<<uint(TaskVerifying) |
	1<<uint(TaskRetryWait) | 1<<uint(TaskFailed)

// Pending 判断任务是否应保留在下载页「待下载」列表，O(1)。
//
// 只要用户触发过下载且任务未完成，无论经历元信息超时、网络中断、
// 强制退出还是系统重启，该状态集合内的任务都必须继续展示。
func (s TaskState) Pending() bool {
	return s.Valid() && pendingMask&(1<<uint(s)) != 0
}

// recoverMask 应用启动时需要自动恢复下载的状态位图。
//
// Paused（用户主动暂停）与 Cancelled（用户主动取消）属于明确的用户意图，
// 不自动恢复；其余待下载状态都视为异常中断，启动后自动重新入队。
const recoverMask uint32 = 1<<uint(TaskPreparing) | 1<<uint(TaskQueued) |
	1<<uint(TaskDownloading) | 1<<uint(TaskVerifying) | 1<<uint(TaskRetryWait) |
	1<<uint(TaskFailed)

// Recoverable 判断任务是否需要在应用启动时自动恢复下载流程，O(1)。
func (s TaskState) Recoverable() bool {
	return s.Valid() && recoverMask&(1<<uint(s)) != 0
}

// =========================================================
// 本地文件可用性（LocalState）
// =========================================================

// LocalState 本地文件可用性状态。
type LocalState int8

const (
	// LocalNone 尚无任何本地文件。
	LocalNone LocalState = iota
	// LocalPartial 已有部分数据，可续传。
	LocalPartial
	// LocalPresent 文件齐全且校验通过。
	LocalPresent
	// LocalMissing 应当存在但缺失，或已被删除。
	LocalMissing
	// LocalCorrupted 文件存在但大小/校验不匹配。
	LocalCorrupted
)

const localStateCount = 5

// LocalStateCount LocalState 枚举数量。
const LocalStateCount = 5

// LocalKeep 规则中表示"保持当前 LocalState 不变"。
const LocalKeep LocalState = -1

// LocalReset 规则中表示"缺失/损坏时归零，否则保持不变"。
const LocalReset LocalState = -2

var localStateNames = [localStateCount]string{
	"none", "partial", "present", "missing", "corrupted",
}

func (s LocalState) String() string {
	if s < 0 || int(s) >= localStateCount {
		return "unknown"
	}
	return localStateNames[s]
}

func (s LocalState) Valid() bool { return s >= 0 && int(s) < localStateCount }

// =========================================================
// 本地文件丢失原因
// =========================================================

// LocalLossReason 本地文件缺失/损坏的成因。
type LocalLossReason int8

const (
	// LossNone 无丢失。
	LossNone LocalLossReason = iota
	// LossNotFound 外部删除或路径不可达。
	LossNotFound
	// LossSizeMismatch 文件大小不一致。
	LossSizeMismatch
	// LossUserRemoved 用户主动删除。
	LossUserRemoved
	// LossWriteError 写盘失败。
	LossWriteError
)

const localLossReasonCount = 5

var localLossReasonNames = [localLossReasonCount]string{
	"none", "not_found", "size_mismatch", "user_removed", "write_error",
}

func (r LocalLossReason) String() string {
	if r < 0 || int(r) >= localLossReasonCount {
		return "unknown"
	}
	return localLossReasonNames[r]
}

func (r LocalLossReason) Valid() bool { return r >= 0 && int(r) < localLossReasonCount }

// =========================================================
// 展示态（派生，不落库）
// =========================================================

// DisplayState 前端展示态，由 (Kind, TaskState, LocalState) 派生。
type DisplayState int8

const (
	ShowUndownloaded DisplayState = iota
	ShowFetchingMeta
	ShowQueued
	ShowDownloading
	ShowPaused
	ShowVerifying
	ShowRetrying
	ShowCompleted
	ShowFailed
	ShowCancelled
	ShowFileLost
	ShowCorrupted
	ShowLocalReady
	ShowLocalLost
)

// DisplayStateCount DisplayState 枚举数量。
const DisplayStateCount = 14

var displayStateNames = [DisplayStateCount]string{
	"undownloaded", "fetching_meta", "queued", "downloading", "paused",
	"verifying", "retrying", "completed", "failed", "cancelled",
	"file_lost", "corrupted", "local_ready", "local_lost",
}

func (s DisplayState) String() string {
	if s < 0 || int(s) >= DisplayStateCount {
		return "unknown"
	}
	return displayStateNames[s]
}

func (s DisplayState) Valid() bool { return s >= 0 && int(s) < DisplayStateCount }

// =========================================================
// 事件
// =========================================================

// Event 状态机事件。
type Event int8

const (
	EvCrawlDiscover Event = iota
	EvAddDownload
	EvAttachOk
	EvMetaReady
	EvMetaTimeout
	EvAttachFail
	EvSourceUnresolved
	EvRetryDelayElapsed
	EvSlotGranted
	EvProgress
	EvStalled
	EvDownloadFatal
	EvDownloadComplete
	EvVerifyOK
	EvVerifyMissing
	EvVerifyCorrupted
	EvPause
	EvResume
	EvCancel
	EvManualRetry
	EvRedownload
	EvRepair
	EvFileLost
	EvDeleteLocal
	EvImportLocal
	EvPriorityChanged
	EvRemovePlaylist
)

const eventCount = 27

var eventNames = [eventCount]string{
	"crawl_discover", "add_download", "attach_ok", "meta_ready", "meta_timeout",
	"attach_fail", "source_unresolved", "retry_delay_elapsed", "slot_granted", "progress",
	"stalled", "download_fatal", "download_complete", "verify_ok", "verify_missing",
	"verify_corrupted", "pause", "resume", "cancel", "manual_retry",
	"redownload", "repair", "file_lost", "delete_local", "import_local",
	"priority_changed", "remove_playlist",
}

func (e Event) String() string {
	if e < 0 || int(e) >= eventCount {
		return "unknown"
	}
	return eventNames[e]
}

func (e Event) Valid() bool { return e >= 0 && int(e) < eventCount }

// =========================================================
// 规则 / 守卫 / 副作用
// =========================================================

// EffectBits 状态流转后需要执行的副作用位图。
type EffectBits uint32

const (
	// EffectPersist 需要落库。
	EffectPersist EffectBits = 1 << iota
	// EffectEmit 需要推送状态变更事件。
	EffectEmit
	// EffectStopContext 需要取消任务上下文。
	EffectStopContext
	// EffectDropTorrent 需要释放 Torrent 句柄。
	EffectDropTorrent
	// EffectClearPieces 需要清理该任务的分片完成记录。
	EffectClearPieces
	// EffectSchedule 需要触发调度。
	EffectSchedule
)

// GuardID 规则守卫类型。
type GuardID int8

const (
	GuardNone GuardID = iota
	// GuardRetryGate 依据尝试次数在 To / AltTo 之间选择。
	GuardRetryGate
	// GuardRepair 仅允许 LocalState 为 Missing / Corrupted。
	GuardRepair
	// GuardDelete 仅允许非活跃状态删除本地文件。
	GuardDelete
	// GuardSource 来源必须可解析。
	GuardSource
	// GuardMetadata 元信息就绪时取 To，否则取 AltTo。
	GuardMetadata
)

// Rule 单个事件在单个起始状态下的流转规则。零值表示非法。
type Rule struct {
	To      TaskState
	AltTo   TaskState
	LocalTo LocalState
	Guard   GuardID
	Effects EffectBits
	Valid   bool
}

// taskRules[event][from] 为 O(1) 规则表。
var taskRules [eventCount][TaskStateCount]Rule

// setRule 注册一条规则。
func setRule(e Event, from TaskState, to TaskState, localTo LocalState, guard GuardID, effects EffectBits) {
	taskRules[e][from] = Rule{
		To:      to,
		AltTo:   to,
		LocalTo: localTo,
		Guard:   guard,
		Effects: effects,
		Valid:   true,
	}
}

// setAltRule 注册一条带备选目标的规则（重试耗尽、元信息不可用等场景）。
func setAltRule(e Event, from TaskState, to, alt TaskState, localTo LocalState, guard GuardID, effects EffectBits) {
	taskRules[e][from] = Rule{
		To:      to,
		AltTo:   alt,
		LocalTo: localTo,
		Guard:   guard,
		Effects: effects,
		Valid:   true,
	}
}

func init() {
	const pe = EffectPersist | EffectEmit

	// 入队 / 重试 / 修复
	setRule(EvAddDownload, TaskDiscovered, TaskPreparing, LocalNone, GuardSource, pe)
	setRule(EvManualRetry, TaskFailed, TaskPreparing, LocalNone, GuardSource, pe)
	setRule(EvRedownload, TaskCancelled, TaskPreparing, LocalReset, GuardNone, pe)
	setRule(EvRepair, TaskCompleted, TaskPreparing, LocalNone, GuardRepair, pe)

	// 元信息阶段
	setRule(EvMetaReady, TaskPreparing, TaskQueued, LocalKeep, GuardNone, pe|EffectSchedule)
	setAltRule(EvMetaTimeout, TaskPreparing, TaskRetryWait, TaskFailed, LocalKeep, GuardRetryGate, pe)
	setAltRule(EvAttachFail, TaskPreparing, TaskRetryWait, TaskFailed, LocalKeep, GuardRetryGate, pe)
	setRule(EvSourceUnresolved, TaskPreparing, TaskFailed, LocalKeep, GuardNone, pe)
	setRule(EvRetryDelayElapsed, TaskRetryWait, TaskPreparing, LocalKeep, GuardNone, pe)

	// 下载阶段
	setRule(EvSlotGranted, TaskQueued, TaskDownloading, LocalKeep, GuardNone, pe)
	setRule(EvProgress, TaskDownloading, TaskDownloading, LocalPartial, GuardNone, EffectPersist)
	setAltRule(EvStalled, TaskDownloading, TaskRetryWait, TaskFailed, LocalKeep, GuardRetryGate, pe|EffectDropTorrent)
	setRule(EvDownloadFatal, TaskDownloading, TaskFailed, LocalKeep, GuardNone, pe|EffectDropTorrent)
	setRule(EvDownloadComplete, TaskDownloading, TaskVerifying, LocalPartial, GuardNone, pe)

	// 校验阶段
	setRule(EvVerifyOK, TaskVerifying, TaskCompleted, LocalPresent, GuardNone, pe)
	setRule(EvVerifyMissing, TaskVerifying, TaskFailed, LocalMissing, GuardNone, pe|EffectClearPieces)
	setRule(EvVerifyCorrupted, TaskVerifying, TaskFailed, LocalCorrupted, GuardNone, pe|EffectClearPieces)

	// 用户操作
	ps := []TaskState{TaskPreparing, TaskQueued, TaskDownloading, TaskRetryWait}
	for _, from := range ps {
		setRule(EvPause, from, TaskPaused, LocalKeep, GuardNone, pe|EffectStopContext)
	}
	// 取消即恢复到「未下载」：任务记录回到 Discovered，列表与状态不留下下载痕迹。
	// Paused / Failed / Cancelled 同样允许取消，Cancelled 用于兼容历史数据。
	cs := []TaskState{
		TaskPreparing, TaskQueued, TaskDownloading, TaskPaused,
		TaskRetryWait, TaskFailed, TaskCancelled,
	}
	for _, from := range cs {
		setRule(EvCancel, from, TaskDiscovered, LocalNone, GuardNone, pe|EffectStopContext|EffectDropTorrent)
	}
	setAltRule(EvResume, TaskPaused, TaskQueued, TaskPreparing, LocalKeep, GuardMetadata, pe)

	// 本地文件丢失 / 删除
	setRule(EvFileLost, TaskCompleted, TaskCompleted, LocalMissing, GuardNone, pe)
	setRule(EvFileLost, TaskPaused, TaskFailed, LocalMissing, GuardNone, pe)
	setRule(EvFileLost, TaskRetryWait, TaskFailed, LocalMissing, GuardNone, pe)

	setRule(EvDeleteLocal, TaskCompleted, TaskCompleted, LocalMissing, GuardDelete, pe)
	setRule(EvDeleteLocal, TaskFailed, TaskFailed, LocalMissing, GuardDelete, pe)
	setRule(EvDeleteLocal, TaskCancelled, TaskCancelled, LocalMissing, GuardDelete, pe)

	// 从播放列表移除种子资源：恢复到「未下载」，磁盘上的文件保留。
	// 播放列表可能包含下载中、校验中或已完成的资源，均允许移除。
	for _, from := range []TaskState{TaskDownloading, TaskVerifying, TaskCompleted} {
		setRule(EvRemovePlaylist, from, TaskDiscovered, LocalNone, GuardNone, pe|EffectStopContext|EffectDropTorrent)
	}
}

// CanTransit 查询规则表，O(1)。
func CanTransit(e Event, from TaskState) (Rule, bool) {
	if !e.Valid() || !from.Valid() {
		return Rule{}, false
	}
	rule := taskRules[e][from]
	return rule, rule.Valid
}

// 状态对合法性矩阵：validPair[taskState][localState]。
var validPair [TaskStateCount][LocalStateCount]bool

func allowPair(task TaskState, locals ...LocalState) {
	for _, local := range locals {
		validPair[task][local] = true
	}
}

func init() {
	allowPair(TaskDiscovered, LocalNone)
	// Preparing / Queued 阶段可能保留断点续传的部分数据，因此允许 Partial。
	allowPair(TaskPreparing, LocalNone, LocalPartial)
	allowPair(TaskQueued, LocalNone, LocalPartial)
	allowPair(TaskDownloading, LocalNone, LocalPartial)
	allowPair(TaskPaused, LocalNone, LocalPartial)
	allowPair(TaskVerifying, LocalPartial)
	allowPair(TaskCompleted, LocalPresent, LocalMissing, LocalCorrupted)
	allowPair(TaskRetryWait, LocalNone, LocalPartial)
	allowPair(TaskFailed, LocalNone, LocalPartial, LocalMissing, LocalCorrupted)
	allowPair(TaskCancelled, LocalNone, LocalPartial, LocalMissing, LocalCorrupted)
}

// ValidPair 校验 (TaskState, LocalState) 是否为合法组合，O(1)。
func ValidPair(task TaskState, local LocalState) bool {
	if !task.Valid() || !local.Valid() {
		return false
	}
	return validPair[task][local]
}

// StatePairValid 校验资源记录的状态对；KindLocal 固定为 Completed + Present/Missing/Corrupted。
func StatePairValid(kind Kind, task TaskState, local LocalState) bool {
	if !ValidPair(task, local) {
		return false
	}
	if kind == KindLocal {
		return task == TaskCompleted && (local == LocalPresent || local == LocalMissing || local == LocalCorrupted)
	}
	return true
}

// displayMatrix[taskState][localState] → DisplayState（KindTorrent 使用）。
var displayMatrix [TaskStateCount][LocalStateCount]DisplayState

func setDisplay(task TaskState, display DisplayState, locals ...LocalState) {
	for _, local := range locals {
		displayMatrix[task][local] = display
	}
}

func init() {
	setDisplay(TaskDiscovered, ShowUndownloaded, LocalNone)
	setDisplay(TaskPreparing, ShowFetchingMeta, LocalNone, LocalPartial)
	setDisplay(TaskQueued, ShowQueued, LocalNone, LocalPartial)
	setDisplay(TaskRetryWait, ShowRetrying, LocalNone, LocalPartial)
	setDisplay(TaskDownloading, ShowDownloading, LocalNone, LocalPartial)
	setDisplay(TaskPaused, ShowPaused, LocalNone, LocalPartial)
	setDisplay(TaskVerifying, ShowVerifying, LocalPartial)
	setDisplay(TaskCompleted, ShowCompleted, LocalPresent)
	setDisplay(TaskCompleted, ShowFileLost, LocalMissing)
	setDisplay(TaskCompleted, ShowCorrupted, LocalCorrupted)
	setDisplay(TaskFailed, ShowFailed, LocalNone, LocalPartial)
	setDisplay(TaskFailed, ShowFileLost, LocalMissing)
	setDisplay(TaskFailed, ShowCorrupted, LocalCorrupted)
	setDisplay(TaskCancelled, ShowCancelled, LocalNone, LocalPartial)
	setDisplay(TaskCancelled, ShowFileLost, LocalMissing)
	setDisplay(TaskCancelled, ShowCorrupted, LocalCorrupted)
}

// DisplayOf 派生展示态，O(1)。
func DisplayOf(kind Kind, task TaskState, local LocalState) DisplayState {
	if kind == KindLocal {
		switch local {
		case LocalPresent:
			return ShowLocalReady
		case LocalMissing, LocalCorrupted:
			return ShowLocalLost
		default:
			return ShowLocalLost
		}
	}
	if !task.Valid() || !local.Valid() {
		return ShowFailed
	}
	if display := displayMatrix[task][local]; display.Valid() {
		return display
	}
	return ShowFailed
}

// =========================================================
// 流转计算
// =========================================================

// 状态机错误。错误文本即 i18n 消息 ID，上层可直接 T(err.Error())。
var (
	// ErrInvalidTransition 非法跳转。
	ErrInvalidTransition = errors.New("download.error.invalid_transition")
	// ErrCancelTooLate 已完成校验，无法取消。
	ErrCancelTooLate = errors.New("download.error.cancel_too_late")
	// ErrRepairNotNeeded 本地文件完好，无需修复。
	ErrRepairNotNeeded = errors.New("download.error.repair_not_needed")
	// ErrDeleteNotAllowed 当前状态不允许删除本地文件。
	ErrDeleteNotAllowed = errors.New("download.error.delete_not_allowed")
	// ErrNoSource 缺少可用下载来源。
	ErrNoSource = errors.New("download.error.no_source")
)

// EventPayload 事件入参。
type EventPayload struct {
	// Now 事件发生时间，零值时由调用方补齐。
	Now time.Time
	// Attempt 本次流转后的尝试次数（重试类事件由调用方 +1）。
	Attempt uint16
	// MetadataReady 元信息是否就绪，供 Resume 选择目标。
	MetadataReady bool
	// SourceAvailable 来源是否可解析。
	SourceAvailable bool
	// ErrorCode 失败原因（i18n key）。
	ErrorCode string
	// LossReason 本地文件丢失原因。
	LossReason LocalLossReason
	// NextRetryAt 下一次自动重试时间。
	NextRetryAt *time.Time
}

// Result 流转结果。
type Result struct {
	Task         TaskState
	Local        LocalState
	Effects      EffectBits
	VersionDelta uint64
}

// resolveLocal 按规则解析目标 LocalState。
func resolveLocal(rule Rule, current LocalState) LocalState {
	switch rule.LocalTo {
	case LocalKeep:
		return current
	case LocalReset:
		if current == LocalMissing || current == LocalCorrupted {
			return LocalNone
		}
		return current
	default:
		return rule.LocalTo
	}
}

// Transition 纯函数计算流转结果，不修改任何状态。
//
// 返回 ErrInvalidTransition / ErrCancelTooLate / ErrRepairNotNeeded / ErrNoSource。
func Transition(kind Kind, from TaskState, local LocalState, attempt, maxAttempts uint16, e Event, p EventPayload) (Result, error) {
	if !kind.Valid() || !from.Valid() || !local.Valid() {
		return Result{}, ErrInvalidTransition
	}

	// KindLocal 仅允许文件丢失 / 删除本地文件，且状态恒为 Completed。
	if kind == KindLocal {
		switch e {
		case EvFileLost:
			if from == TaskCompleted && local == LocalPresent {
				return Result{Task: TaskCompleted, Local: LocalMissing, Effects: EffectPersist | EffectEmit, VersionDelta: 1}, nil
			}
		case EvDeleteLocal:
			if from == TaskCompleted && (local == LocalPresent || local == LocalMissing) {
				return Result{Task: TaskCompleted, Local: LocalMissing, Effects: EffectPersist | EffectEmit, VersionDelta: 1}, nil
			}
		}
		return Result{}, ErrInvalidTransition
	}

	// 幂等事件：不改变状态，也不产生新版本。
	switch e {
	case EvPause:
		if from == TaskPaused {
			return Result{Task: from, Local: local}, nil
		}
	case EvCancel:
		if from == TaskVerifying {
			return Result{}, ErrCancelTooLate
		}
		// 已完成与未下载状态取消为幂等操作，不改变任何状态。
		if from == TaskCompleted || from == TaskDiscovered {
			return Result{Task: from, Local: local}, nil
		}
	}

	rule, ok := CanTransit(e, from)
	if !ok {
		return Result{}, ErrInvalidTransition
	}

	to := rule.To
	switch rule.Guard {
	case GuardRetryGate:
		// p.Attempt 为本次流转后的尝试次数，达到上限即降级为 AltTo。
		if p.Attempt >= maxAttempts {
			to = rule.AltTo
		}
	case GuardMetadata:
		if !p.MetadataReady {
			to = rule.AltTo
		}
	case GuardRepair:
		if local != LocalMissing && local != LocalCorrupted {
			return Result{}, ErrRepairNotNeeded
		}
	case GuardSource:
		if !p.SourceAvailable {
			return Result{}, ErrNoSource
		}
	case GuardDelete:
		if from != TaskCompleted && from != TaskFailed && from != TaskCancelled {
			return Result{}, ErrDeleteNotAllowed
		}
	}

	nextLocal := resolveLocal(rule, local)
	if !ValidPair(to, nextLocal) {
		return Result{}, ErrInvalidTransition
	}

	delta := uint64(0)
	if to != from || nextLocal != local {
		delta = 1
	}

	return Result{
		Task:         to,
		Local:        nextLocal,
		Effects:      rule.Effects,
		VersionDelta: delta,
	}, nil
}

// ApplyEvent 在资源记录上执行一次流转，并更新状态相关字段；不落库。
//
// 幂等事件返回零版本变更，调用方据此跳过持久化。
func (r *Resource) ApplyEvent(e Event, p EventPayload) (Result, error) {
	if r == nil {
		return Result{}, ErrInvalidTransition
	}

	res, err := Transition(r.Kind, r.TaskState, r.LocalState, r.Attempt, r.MaxAttempts, e, p)
	if err != nil {
		return res, err
	}

	if res.VersionDelta == 0 && res.Task == r.TaskState && res.Local == r.LocalState {
		return res, nil
	}

	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}

	r.TaskState = res.Task
	r.LocalState = res.Local
	r.StateVersion += res.VersionDelta
	r.StateChangedAt = &now

	switch e {
	case EvAddDownload, EvManualRetry, EvRedownload, EvRepair:
		r.Attempt = 0
		r.ErrorCode = ""
		r.NextRetryAt = nil
	case EvCancel, EvRemovePlaylist:
		// 取消 / 从播放列表移除等同从未下载：清除下载意图、进度与本地文件关联，
		// 资源记录本身保留；磁盘上的文件由调用方决定是否删除。
		r.Attempt = 0
		r.ErrorCode = ""
		r.NextRetryAt = nil
		r.VerifiedAt = nil
		r.LocalCheckedAt = nil
		r.LocalLossReason = LossNone
		r.FileName = nil
		r.FileSize = nil
		r.DownloadedSize = nil
		r.MimeType = nil
		r.LocalPath = ""
	case EvMetaReady:
		// 元数据就绪只说明这一步成功，不代表任务已恢复正常；
		// 连续失败次数继续累计，否则每次重试都会被清零，永远到不了上限。
		r.ErrorCode = ""
		r.NextRetryAt = nil
	case EvMetaTimeout, EvAttachFail, EvStalled:
		r.Attempt = p.Attempt
		r.ErrorCode = p.ErrorCode
		if res.Task == TaskRetryWait {
			r.NextRetryAt = p.NextRetryAt
		} else {
			r.NextRetryAt = nil
		}
	case EvSourceUnresolved, EvDownloadFatal:
		// 致命错误不进入重试流程，清空尝试次数，前端只展示错误信息。
		r.Attempt = 0
		r.NextRetryAt = nil
		r.ErrorCode = p.ErrorCode
	case EvVerifyOK:
		r.ErrorCode = ""
		r.VerifiedAt = &now
		r.LocalCheckedAt = &now
		r.LocalLossReason = LossNone
	case EvVerifyMissing, EvVerifyCorrupted, EvFileLost, EvDeleteLocal:
		r.ErrorCode = p.ErrorCode
		r.LocalLossReason = p.LossReason
		r.LocalCheckedAt = &now
	case EvSlotGranted, EvRetryDelayElapsed, EvResume:
		r.NextRetryAt = nil
	}

	if r.LocalState == LocalNone || r.LocalState == LocalPresent {
		if e == EvVerifyOK || e == EvAddDownload || e == EvManualRetry || e == EvRepair || e == EvProgress {
			r.LocalLossReason = LossNone
		}
	}

	return res, nil
}
