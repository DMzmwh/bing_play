package app

import (
	"changeme/internal/config"
	"changeme/internal/logger"
	"changeme/internal/utils/updater"
	"embed"
	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/fx"
)

var Version = "v1.0.1"

type WailsAppParams struct {
	fx.In

	Config   *config.Config
	Services []application.Service `group:"wails-services"`
	Assets   embed.FS
}

func NewWailsApp(p WailsAppParams) *application.App {
	cfg := p.Config
	assets := p.Assets
	services := p.Services

	app := application.New(application.Options{
		Name:        cfg.AppName,
		Description: "A demo of using raw HTML & CSS",

		Services: services,

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
			Title:     cfg.AppName,
			Width:     1000,
			Height:    618,
			Frameless: true, // ✅ 核心：去掉系统标题栏、去掉窗口边框
			Mac: application.MacWindow{
				InvisibleTitleBarHeight: 50,
				Backdrop:                application.MacBackdropTranslucent,
				TitleBar:                application.MacTitleBarHidden, // mac彻底隐藏标题栏
			},
			Windows: application.WindowsWindow{
				//HiddenOnTaskbar: true, // Windows隐藏标题栏
				// Windows可选：开启亚克力/云母透明效果（Win11）
				// BackdropType: application.BackdropMica,
			},
			BackgroundColour: application.NewRGB(6, 7, 15),

			URL: "/",
		},
	)
	updater.Init(app, Version)
	return app
}
