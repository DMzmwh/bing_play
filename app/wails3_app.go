package wailsapp

import (
	"changeme/internal/config"
	"changeme/internal/logger"
	"changeme/internal/service"
	"embed"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func NewWailsApp(
	cfg *config.Config,
	torrentService *service.TorrentService,
	greetService *service.GreetService,
	assets embed.FS,
) *application.App {

	app := application.New(application.Options{
		Name:        cfg.AppName,
		Description: "A demo of using raw HTML & CSS",

		Services: []application.Service{
			application.NewService(greetService),
			application.NewService(torrentService),
		},

		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},

		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},

		Logger: logger.App,

		ShouldQuit: func() bool {
			logger.App.Info("应用准备退出1")
			return true
		},

		OnShutdown: func() {
			logger.App.Info("应用准备退出2")
		},

		PostShutdown: func() {
			logger.App.Info("应用准备退出3")
		},
	})

	app.Window.NewWithOptions(
		application.WebviewWindowOptions{
			Title:  cfg.AppName,
			Width:  1000,
			Height: 618,

			Mac: application.MacWindow{
				InvisibleTitleBarHeight: 50,
				Backdrop:                application.MacBackdropTranslucent,
				TitleBar:                application.MacTitleBarHiddenInset,
			},

			BackgroundColour: application.NewRGB(6, 7, 15),

			URL: "/",
		},
	)
	go func() {
		for {
			now := time.Now().Format(time.RFC1123)
			app.Event.Emit("time", now)
			time.Sleep(time.Second)
		}
	}()

	return app
}
