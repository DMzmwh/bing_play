package logger

import (
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	App     *slog.Logger
	Torrent *slog.Logger
	Error   *slog.Logger
)

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
			src.File = filepath.Base(src.File)
		}
		return a
	}

	return a
}

func Init(dataDir string) error {
	logDir := filepath.Join(dataDir, "logs")

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	appWriter := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "debug.log"),
		MaxSize:    100,
		MaxBackups: 10,
		MaxAge:     30,
		Compress:   true,
	}

	torrentWriter := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "torrent.log"),
		MaxSize:    200,
		MaxBackups: 5,
		MaxAge:     7,
		Compress:   true,
	}

	errorWriter := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "error.log"),
		MaxSize:    100,
		MaxBackups: 10,
		MaxAge:     60,
		Compress:   true,
	}

	commonOpts := &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		AddSource:   true,
		ReplaceAttr: replaceAttr,
	}

	App = slog.New(
		slog.NewJSONHandler(newWriter(appWriter), commonOpts),
	)

	Torrent = slog.New(
		slog.NewJSONHandler(newWriter(torrentWriter), commonOpts),
	)

	Error = slog.New(
		slog.NewJSONHandler(
			newWriter(errorWriter),
			&slog.HandlerOptions{
				Level:       slog.LevelError,
				AddSource:   true,
				ReplaceAttr: replaceAttr,
			},
		),
	)

	return nil
}
