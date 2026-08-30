package main

import (
	"embed"
	"github.com/bing/bing-play/app"
	"os"
	"path/filepath"
	"runtime/debug"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Register a custom event whose associated data type is string.
	// This is not required, but the binding generator will pick up registered events
	// and provide a strongly typed JS/TS API for them.
	//application.RegisterEvent[string]("time")
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {
	// 全局捕获panic，写入日志文件
	defer func() {
		if r := recover(); r != nil {
			// 日志输出到exe同目录 app.log
			logPath, _ := filepath.Abs("app.log")
			_ = os.WriteFile(logPath, debug.Stack(), 0644)
			// 重新panic，让程序退出
			panic(r)
		}
	}()

	bz := app.NewApp(assets)
	if err := bz.Run(); err != nil {
		logPath, _ := filepath.Abs("app.log")
		_ = os.WriteFile(
			logPath,
			[]byte(err.Error()+"\n"),
			0644,
		)
		os.Exit(1)
	}
}
