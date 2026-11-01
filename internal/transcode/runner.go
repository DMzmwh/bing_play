package transcode

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"sync"
)

// Runner 执行外部命令的抽象，便于单元测试替换。
type Runner interface {
	// Run 逐行回调 stdout / stderr，命令结束时返回执行错误。
	Run(ctx context.Context, exe string, args []string, onStdout, onStderr func(string)) error
	// Output 返回命令的标准输出。
	Output(ctx context.Context, exe string, args []string) ([]byte, error)
}

// execRunner 基于 os/exec 的默认实现。
type execRunner struct{}

// NewExecRunner 创建默认的命令执行器。
func NewExecRunner() Runner {
	return execRunner{}
}

func (execRunner) Run(
	ctx context.Context,
	exe string,
	args []string,
	onStdout, onStderr func(string),
) error {
	cmd := exec.CommandContext(ctx, exe, args...)
	hideWindow(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanLines(stdout, onStdout)
	}()
	go func() {
		defer wg.Done()
		scanLines(stderr, onStderr)
	}()

	wg.Wait()
	return cmd.Wait()
}

func (execRunner) Output(ctx context.Context, exe string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	hideWindow(cmd)
	return cmd.Output()
}

func scanLines(r io.Reader, fn func(string)) {
	if fn == nil {
		_, _ = io.Copy(io.Discard, r)
		return
	}
	scanner := bufio.NewScanner(r)
	// ffmpeg 的 stderr 单行可能较长，放宽缓冲上限。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		fn(scanner.Text())
	}
}
