package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/bing/bing-play/internal/logger"
)

// Phase 转换任务所处阶段。
type Phase string

const (
	PhasePending    Phase = "pending"
	PhaseProbing    Phase = "probing"
	PhaseConverting Phase = "converting"
	PhaseDone       Phase = "done"
	PhaseCanceled   Phase = "canceled"
)

// Snapshot 任务状态快照，可直接推送给前端。
type Snapshot struct {
	JobID    string  `json:"jobId"`
	Source   string  `json:"source"`
	Output   string  `json:"output"`
	Phase    Phase   `json:"phase"`
	Progress float64 `json:"progress"`
	Speed    string  `json:"speed"`
	Action   Action  `json:"action"`
	Reason   Reason  `json:"reason"`
}

// Reporter 进度上报接口。实现不应阻塞，调度器会在任务 goroutine 内调用。
type Reporter interface {
	OnProgress(Snapshot)
}

// ReporterFunc 把普通函数适配为 Reporter。
type ReporterFunc func(Snapshot)

// OnProgress 实现 Reporter。
func (f ReporterFunc) OnProgress(snapshot Snapshot) { f(snapshot) }

// Request 转换请求。
type Request struct {
	// Path 待处理的视频文件路径。
	Path string `json:"path"`
	// Force 跳过兼容性判断，强制执行转换。
	Force bool `json:"force"`
}

// Result 转换任务结果。
type Result struct {
	Action    Action `json:"action"`
	PlayPath  string `json:"playPath"`
	Format    Format `json:"format"`
	FromCache bool   `json:"fromCache"`
	Reason    Reason `json:"reason"`
	// Err 非空表示发生了降级或失败，PlayPath 仍然可用（原始文件）。
	Err error `json:"-"`
}

// Options 调度器配置。未设置的字段会使用默认实现。
type Options struct {
	CacheDir   string
	Policy     Policy
	Target     Target
	Workers    int
	Reporter   Reporter
	Prober     Prober
	Runner     Runner
	Binaries   Binaries
	Logger     *slog.Logger
	Conditions func(Format) Conditions
}

// Scheduler 转换任务调度器。
//
// 对外只暴露 Submit：提交后立即返回 Job，转换在后台 goroutine 中执行，
// 同一文件并发提交会被合并成同一个任务；结果写入缓存目录，原文件不动。
type Scheduler struct {
	opts     Options
	logger   *slog.Logger
	runner   Runner
	prober   Prober
	binaries Binaries
	cacheDir string
	target   Target
	policy   Policy
	reporter Reporter

	ctx    context.Context
	cancel context.CancelFunc
	sem    chan struct{}
	wg     sync.WaitGroup

	mu     sync.Mutex
	jobs   map[string]*Job
	closed bool
}

// NewScheduler 创建调度器。
func NewScheduler(opts Options) *Scheduler {
	if opts.Policy.OutputSizeFactor <= 0 {
		opts.Policy.OutputSizeFactor = DefaultPolicy().OutputSizeFactor
	}
	if opts.Workers <= 0 {
		opts.Workers = 1
	}
	if opts.Logger == nil {
		opts.Logger = logger.App
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	binaries := opts.Binaries
	if binaries.FFmpeg == "" && binaries.FFprobe == "" {
		binaries = LocateBinaries()
	}

	runner := opts.Runner
	if runner == nil {
		runner = NewExecRunner()
	}

	prober := opts.Prober
	if prober == nil {
		prober = NewDetector(binaries, runner)
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "bing-play", "transcode")
	}
	_ = os.MkdirAll(cacheDir, 0o755)
	cleanupPartFiles(cacheDir, opts.Logger)

	target := opts.Target
	if target == "" {
		target = TargetForOS(runtime.GOOS)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		opts:     opts,
		logger:   opts.Logger,
		runner:   runner,
		prober:   prober,
		binaries: binaries,
		cacheDir: cacheDir,
		target:   target,
		policy:   opts.Policy,
		reporter: opts.Reporter,
		ctx:      ctx,
		cancel:   cancel,
		sem:      make(chan struct{}, opts.Workers),
		jobs:     make(map[string]*Job),
	}
}

// CacheDir 返回缓存目录。
func (s *Scheduler) CacheDir() string { return s.cacheDir }

// Binaries 返回当前使用的 ffmpeg 路径，便于日志与诊断。
func (s *Scheduler) Binaries() Binaries { return s.binaries }

// Submit 提交转换请求并立即返回任务句柄。
func (s *Scheduler) Submit(req Request) *Job {
	src := req.Path
	if abs, err := filepath.Abs(src); err == nil {
		src = abs
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return newFinishedJob(src, Result{
			Action: ActionFallback, PlayPath: src, Reason: ReasonClosed, Err: ErrSchedulerClosed,
		}, ErrSchedulerClosed)
	}
	if existing, ok := s.jobs[src]; ok {
		if !existing.isDone() {
			s.mu.Unlock()
			return existing
		}
		delete(s.jobs, src)
	}

	jobCtx, jobCancel := context.WithCancel(s.ctx)
	job := &Job{
		ID:       jobID(src),
		Source:   src,
		phase:    PhasePending,
		done:     make(chan struct{}),
		ctx:      jobCtx,
		cancelFn: jobCancel,
		reporter: s.reporter,
	}
	s.jobs[src] = job
	s.mu.Unlock()

	s.wg.Add(1)
	go s.execute(req, job)
	return job
}

// JobByID 按任务 ID 查找任务句柄，供 HTTP 接口轮询转换进度。
func (s *Scheduler) JobByID(id string) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.jobs {
		if job.ID == id {
			return job, true
		}
	}
	return nil, false
}

// Plan 只做探测与决策，不启动转换，用于调用方决定是否等待。
func (s *Scheduler) Plan(ctx context.Context, req Request) (Decision, Format, error) {
	format, err := s.prober.Probe(ctx, req.Path)
	if err != nil {
		return Decision{Action: ActionFallback, Reason: ReasonProbeFailed}, Format{}, err
	}
	policy := s.policy
	if req.Force {
		policy.AlwaysConvert = true
	}
	return Decide(format, s.conditions(format), policy), format, nil
}

// Close 停止调度器，正在执行的任务会被取消。
func (s *Scheduler) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()
	s.wg.Wait()
}

func (s *Scheduler) execute(req Request, job *Job) {
	defer s.wg.Done()

	ctx := job.ctx
	src := job.Source

	job.setPhase(PhaseProbing)
	format, err := s.prober.Probe(ctx, src)
	if err != nil {
		s.finish(job, Result{Action: ActionFallback, PlayPath: src, Reason: ReasonProbeFailed, Err: err}, err)
		return
	}
	job.setFormat(format)

	policy := s.policy
	if req.Force {
		policy.AlwaysConvert = true
	}
	decision := Decide(format, s.conditions(format), policy)
	job.setDecision(decision)

	switch decision.Action {
	case ActionPlayDirect:
		s.finish(job, Result{
			Action: ActionPlayDirect, PlayPath: src, Reason: ReasonCompatible, Format: format,
		}, nil)
		return
	case ActionDefer, ActionFallback:
		errReason := errForReason(decision.Reason)
		s.finish(job, Result{
			Action: decision.Action, PlayPath: src, Reason: decision.Reason, Format: format, Err: errReason,
		}, errReason)
		return
	}

	out, err := s.cachePath(src)
	if err != nil {
		s.finish(job, Result{
			Action: ActionFallback, PlayPath: src, Reason: ReasonConvertFailed, Format: format, Err: ErrConvertFailed,
		}, ErrConvertFailed)
		return
	}

	if stat, statErr := os.Stat(out); statErr == nil && stat.Size() > 0 {
		// 只有包含 moov 的完整文件才算缓存，避免断电留下的残缺文件被当作结果。
		if isCompleteMP4(out) {
			s.finish(job, Result{
				Action: ActionPlayConverted, PlayPath: out, Format: format,
				FromCache: true, Reason: ReasonCompatible,
			}, nil)
			return
		}
		s.logger.Warn("转码缓存不完整，重新生成", "output", out)
		_ = os.Remove(out)
	}

	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		s.finish(job, Result{
			Action: ActionFallback, PlayPath: src, Reason: ReasonCanceled, Format: format, Err: ErrCanceled,
		}, ErrCanceled)
		return
	}

	job.setOutput(out)
	job.setPhase(PhaseConverting)

	if err := s.convert(ctx, job, format, out, decision.UseHardware); err != nil {
		if ctx.Err() != nil {
			s.finish(job, Result{
				Action: ActionFallback, PlayPath: src, Reason: ReasonCanceled, Format: format, Err: ErrCanceled,
			}, ErrCanceled)
			return
		}
		s.logger.Error("视频转换失败，回退原文件播放", "source", src, "err", err)
		s.finish(job, Result{
			Action: ActionFallback, PlayPath: src, Reason: ReasonConvertFailed, Format: format, Err: ErrConvertFailed,
		}, ErrConvertFailed)
		return
	}

	s.finish(job, Result{
		Action: ActionPlayConverted, PlayPath: out, Format: format, Reason: ReasonCompatible,
	}, nil)
}

func (s *Scheduler) finish(job *Job, result Result, err error) {
	job.complete(result, err)
}

// convert 依次尝试硬件编码与软件编码，任何一次成功即返回。
//
// 输出先写入 .part 临时文件，成功后原子重命名成正式缓存；中断只会留下
// 临时文件，不会污染缓存目录，下次启动时会被清理。
func (s *Scheduler) convert(ctx context.Context, job *Job, format Format, out string, useHardware bool) error {
	var lastErr error
	part := out + ".part"

	for _, attempt := range encoderPlan(useHardware) {
		if ctx.Err() != nil {
			return ErrCanceled
		}

		_ = os.Remove(part)
		args := transcodeArgs(job.Source, part, format, attempt.args)
		err := s.runner.Run(
			ctx,
			s.binaries.FFmpeg,
			args,
			func(line string) { s.handleProgressLine(job, format, line) },
			func(line string) {
				s.logger.Debug("ffmpeg", "line", line, "hardware", attempt.hardware)
			},
		)
		if err == nil {
			if stat, statErr := os.Stat(part); statErr == nil && stat.Size() > 0 {
				err = os.Rename(part, out)
			} else {
				err = ErrConvertFailed
			}
			if err == nil {
				return nil
			}
		}

		lastErr = err
		_ = os.Remove(part)
		s.logger.Warn("视频编码尝试失败", "source", job.Source, "hardware", attempt.hardware, "err", err)
	}

	if lastErr == nil {
		lastErr = ErrConvertFailed
	}
	return lastErr
}

type encoderAttempt struct {
	args     []string
	hardware bool
}

// encoderPlan 返回编码尝试顺序：优先硬件，失败回退软件。
func encoderPlan(useHardware bool) []encoderAttempt {
	software := encoderAttempt{args: softwareEncoderArgs()}
	if !useHardware {
		return []encoderAttempt{software}
	}

	hardwareArgs, ok := hardwareEncoderArgs()
	if !ok {
		return []encoderAttempt{software}
	}
	return []encoderAttempt{
		{args: hardwareArgs, hardware: true},
		software,
	}
}

// handleProgressLine 解析 ffmpeg -progress 输出。
func (s *Scheduler) handleProgressLine(job *Job, format Format, line string) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}

	switch key {
	case "out_time_us", "out_time_ms":
		// out_time_ms 历史原因单位实际是微秒，两者按同一单位处理。
		micros, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || format.DurationMS <= 0 {
			return
		}
		percent := float64(micros) / 1000 / float64(format.DurationMS) * 100
		if percent > 100 {
			percent = 100
		}
		job.setProgress(percent)
	case "speed":
		job.setSpeed(strings.TrimSpace(value))
	}
}

func (s *Scheduler) cachePath(src string) (string, error) {
	stat, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%s|%d|%d|%s", src, stat.Size(), stat.ModTime().UnixNano(), s.target)
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:])[:24] + ".mp4"
	return filepath.Join(s.cacheDir, name), nil
}

func (s *Scheduler) conditions(format Format) Conditions {
	if s.opts.Conditions != nil {
		return s.opts.Conditions(format)
	}

	network := NetworkLocal
	hardware := true
	switch runtime.GOOS {
	case "windows", "darwin":
	default:
		hardware = false
	}

	return Conditions{
		FFmpegAvailable: s.binaries.Available(),
		FreeDiskBytes:   freeDiskBytes(s.cacheDir),
		Network:         network,
		HardwareEncoder: hardware,
		SizeBytes:       format.SizeBytes,
		Target:          s.target,
	}
}

func jobID(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:])[:12]
}

// cleanupPartFiles 清理上次运行中断留下的 .part 临时文件。
func cleanupPartFiles(dir string, log *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			log.Warn("清理未完成的转码文件失败", "path", path, "err", err)
		}
	}
}
