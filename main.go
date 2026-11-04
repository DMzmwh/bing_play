package main

import (
	"embed"
	"fmt"
	"github.com/bing/bing-play/app"
	resourceassets "github.com/bing/bing-play/internal/assets"
	"github.com/bing/bing-play/internal/logger"
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

// 内置语言包与数据库迁移，保证自动更新只替换 exe 时也能拿到最新文案和迁移。
// 语言包仍允许运行目录下的 resources/locales 覆盖，迁移只使用内置版本。
//
//go:embed resources/locales/*.json
//go:embed resources/migrations/*.sql
var resourceAssets embed.FS

//go:embed build/appicon.png
var appIcon []byte

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
	// 全局捕获panic，优先写入受管理的 error.log
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			// 日志尚未初始化或写盘失败时，保留旧的 exe 同目录兜底文件
			if logErr := logger.RecordPanic(fmt.Sprint(r), stack); logErr != nil {
				logPath, _ := filepath.Abs("app.log")
				_ = os.WriteFile(logPath, stack, 0644)
			}
			// 重新panic，让程序退出
			panic(r)
		}
	}()

	bz := app.NewApp(assets, resourceassets.FS(resourceAssets), app.AppIcon(appIcon))
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
