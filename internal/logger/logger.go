package logger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/fx"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	App     *slog.Logger
	Torrent *slog.Logger
	Error   *slog.Logger
)

// ModuleConfig 一个日志模块的轮转配置。
// 每个模块写自己的活动文件，超过 MaxSizeMB 后自动切分；
// 历史文件由 lumberjack 自动 gzip 压缩。
type ModuleConfig struct {
	// Name 模块名，app / torrent / error。
	Name string
	// File 活动日志文件名，例如 debug.log。
	File string
	// MaxSizeMB 单个文件最大体积，超过后立即切分并压缩，<=0 使用默认值 50。
	MaxSizeMB int
	// MaxBackups 最多保留多少个历史文件，<=0 使用默认值 10。
	MaxBackups int
	// MaxAgeDays 历史文件最长保留天数，<=0 使用默认值 30。
	MaxAgeDays int
	// Level 采集级别。
	Level slog.Level
}

// RotationConfig 固定时长切分配置。
type RotationConfig struct {
	// Daily 为 true 时在每个自然日 00:00 切分。
	Daily bool
	// Interval 在 Daily=false 时按固定间隔切分，例如 6*time.Hour。
	Interval time.Duration
}

// Config 日志子系统配置。
type Config struct {
	Dir      string
	Modules  []ModuleConfig
	Rotation RotationConfig
}

// DefaultConfig 返回适合桌面客户端的默认配置：
//   - 全部模块使用 Debug 级别，Debug 及以上都会写入
//   - debug.log：50MB 切分，保留 10 个 / 30 天
//   - torrent.log：100MB 切分，保留 5 个 / 14 天
//   - error.log：50MB 切分，保留 10 个 / 60 天
//   - 每天 00:00 自动切分，所有历史文件自动 gzip 压缩
func DefaultConfig(dataDir string) Config {
	return Config{
		Dir: filepath.Join(dataDir, "logs"),
		Modules: []ModuleConfig{
			{Name: "app", File: "debug.log", MaxSizeMB: 50, MaxBackups: 10, MaxAgeDays: 30, Level: slog.LevelDebug},
			{Name: "torrent", File: "torrent.log", MaxSizeMB: 100, MaxBackups: 5, MaxAgeDays: 14, Level: slog.LevelDebug},
			{Name: "error", File: "error.log", MaxSizeMB: 50, MaxBackups: 10, MaxAgeDays: 60, Level: slog.LevelDebug},
		},
		Rotation: RotationConfig{Daily: true},
	}
}

func (c Config) normalize() Config {
	if !c.Rotation.Daily && c.Rotation.Interval <= 0 {
		c.Rotation.Interval = 24 * time.Hour
	}
	for i := range c.Modules {
		if c.Modules[i].MaxSizeMB <= 0 {
			c.Modules[i].MaxSizeMB = 50
		}
		if c.Modules[i].MaxBackups <= 0 {
			c.Modules[i].MaxBackups = 10
		}
		if c.Modules[i].MaxAgeDays <= 0 {
			c.Modules[i].MaxAgeDays = 30
		}
	}
	return c
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Dir) == "" {
		return errors.New("logger: log dir is empty")
	}
	if len(c.Modules) == 0 {
		return errors.New("logger: no log module configured")
	}

	seenName := make(map[string]struct{}, len(c.Modules))
	seenFile := make(map[string]struct{}, len(c.Modules))
	for _, mod := range c.Modules {
		if strings.TrimSpace(mod.Name) == "" || strings.TrimSpace(mod.File) == "" {
			return errors.New("logger: module name and file are required")
		}
		if _, ok := seenName[mod.Name]; ok {
			return fmt.Errorf("logger: duplicate module %q", mod.Name)
		}
		if _, ok := seenFile[mod.File]; ok {
			return fmt.Errorf("logger: duplicate module file %q", mod.File)
		}
		seenName[mod.Name] = struct{}{}
		seenFile[mod.File] = struct{}{}
	}

	for _, required := range []string{"app", "torrent", "error"} {
		if _, ok := seenName[required]; !ok {
			return fmt.Errorf("logger: module %q is required", required)
		}
	}

	return nil
}

var replaceAttr = func(groups []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		return slog.String(
			slog.TimeKey,
			a.Value.Time().Format("2006-01-02 15:04:05"),
		)

	case slog.SourceKey:
		src, ok := a.Value.Any().(*slog.Source)
		if ok && src != nil {
			src.File = shortSource(src.File)
		}
		return a
	}

	return a
}

// shortSource 保留路径最后两段，例如 internal/db/db.go -> db/db.go。
func shortSource(file string) string {
	normalized := filepath.ToSlash(file)
	parts := strings.Split(normalized, "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return normalized
}

// newWriter 同时输出到终端和日志文件。
// 不再用 build tag 区分 dev / prod：开发时终端能直接看到日志，
// 生产环境窗口程序没有终端时写入 stdout 会被系统丢弃，不影响文件落盘。
func newWriter(file io.Writer) io.Writer {
	return io.MultiWriter(os.Stdout, file)
}

// Manager 管理活动日志文件与固定时长切分。
type Manager struct {
	cfg      Config
	writers  []*lumberjack.Logger
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	startOne sync.Once
	stopOne  sync.Once
}

func newManager(cfg Config) (*Manager, error) {
	cfg = cfg.normalize()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("logger: create log dir: %w", err)
	}

	m := &Manager{cfg: cfg}
	for _, mod := range cfg.Modules {
		writer := &lumberjack.Logger{
			Filename:   filepath.Join(cfg.Dir, mod.File),
			MaxSize:    mod.MaxSizeMB,
			MaxBackups: mod.MaxBackups,
			MaxAge:     mod.MaxAgeDays,
			LocalTime:  true,
			Compress:   true,
		}
		m.writers = append(m.writers, writer)
	}
	return m, nil
}

func (m *Manager) buildLoggers() {
	for i, mod := range m.cfg.Modules {
		logger := slog.New(slog.NewJSONHandler(
			newWriter(m.writers[i]),
			&slog.HandlerOptions{
				Level:       mod.Level,
				AddSource:   true,
				ReplaceAttr: replaceAttr,
			},
		))
		switch mod.Name {
		case "app":
			App = logger
		case "torrent":
			Torrent = logger
		case "error":
			Error = logger
		}
	}
}

func (m *Manager) start() {
	m.startOne.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel

		// 应用上次退出时可能跨天，启动先切分昨天的活动文件。
		m.rotateStale()
		m.wg.Add(1)
		go m.rotateLoop(ctx)
	})
}

func (m *Manager) stop() {
	m.stopOne.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		m.wg.Wait()
		for _, writer := range m.writers {
			_ = writer.Close()
		}
	})
}

// rotateStale 启动时检查活动文件是否已超过当前切分周期，是则先切分。
func (m *Manager) rotateStale() {
	now := time.Now()
	var cutoff time.Time
	if m.cfg.Rotation.Daily {
		cutoff = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	} else {
		cutoff = now.Add(-m.cfg.Rotation.Interval)
	}
	for i, mod := range m.cfg.Modules {
		info, err := os.Stat(filepath.Join(m.cfg.Dir, mod.File))
		if err != nil {
			continue
		}
		// 空文件不切分，避免产生大量无内容的归档。
		if info.Size() > 0 && info.ModTime().Before(cutoff) {
			_ = m.writers[i].Rotate()
		}
	}
}

func (m *Manager) rotateLoop(ctx context.Context) {
	defer m.wg.Done()

	if m.cfg.Rotation.Daily {
		for {
			timer := time.NewTimer(time.Until(nextMidnight(time.Now())))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				m.rotateAll()
			}
		}
	}

	ticker := time.NewTicker(m.cfg.Rotation.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.rotateAll()
		}
	}
}

func (m *Manager) rotateAll() {
	for _, writer := range m.writers {
		// 空文件不切分，避免产生大量无内容的归档。
		if info, err := os.Stat(writer.Filename); err == nil && info.Size() == 0 {
			continue
		}
		// 文件被其他程序占用时可能失败，下一次切分周期会再次尝试。
		_ = writer.Rotate()
	}
}

func nextMidnight(now time.Time) time.Time {
	y, m, d := now.Date()
	// 加 1 秒，避免刚好 00:00:00 触发后重复计算当天。
	return time.Date(y, m, d+1, 0, 0, 1, 0, now.Location())
}

var (
	currentMu sync.RWMutex
	current   *Manager
)

// Init 初始化日志并注册 Fx 生命周期。
func Init(dataDir string, lc fx.Lifecycle) error {
	return InitWithConfig(DefaultConfig(dataDir), lc)
}

// InitWithConfig 使用自定义配置初始化日志子系统。
func InitWithConfig(cfg Config, lc fx.Lifecycle) error {
	m, err := newManager(cfg)
	if err != nil {
		return err
	}
	m.buildLoggers()

	currentMu.Lock()
	prev := current
	current = m
	currentMu.Unlock()

	if prev != nil {
		prev.stop()
	}

	if lc != nil {
		lc.Append(fx.Hook{
			OnStart: func(context.Context) error {
				m.start()
				return nil
			},
			OnStop: func(context.Context) error {
				m.stop()
				return nil
			},
		})
	} else {
		m.start()
	}

	return nil
}

func getManager() (*Manager, error) {
	currentMu.RLock()
	m := current
	currentMu.RUnlock()
	if m == nil {
		return nil, errors.New("logger: not initialized")
	}
	return m, nil
}

// Ready 返回日志是否已经初始化。
func Ready() bool {
	_, err := getManager()
	return err == nil
}

// Dir 返回当前日志目录，未初始化时返回空字符串。
func Dir() string {
	m, err := getManager()
	if err != nil {
		return ""
	}
	return m.cfg.Dir
}

// RecordPanic 把崩溃栈写入受管理的 error.log。
func RecordPanic(value string, stack []byte) error {
	if _, err := getManager(); err != nil {
		return err
	}
	if Error == nil {
		return errors.New("logger: error logger unavailable")
	}
	Error.Error(
		"程序发生未捕获的 panic",
		slog.String("panic", value),
		slog.String("stack", string(stack)),
	)
	return nil
}

// Close 关闭全局日志，主要用于测试。
func Close() {
	currentMu.Lock()
	m := current
	current = nil
	currentMu.Unlock()
	if m != nil {
		m.stop()
	}
}
