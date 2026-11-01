package transcode

import (
	"context"
	"sync"
)

// Job 是一个转换任务句柄，可等待、取消或读取状态快照。
type Job struct {
	ID     string
	Source string
	Output string

	mu       sync.Mutex
	phase    Phase
	progress float64
	speed    string
	format   Format
	decision Decision
	result   Result
	err      error

	done     chan struct{}
	once     sync.Once
	ctx      context.Context
	cancelFn context.CancelFunc
	reporter Reporter
}

// Done 返回任务完成信号。
func (j *Job) Done() <-chan struct{} { return j.done }

// Wait 等待任务结束；ctx 只影响本次等待，不会取消任务本身。
func (j *Job) Wait(ctx context.Context) (Result, error) {
	select {
	case <-j.done:
		j.mu.Lock()
		defer j.mu.Unlock()
		return j.result, j.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// Result 返回已完成任务的结果；任务尚未结束时返回零值，不会阻塞。
func (j *Job) Result() (Result, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.phase != PhaseDone && j.phase != PhaseCanceled {
		return Result{}, nil
	}
	return j.result, j.err
}

// Snapshot 返回当前状态（可并发调用）。
func (j *Job) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Snapshot{
		JobID:    j.ID,
		Source:   j.Source,
		Output:   j.Output,
		Phase:    j.phase,
		Progress: j.progress,
		Speed:    j.speed,
		Action:   j.result.Action,
		Reason:   j.result.Reason,
	}
}

// Cancel 取消任务；仅当任务尚未结束时有效。
func (j *Job) Cancel() {
	if j.cancelFn != nil {
		j.cancelFn()
	}
}

func (j *Job) isDone() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

func (j *Job) setPhase(phase Phase) {
	j.mu.Lock()
	j.phase = phase
	j.mu.Unlock()
	j.emit()
}

func (j *Job) setFormat(format Format) {
	j.mu.Lock()
	j.format = format
	j.mu.Unlock()
}

func (j *Job) setDecision(decision Decision) {
	j.mu.Lock()
	j.decision = decision
	j.mu.Unlock()
}

func (j *Job) setOutput(output string) {
	j.mu.Lock()
	j.Output = output
	j.mu.Unlock()
}

func (j *Job) setProgress(percent float64) {
	j.mu.Lock()
	j.progress = percent
	j.mu.Unlock()
	j.emit()
}

func (j *Job) setSpeed(speed string) {
	j.mu.Lock()
	j.speed = speed
	j.mu.Unlock()
	j.emit()
}

func (j *Job) complete(result Result, err error) {
	j.mu.Lock()
	j.result = result
	j.err = err
	j.phase = PhaseDone
	if result.Reason == ReasonCanceled {
		j.phase = PhaseCanceled
	}
	j.mu.Unlock()

	if j.cancelFn != nil {
		j.cancelFn()
	}
	j.once.Do(func() { close(j.done) })
	j.emit()
}

func (j *Job) emit() {
	if j.reporter == nil {
		return
	}
	j.reporter.OnProgress(j.Snapshot())
}

func newFinishedJob(source string, result Result, err error) *Job {
	job := &Job{
		ID:     jobID(source),
		Source: source,
		phase:  PhaseDone,
		done:   make(chan struct{}),
	}
	job.result = result
	job.err = err
	close(job.done)
	return job
}
