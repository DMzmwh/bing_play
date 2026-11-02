package logger

// 这个文件按“使用目的”拆成多个测试方法，方便逐个运行查看打印效果：
//
//	1. TestLogLevels             查看 Debug / Info / Warn / Error 各写到哪里
//	2. TestLogFields             查看 key/value 与 slog.String、slog.Int 的结构化输出
//	3. TestLogModules            查看 app / torrent / error 三个模块分别落到哪个文件
//	4. TestLogRotateBySize       查看超过固定大小后归档并 gzip
//	5. TestLogRotateByInterval   查看到达固定时长后归档并 gzip
//
// 运行单个示例：
//	go test ./internal/logger -run TestLogLevels -v
//	go test ./internal/logger -run TestLogFields -v
//	go test ./internal/logger -run TestLogModules -v
//	go test ./internal/logger -run TestLogRotateBySize -v
//	go test ./internal/logger -run TestLogRotateByInterval -v
//
// 所有示例都写入项目目录 data/logs/<示例名>/，可以直接用编辑器打开查看。
// 开发环境应用本身也会写 ./data/logs，因为 config_dev.go 的 DataDir 是 "./data"。

import (
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestConfig 生成写入项目 data/logs/<name>/ 的测试配置。
func newTestConfig(t *testing.T, name string) Config {
	t.Helper()

	// go test 的工作目录是包目录 internal/logger，所以 ../../ 是项目根目录。
	dir := filepath.Join("..", "..", "data", "logs", name)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("清理测试日志目录失败: %v", err)
	}

	cfg := DefaultConfig(".")
	cfg.Dir = dir
	for i := range cfg.Modules {
		cfg.Modules[i].MaxSizeMB = 1
		cfg.Modules[i].MaxBackups = 2
		cfg.Modules[i].MaxAgeDays = 1
	}
	// 测试里不让每日定时器干扰，固定间隔示例会单独覆盖这项。
	cfg.Rotation = RotationConfig{Daily: false, Interval: time.Hour}
	return cfg
}

// startTestLogger 初始化日志，并在测试结束时关闭文件句柄。
func startTestLogger(t *testing.T, cfg Config) {
	t.Helper()
	if err := InitWithConfig(cfg, nil); err != nil {
		t.Fatalf("初始化日志失败: %v", err)
	}
	t.Cleanup(Close)
}

// dumpFile 打印明文日志文件内容。
func dumpFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取日志文件失败: %v", err)
	}
	fmt.Printf("\n--- %s ---\n%s", path, data)
}

// listLogDir 打印日志目录下所有文件及大小。
func listLogDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}
	fmt.Printf("\n日志目录: %s\n", dir)
	for _, entry := range entries {
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		fmt.Printf("  %-46s %8d bytes\n", entry.Name(), info.Size())
	}
}

// waitForArchive 等待 debug-*.log.gz 完整生成；lumberjack 的压缩是异步的。
func waitForArchive(t *testing.T, dir string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(dir, "debug-*.log.gz"))
		for _, file := range files {
			if gzComplete(file) {
				return file
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s 未生成完整的 debug-*.log.gz 归档", dir)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// gzComplete 判断 gzip 文件是否已经写完并能完整解压。
func gzComplete(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return false
	}
	defer reader.Close()

	_, err = io.ReadAll(reader)
	return err == nil
}

// dumpArchive 解压归档并打印总行数和前两行。
func dumpArchive(t *testing.T, path string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开归档失败: %v", err)
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("解压归档失败: %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取归档失败: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	fmt.Printf("\n--- %s（共 %d 行，前 2 行）---\n", path, len(lines))
	for i, line := range lines {
		if i >= 2 {
			break
		}
		fmt.Println(line)
	}
}

// silenceStdout 临时把标准输出指向空设备，避免“大小切分”示例刷屏。
// 日志文件仍然正常写入，返回的函数用于恢复标准输出。
func silenceStdout(t *testing.T) func() {
	t.Helper()
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("打开空设备失败: %v", err)
	}
	old := os.Stdout
	os.Stdout = devNull
	restore := func() {
		os.Stdout = old
		_ = devNull.Close()
	}
	t.Cleanup(restore)
	return restore
}

// TestLogLevels 查看不同级别的日志分别写到哪个文件。
//
// 预期效果：
//
//	debug.log 里依次是 DEBUG、INFO、WARN 三条；
//	error.log 里是 ERROR 一条（error 模块只接收 Error 调用）。
//
// 每行都是 JSON，例如：
//
//	{"time":"...","level":"DEBUG","source":{...},"msg":"debug 级别：调试细节","step":1}
func TestLogLevels(t *testing.T) {
	cfg := newTestConfig(t, "levels")
	startTestLogger(t, cfg)

	App.Debug("debug 级别：调试细节", "step", 1)
	App.Info("info 级别：正常流程")
	App.Warn("warn 级别：需要注意")
	Error.Error("error 级别：出错了")

	// 先关闭文件句柄，Windows 下才能立即读到最新大小和内容。
	Close()

	dumpFile(t, filepath.Join(cfg.Dir, "debug.log"))
	dumpFile(t, filepath.Join(cfg.Dir, "error.log"))
}

// TestLogFields 查看结构化字段的两种写法。
//
// 预期效果：
//
//	弱类型： {"msg":"弱类型写法","user":"tom","age":18}
//	强类型： {"msg":"强类型写法","user":"tom","age":18,"vip":true}
func TestLogFields(t *testing.T) {
	cfg := newTestConfig(t, "fields")
	startTestLogger(t, cfg)

	// key/value 写法简单，但类型不明确。
	App.Debug("弱类型写法", "user", "tom", "age", 18)

	// 推荐写法：用 slog.String / slog.Int / slog.Bool，字段类型固定，便于检索。
	App.Debug("强类型写法",
		slog.String("user", "tom"),
		slog.Int("age", 18),
		slog.Bool("vip", true),
	)

	Close()
	dumpFile(t, filepath.Join(cfg.Dir, "debug.log"))
}

// TestLogModules 查看三个模块分别写入自己的活动文件。
//
// 预期效果：
//
//	debug.log    只有 app 模块的日志
//	torrent.log  只有 torrent 模块的日志
//	error.log    只有 error 模块的日志
func TestLogModules(t *testing.T) {
	cfg := newTestConfig(t, "modules")
	startTestLogger(t, cfg)

	App.Debug("app 模块：业务日志")
	Torrent.Debug("torrent 模块：下载日志", "info_hash", "abc123")
	Error.Error("error 模块：错误日志", "err", "connection timeout")

	Close()

	dumpFile(t, filepath.Join(cfg.Dir, "debug.log"))
	dumpFile(t, filepath.Join(cfg.Dir, "torrent.log"))
	dumpFile(t, filepath.Join(cfg.Dir, "error.log"))
}

// TestLogRotateBySize 查看“固定大小”打包。
//
// 配置里 MaxSizeMB=1，持续写入超过 1MB 后：
//   - 旧的 debug.log 被归档为 debug-<时间>.log.gz；
//   - 新的 debug.log 从空文件继续写。
//
// 预期目录：
//
//	debug-<时间>.log.gz   约 20KB（1MB 文本压缩后）
//	debug.log             剩余的少量日志
//	error.log / torrent.log 为空或不存在
func TestLogRotateBySize(t *testing.T) {
	restore := silenceStdout(t)

	cfg := newTestConfig(t, "rotate-size")
	startTestLogger(t, cfg)

	for i := 0; i < 4300; i++ {
		App.Debug("批量写入",
			slog.Int("seq", i),
			slog.String("payload", strings.Repeat("x", 40)),
		)
	}

	Close()
	restore()

	archive := waitForArchive(t, cfg.Dir)
	listLogDir(t, cfg.Dir)
	dumpArchive(t, archive)
}

// TestLogRotateByInterval 查看“固定时长”打包。
//
// 配置改成每 1 秒切分一次，写入 1.2 秒：
//   - 第 1 秒前写入的日志进入归档；
//   - 切分后写入的日志进入新的 debug.log。
//
// 预期目录：
//
//	debug-<时间>.log.gz   切分前的日志
//	debug.log             “第二段”那条日志
func TestLogRotateByInterval(t *testing.T) {
	cfg := newTestConfig(t, "rotate-interval")
	cfg.Rotation = RotationConfig{Daily: false, Interval: time.Second}
	startTestLogger(t, cfg)

	App.Debug("第一段：切分前写入")
	time.Sleep(1200 * time.Millisecond)
	App.Debug("第二段：切分后写入")

	Close()

	archive := waitForArchive(t, cfg.Dir)
	listLogDir(t, cfg.Dir)
	dumpArchive(t, archive)
	dumpFile(t, filepath.Join(cfg.Dir, "debug.log"))
}
