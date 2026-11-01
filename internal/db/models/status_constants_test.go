package models

import (
	"errors"
	"testing"
	"time"
)

// TestRulesProduceValidPairs 校验规则表中的每个目标都与 LocalState 组合合法。
func TestRulesProduceValidPairs(t *testing.T) {
	for e := Event(0); e < eventCount; e++ {
		for from := TaskState(0); from < TaskStateCount; from++ {
			rule, ok := CanTransit(e, from)
			if !ok {
				continue
			}
			for local := LocalState(0); local < LocalStateCount; local++ {
				if !ValidPair(from, local) {
					continue
				}
				// 守卫满足时的主目标
				next := resolveLocal(rule, local)
				if rule.Guard == GuardRetryGate || rule.Guard == GuardMetadata {
					if !ValidPair(rule.AltTo, next) {
						t.Fatalf("事件 %v 从 %v/%v 的备选目标 %v 组合非法", e, from, local, rule.AltTo)
					}
				}
				if !ValidPair(rule.To, next) {
					t.Fatalf("事件 %v 从 %v/%v 的目标 %v 组合非法", e, from, local, rule.To)
				}
			}
		}
	}
}

func TestKindLocalRules(t *testing.T) {
	if _, err := Transition(KindLocal, TaskCompleted, LocalPresent, 0, 3, EvAddDownload, EventPayload{SourceAvailable: true}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("本地视频不应允许下载事件, err=%v", err)
	}

	res, err := Transition(KindLocal, TaskCompleted, LocalPresent, 0, 3, EvFileLost, EventPayload{LossReason: LossNotFound})
	if err != nil {
		t.Fatalf("本地文件丢失流转失败: %v", err)
	}
	if res.Local != LocalMissing || res.VersionDelta != 1 {
		t.Fatalf("本地文件丢失结果错误: %+v", res)
	}

	res, err = Transition(KindLocal, TaskCompleted, LocalMissing, 0, 3, EvDeleteLocal, EventPayload{LossReason: LossUserRemoved})
	if err != nil {
		t.Fatalf("删除本地文件流转失败: %v", err)
	}
	if res.Local != LocalMissing {
		t.Fatalf("删除本地文件结果错误: %+v", res)
	}

	if got := DisplayOf(KindLocal, TaskCompleted, LocalPresent); got != ShowLocalReady {
		t.Fatalf("本地视频展示态 = %v, want %v", got, ShowLocalReady)
	}
	if got := DisplayOf(KindLocal, TaskCompleted, LocalMissing); got != ShowLocalLost {
		t.Fatalf("本地视频丢失展示态 = %v, want %v", got, ShowLocalLost)
	}
}

func TestCancelTooLateInVerifying(t *testing.T) {
	if _, err := Transition(KindTorrent, TaskVerifying, LocalPartial, 0, 3, EvCancel, EventPayload{}); !errors.Is(err, ErrCancelTooLate) {
		t.Fatalf("校验阶段取消应返回 ErrCancelTooLate, got %v", err)
	}
	if _, err := Transition(KindTorrent, TaskCompleted, LocalPresent, 0, 3, EvCancel, EventPayload{}); err != nil {
		t.Fatalf("已完成任务取消应为幂等, got %v", err)
	}
}

func TestRetryGate(t *testing.T) {
	maxAttempts := uint16(3)

	res, err := Transition(KindTorrent, TaskDownloading, LocalPartial, 0, maxAttempts, EvStalled, EventPayload{Attempt: 1})
	if err != nil {
		t.Fatalf("首次停滞应进入重试等待: %v", err)
	}
	if res.Task != TaskRetryWait {
		t.Fatalf("首次停滞目标 = %v, want retry_wait", res.Task)
	}
	if res.Effects&EffectDropTorrent == 0 {
		t.Fatal("停滞重试应释放 Torrent 句柄")
	}

	res, err = Transition(KindTorrent, TaskDownloading, LocalPartial, 0, maxAttempts, EvStalled, EventPayload{Attempt: maxAttempts})
	if err != nil {
		t.Fatalf("重试耗尽流转失败: %v", err)
	}
	if res.Task != TaskFailed {
		t.Fatalf("重试耗尽目标 = %v, want failed", res.Task)
	}
}

func TestRepairGuard(t *testing.T) {
	if _, err := Transition(KindTorrent, TaskCompleted, LocalPresent, 0, 3, EvRepair, EventPayload{}); !errors.Is(err, ErrRepairNotNeeded) {
		t.Fatalf("本地文件完好时修复应拒绝, got %v", err)
	}
	if _, err := Transition(KindTorrent, TaskCompleted, LocalMissing, 0, 3, EvRepair, EventPayload{}); err != nil {
		t.Fatalf("本地文件缺失时应允许修复, got %v", err)
	}
}

func TestSourceGuard(t *testing.T) {
	if _, err := Transition(KindTorrent, TaskDiscovered, LocalNone, 0, 3, EvAddDownload, EventPayload{}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("无来源应返回 ErrNoSource, got %v", err)
	}
	res, err := Transition(KindTorrent, TaskDiscovered, LocalNone, 0, 3, EvAddDownload, EventPayload{SourceAvailable: true})
	if err != nil || res.Task != TaskPreparing {
		t.Fatalf("有来源应进入 preparing, got %+v err=%v", res, err)
	}
}

func TestVerifyFailureEffects(t *testing.T) {
	res, err := Transition(KindTorrent, TaskVerifying, LocalPartial, 0, 3, EvVerifyMissing, EventPayload{})
	if err != nil {
		t.Fatalf("校验缺失流转失败: %v", err)
	}
	if res.Task != TaskFailed || res.Local != LocalMissing {
		t.Fatalf("校验缺失结果错误: %+v", res)
	}
	if res.Effects&EffectClearPieces == 0 {
		t.Fatal("校验失败应清理分片完成记录")
	}
}

func TestResumeMetadataGate(t *testing.T) {
	res, err := Transition(KindTorrent, TaskPaused, LocalPartial, 0, 3, EvResume, EventPayload{MetadataReady: true})
	if err != nil || res.Task != TaskQueued {
		t.Fatalf("元信息就绪应回到 queued, got %+v err=%v", res, err)
	}
	res, err = Transition(KindTorrent, TaskPaused, LocalPartial, 0, 3, EvResume, EventPayload{MetadataReady: false})
	if err != nil || res.Task != TaskPreparing {
		t.Fatalf("元信息未就绪应回到 preparing, got %+v err=%v", res, err)
	}
}

func TestPauseIdempotent(t *testing.T) {
	res, err := Transition(KindTorrent, TaskPaused, LocalPartial, 0, 3, EvPause, EventPayload{})
	if err != nil {
		t.Fatalf("重复暂停应幂等: %v", err)
	}
	if res.VersionDelta != 0 || res.Task != TaskPaused {
		t.Fatalf("重复暂停不应推进版本: %+v", res)
	}
}

func TestApplyEventProgressKeepsVersion(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	r := &Resource{
		Kind:         KindTorrent,
		TaskState:    TaskDownloading,
		LocalState:   LocalPartial,
		StateVersion: 5,
		MaxAttempts:  3,
	}

	if _, err := r.ApplyEvent(EvProgress, EventPayload{Now: now}); err != nil {
		t.Fatalf("进度事件失败: %v", err)
	}
	if r.StateVersion != 5 || r.TaskState != TaskDownloading || r.LocalState != LocalPartial {
		t.Fatalf("进度事件不应改变状态与版本: %+v", r)
	}

	res, err := r.ApplyEvent(EvDownloadComplete, EventPayload{Now: now})
	if err != nil {
		t.Fatalf("下载完成事件失败: %v", err)
	}
	if res.Task != TaskVerifying || res.Local != LocalPartial {
		t.Fatalf("下载完成结果错误: %+v", res)
	}
	if r.StateVersion != 6 || r.StateChangedAt == nil {
		t.Fatalf("状态变化应推进版本并记录时间: %+v", r)
	}
}

func TestDisplayMatrixCoversValidPairs(t *testing.T) {
	for task := TaskState(0); task < TaskStateCount; task++ {
		for local := LocalState(0); local < LocalStateCount; local++ {
			if !ValidPair(task, local) {
				continue
			}
			if got := DisplayOf(KindTorrent, task, local); !got.Valid() {
				t.Fatalf("合法状态对 %v/%v 缺少展示态", task, local)
			}
		}
	}
}

// TestPendingKeepsUnfinishedTasks 待下载列表必须保留所有用户触发过、
// 且尚未完成的任务状态，只有已完成与仅爬虫发现的记录才被排除。
func TestPendingKeepsUnfinishedTasks(t *testing.T) {
	pending := []TaskState{
		TaskPreparing, TaskQueued, TaskDownloading, TaskPaused,
		TaskVerifying, TaskRetryWait, TaskFailed, TaskCancelled,
	}
	for _, state := range pending {
		if !state.Pending() {
			t.Fatalf("状态 %v 应保留在待下载列表", state)
		}
	}

	for _, state := range []TaskState{TaskDiscovered, TaskCompleted} {
		if state.Pending() {
			t.Fatalf("状态 %v 不应出现在待下载列表", state)
		}
	}
	if TaskState(TaskStateCount).Pending() {
		t.Fatal("越界状态不应出现在待下载列表")
	}
}

// TestRecoverableExcludesUserIntent 自动恢复集合只覆盖异常中断，
// 用户主动暂停 / 取消的任务保留在列表但不自动继续下载。
func TestRecoverableExcludesUserIntent(t *testing.T) {
	recoverable := []TaskState{
		TaskPreparing, TaskQueued, TaskDownloading,
		TaskVerifying, TaskRetryWait, TaskFailed,
	}
	for _, state := range recoverable {
		if !state.Recoverable() {
			t.Fatalf("状态 %v 应在启动时自动恢复", state)
		}
	}

	for _, state := range []TaskState{TaskPaused, TaskCancelled, TaskCompleted, TaskDiscovered} {
		if state.Recoverable() {
			t.Fatalf("状态 %v 不应自动恢复", state)
		}
	}
	if TaskState(TaskStateCount).Recoverable() {
		t.Fatal("越界状态不应自动恢复")
	}
}
