package app

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/bing/bing-play/internal/assets"
	"github.com/bing/bing-play/internal/container"
	"github.com/bing/bing-play/internal/logger"
	"github.com/bing/bing-play/internal/server/handler"

	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/db"
	"github.com/bing/bing-play/internal/dhtcrawler"
	"github.com/bing/bing-play/internal/i18n"
	"github.com/bing/bing-play/internal/server"
	"github.com/bing/bing-play/internal/service"
	"github.com/bing/bing-play/internal/torrentmgr"
	"github.com/bing/bing-play/internal/transcode"
	"github.com/bing/bing-play/internal/tv"

	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/fx"
)

type App struct {
	fxApp    *fx.App
	wailsApp *application.App
}

// AppIcon 窗口图标 PNG 数据，由 main 从 build/appicon.png 嵌入后传入。
type AppIcon []byte

func NewApp(assets embed.FS, resFS assets.FS, icon AppIcon) *App {
	a := &App{}
	a.fxApp = fx.New(
		// =========================
		// Modules
		// =========================
		config.Module,
		// =========================
		// Logger
		// =========================
		//
		// logger 不通过 fx.Provide
		// 而是在 Config 创建完成后初始化
		//
		fx.Invoke(func(lc fx.Lifecycle, cfg *config.Config) error {
			return logger.Init(
				cfg.DataDir,
				lc,
			)
		}),
		db.Module,
		container.Module,
		i18n.Module,
		handler.Module,
		server.Module,
		service.Module,
		torrentmgr.Module,
		dhtcrawler.Module,
		transcode.Module,
		tv.Module,
		// =========================
		// Wails assets
		// =========================
		fx.Supply(assets),
		fx.Supply(resFS),
		fx.Supply(icon),
		// 强制让后台服务进入 Fx 生命周期
		fx.Invoke(func(
			_ *server.Server,
			_ *torrentmgr.TorrentManager,
			_ *dhtcrawler.Service,
		) {

		}),
		// =========================
		// 创建 Wails App
		// =========================
		fx.Provide(
			NewWailsApp,
		),
		// =========================
		// 获取 Wails App
		// =========================
		fx.Invoke(
			a.captureWailsApp,
		),
	)
	return a
}

func (a *App) captureWailsApp(wailsApp *application.App) {
	a.wailsApp = wailsApp
}

func (a *App) Run() error {
	// =========================
	// 1. 启动 Fx
	// =========================
	startCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := a.fxApp.Start(startCtx); err != nil {
		return fmt.Errorf(
			"start fx: %w",
			err,
		)
	}
	// =========================
	// 2. 启动 Wails
	// =========================
	wailsErr := a.wailsApp.Run()
	// =========================
	// 3. Wails 退出
	// =========================
	stopCtx, stopCancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer stopCancel()

	// =========================
	// 4. 关闭 Fx
	// =========================
	fxErr := a.fxApp.Stop(stopCtx)
	// =========================
	// 5. 错误处理
	// =========================

	if wailsErr != nil {
		return fmt.Errorf(
			"wails run: %w",
			wailsErr,
		)
	}

	if fxErr != nil {
		return fmt.Errorf(
			"fx stop: %w",
			fxErr,
		)
	}

	return nil
}
