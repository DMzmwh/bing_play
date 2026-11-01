package transcode

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeProber struct {
	format Format
	err    error
}

func (p *fakeProber) Probe(_ context.Context, _ string) (Format, error) {
	return p.format, p.err
}

type runCall struct {
	hardware bool
	args     []string
}

type fakeRunner struct {
	mu           sync.Mutex
	calls        []runCall
	probeOutput  []byte
	failHardware bool
	failAll      bool
	block        chan struct{}
}

func (r *fakeRunner) Output(_ context.Context, _ string, _ []string) ([]byte, error) {
	return r.probeOutput, nil
}

func (r *fakeRunner) Run(
	ctx context.Context,
	_ string,
	args []string,
	onStdout, _ func(string),
) error {
	hardware := looksLikeHardwareArgs(args)

	r.mu.Lock()
	r.calls = append(r.calls, runCall{hardware: hardware, args: args})
	r.mu.Unlock()

	if r.block != nil {
		select {
		case <-r.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.failAll || (hardware && r.failHardware) {
		return errors.New("fake ffmpeg failed")
	}

	if len(args) > 0 {
		dst := args[len(args)-1]
		if err := os.WriteFile(dst, buildMP4("isom", "avc1", "mp4a"), 0o644); err != nil {
			return err
		}
	}
	if onStdout != nil {
		onStdout("out_time_us=5000000")
		onStdout("speed=2.5x")
		onStdout("progress=end")
	}
	return nil
}

func (r *fakeRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *fakeRunner) firstCall() runCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return runCall{}
	}
	return r.calls[0]
}

type snapshotRecorder struct {
	mu        sync.Mutex
	snapshots []Snapshot
}

func (r *snapshotRecorder) OnProgress(snapshot Snapshot) {
	r.mu.Lock()
	r.snapshots = append(r.snapshots, snapshot)
	r.mu.Unlock()
}

func (r *snapshotRecorder) maxProgress() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	best := 0.0
	for _, snapshot := range r.snapshots {
		if snapshot.Progress > best {
			best = snapshot.Progress
		}
	}
	return best
}

func newTestScheduler(t *testing.T, format Format, runner *fakeRunner, cond *Conditions) *Scheduler {
	t.Helper()

	options := Options{
		CacheDir: filepath.Join(t.TempDir(), "cache"),
		Policy:   DefaultPolicy(),
		Workers:  1,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Prober:   &fakeProber{format: format},
		Runner:   runner,
		Binaries: Binaries{FFmpeg: "ffmpeg", FFprobe: "ffprobe"},
	}
	if cond != nil {
		options.Conditions = func(Format) Conditions { return *cond }
	}

	scheduler := NewScheduler(options)
	t.Cleanup(scheduler.Close)
	return scheduler
}

func waitJob(t *testing.T, job *Job) (Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return job.Wait(ctx)
}

func tempVideo(t *testing.T) string {
	t.Helper()
	return writeTemp(t, "sample.mp4", buildMP4("isom", "hvc1", "mp4a"))
}

func TestSchedulerPlaysCompatibleFileDirectly(t *testing.T) {
	runner := &fakeRunner{}
	format := Format{
		Container:  ContainerMP4,
		VideoCodec: CodecH264,
		AudioCodec: CodecAAC,
		HasVideo:   true,
		HasAudio:   true,
	}
	scheduler := newTestScheduler(t, format, runner, nil)

	job := scheduler.Submit(Request{Path: tempVideo(t)})
	result, err := waitJob(t, job)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if result.Action != ActionPlayDirect {
		t.Fatalf("action = %s, want play_direct", result.Action)
	}
	if runner.callCount() != 0 {
		t.Fatalf("ffmpeg should not run, got %d calls", runner.callCount())
	}
}

func TestSchedulerConvertsAndCaches(t *testing.T) {
	runner := &fakeRunner{}
	format := hevcFormat(10 << 20)
	format.DurationMS = 10000
	scheduler := newTestScheduler(t, format, runner, nil)

	src := tempVideo(t)

	first := scheduler.Submit(Request{Path: src})
	result, err := waitJob(t, first)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if result.Action != ActionPlayConverted {
		t.Fatalf("action = %s, want play_converted", result.Action)
	}
	if result.FromCache {
		t.Fatal("first run should not come from cache")
	}
	if _, statErr := os.Stat(result.PlayPath); statErr != nil {
		t.Fatalf("converted file missing: %v", statErr)
	}

	second := scheduler.Submit(Request{Path: src})
	result, err = waitJob(t, second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !result.FromCache {
		t.Fatal("second run should hit cache")
	}
	if runner.callCount() != 1 {
		t.Fatalf("ffmpeg calls = %d, want 1", runner.callCount())
	}
}

func TestSchedulerFallsBackFromHardwareToSoftware(t *testing.T) {
	if _, ok := hardwareEncoderArgs(); !ok {
		t.Skip("平台没有默认硬件编码器")
	}

	runner := &fakeRunner{failHardware: true}
	scheduler := newTestScheduler(t, hevcFormat(10<<20), runner, nil)

	job := scheduler.Submit(Request{Path: tempVideo(t)})
	result, err := waitJob(t, job)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if result.Action != ActionPlayConverted {
		t.Fatalf("action = %s, want play_converted", result.Action)
	}
	if runner.callCount() != 2 {
		t.Fatalf("ffmpeg calls = %d, want 2", runner.callCount())
	}
	if !runner.firstCall().hardware {
		t.Fatal("first attempt should use hardware encoder")
	}
}

func TestSchedulerFallsBackWhenConversionFails(t *testing.T) {
	runner := &fakeRunner{failAll: true}
	scheduler := newTestScheduler(t, hevcFormat(10<<20), runner, nil)

	src := tempVideo(t)
	job := scheduler.Submit(Request{Path: src})
	result, err := waitJob(t, job)
	if err == nil {
		t.Fatal("expected conversion error")
	}
	if !errors.Is(err, ErrConvertFailed) {
		t.Fatalf("err = %v, want ErrConvertFailed", err)
	}
	if result.Action != ActionFallback {
		t.Fatalf("action = %s, want fallback", result.Action)
	}
	if result.PlayPath != src {
		t.Fatalf("fallback path = %s, want source", result.PlayPath)
	}
}

func TestSchedulerDefersOnMeteredNetwork(t *testing.T) {
	cond := goodConditions()
	cond.Network = NetworkMetered
	runner := &fakeRunner{}
	scheduler := newTestScheduler(t, hevcFormat(10<<20), runner, &cond)

	job := scheduler.Submit(Request{Path: tempVideo(t)})
	result, err := waitJob(t, job)
	if !errors.Is(err, ErrDeferredNetwork) {
		t.Fatalf("err = %v, want ErrDeferredNetwork", err)
	}
	if result.Action != ActionDefer {
		t.Fatalf("action = %s, want defer", result.Action)
	}
	if runner.callCount() != 0 {
		t.Fatalf("ffmpeg should not run, got %d calls", runner.callCount())
	}
}

func TestSchedulerSkipsWhenDiskInsufficient(t *testing.T) {
	cond := goodConditions()
	cond.FreeDiskBytes = 1 << 20
	runner := &fakeRunner{}
	scheduler := newTestScheduler(t, hevcFormat(1<<30), runner, &cond)

	job := scheduler.Submit(Request{Path: tempVideo(t)})
	result, err := waitJob(t, job)
	if !errors.Is(err, ErrInsufficientDisk) {
		t.Fatalf("err = %v, want ErrInsufficientDisk", err)
	}
	if result.Action != ActionFallback {
		t.Fatalf("action = %s, want fallback", result.Action)
	}
}

func TestSchedulerMergesConcurrentSubmits(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{})}
	scheduler := newTestScheduler(t, hevcFormat(10<<20), runner, nil)

	src := tempVideo(t)
	first := scheduler.Submit(Request{Path: src})
	second := scheduler.Submit(Request{Path: src})

	if first != second {
		t.Fatal("concurrent submits should share one job")
	}

	close(runner.block)
	// 第一个任务完成后缓存已存在，第二个句柄是同一个任务。
	if _, err := waitJob(t, second); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if runner.callCount() != 1 {
		t.Fatalf("ffmpeg calls = %d, want 1", runner.callCount())
	}
}

func TestSchedulerCancelsRunningJob(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{})}
	scheduler := newTestScheduler(t, hevcFormat(10<<20), runner, nil)

	job := scheduler.Submit(Request{Path: tempVideo(t)})

	deadline := time.Now().Add(5 * time.Second)
	for job.Snapshot().Phase != PhaseConverting && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	job.Cancel()

	result, err := waitJob(t, job)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("err = %v, want ErrCanceled", err)
	}
	if result.Action != ActionFallback {
		t.Fatalf("action = %s, want fallback", result.Action)
	}
	close(runner.block)
}

func TestSchedulerLookupJobAndResult(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{})}
	scheduler := newTestScheduler(t, hevcFormat(10<<20), runner, nil)

	job := scheduler.Submit(Request{Path: tempVideo(t)})

	found, ok := scheduler.JobByID(job.ID)
	if !ok || found != job {
		t.Fatal("JobByID should return the submitted job")
	}
	if result, _ := job.Result(); result.Action != "" {
		t.Fatalf("result before completion = %s, want empty", result.Action)
	}

	close(runner.block)
	if _, err := waitJob(t, job); err != nil {
		t.Fatalf("wait: %v", err)
	}

	result, err := job.Result()
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if result.Action != ActionPlayConverted {
		t.Fatalf("action = %s, want play_converted", result.Action)
	}
	if _, ok := scheduler.JobByID("missing"); ok {
		t.Fatal("unknown job id should not be found")
	}
}

func TestSchedulerRemovesIncompleteCache(t *testing.T) {
	runner := &fakeRunner{}
	scheduler := newTestScheduler(t, hevcFormat(10<<20), runner, nil)
	src := tempVideo(t)

	out, err := scheduler.cachePath(src)
	if err != nil {
		t.Fatalf("cachePath: %v", err)
	}
	if err := os.WriteFile(out, []byte("truncated"), 0o644); err != nil {
		t.Fatalf("write incomplete cache: %v", err)
	}

	job := scheduler.Submit(Request{Path: src})
	result, err := waitJob(t, job)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if result.FromCache {
		t.Fatal("incomplete cache should not be treated as a hit")
	}
	if result.Action != ActionPlayConverted {
		t.Fatalf("action = %s, want play_converted", result.Action)
	}
	if runner.callCount() != 1 {
		t.Fatalf("ffmpeg calls = %d, want 1", runner.callCount())
	}
	if !isCompleteMP4(result.PlayPath) {
		t.Fatal("converted cache should be complete")
	}
}

func TestSchedulerCleansPartialFilesOnStart(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	part := filepath.Join(cacheDir, "leftover.mp4.part")
	cached := filepath.Join(cacheDir, "leftover.mp4")
	if err := os.WriteFile(part, []byte("partial"), 0o644); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := os.WriteFile(cached, []byte("cached"), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	scheduler := NewScheduler(Options{
		CacheDir: cacheDir,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(scheduler.Close)

	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf(".part 文件应在启动时被清理: %v", err)
	}
	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("正式缓存不应被清理: %v", err)
	}
}

func TestSchedulerReportsProgress(t *testing.T) {
	recorder := &snapshotRecorder{}
	runner := &fakeRunner{}
	format := hevcFormat(10 << 20)
	format.DurationMS = 10000

	scheduler := NewScheduler(Options{
		CacheDir: t.TempDir(),
		Policy:   DefaultPolicy(),
		Workers:  1,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Prober:   &fakeProber{format: format},
		Runner:   runner,
		Binaries: Binaries{FFmpeg: "ffmpeg"},
		Reporter: recorder,
	})
	t.Cleanup(scheduler.Close)

	job := scheduler.Submit(Request{Path: tempVideo(t)})
	if _, err := waitJob(t, job); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if progress := recorder.maxProgress(); progress < 49 || progress > 51 {
		t.Fatalf("reported progress = %f, want about 50", progress)
	}
}
