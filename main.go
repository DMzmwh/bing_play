package main

import (
	"changeme/internal/config"
	"changeme/internal/logger"
	"changeme/internal/service"
	"changeme/internal/start"
	"changeme/internal/torrentmgr"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	application.RegisterEvent[string]("time")
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
	configInfo := config.Load()
	appName := configInfo.AppName

	errLogger := logger.Init(configInfo.DataDir)
	if errLogger != nil {
		panic(errLogger)
	}
	fmt.Print("----------" + appName)
	logger.App.Info("application start")

	startUp := start.NewStartup(configInfo)

	manager, errLogger := torrentmgr.NewManager(startUp.Cn)
	if errLogger != nil {
		panic(errLogger)
	}
	// 初始化 Service
	torrentService := service.NewTorrentService(manager)

	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running an macOS.
	app := application.New(application.Options{
		Name:        appName,
		Description: "A demo of using raw HTML & CSS",
		Services: []application.Service{
			application.NewService(&GreetService{}), application.NewService(torrentService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Logger: logger.App,
		ShouldQuit: func() bool {
			log.Println("应用准备退出1")
			return true
		},
		OnShutdown: func() {
			log.Println("应用准备退出2")
		},
		PostShutdown: func() {
			log.Println("应用准备退出3")
		},
	})

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Window 1",
		// Window sized to the golden ratio (1000 / 618 ≈ 1.618).
		Width:  1000,
		Height: 618,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	// Create a goroutine that emits an event containing the current time every second.
	// The frontend can listen to this event and update the UI accordingly.
	go func() {
		for {
			now := time.Now().Format(time.RFC1123)
			app.Event.Emit("time", now)
			time.Sleep(time.Second)
		}
	}()

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
