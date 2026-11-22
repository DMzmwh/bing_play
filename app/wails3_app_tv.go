//go:build android

package app

import (
	"context"
	"embed"
	"log/slog"

	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/i18n"
	"github.com/bing/bing-play/internal/logger"
	"github.com/bing/bing-play/internal/server"
	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/fx"
)

// newWailsLogger 返回只保留 Info 及以上级别的 Wails 系统日志器。
// 与桌面版（wails3_app.go）保持一致，避免电视频道快照等大对象产生 MB 级日志。
func newWailsLogger(base *slog.Logger) *slog.Logger {
	if base == nil {
		return nil
	}
	return slog.New(&minLevelHandler{Handler: base.Handler(), min: slog.LevelInfo})
}

// minLevelHandler 在已有 Handler 上再叠加一个最小级别限制。
type minLevelHandler struct {
	slog.Handler
	min slog.Level
}

func (h *minLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.min && h.Handler.Enabled(ctx, level)
}

func (h *minLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &minLevelHandler{Handler: h.Handler.WithAttrs(attrs), min: h.min}
}

func (h *minLevelHandler) WithGroup(name string) slog.Handler {
	return &minLevelHandler{Handler: h.Handler.WithGroup(name), min: h.min}
}

// WailsAppParams TV 版窗口依赖：去掉了桌面端的 VideoService。
type WailsAppParams struct {
	fx.In

	Config     *config.Config
	Services   []application.Service `group:"wails-services"`
	Assets     embed.FS
	AppIcon    AppIcon
	I18n       *i18n.I18nService
	HTTPServer *server.Server
}

// NewWailsApp 创建 Android TV 版 Wails 应用与窗口。
//
// 与桌面版的差异：
//   - 不设固定窗口尺寸，由 Android TV 系统全屏展示；
//   - 不启用 Frameless / EnableFileDrop 等桌面专属选项；
//   - 不接入桌面端的文件拖拽处理与更新器（Updater），电视端不需要。
func NewWailsApp(p WailsAppParams) *application.App {
	cfg := p.Config
	assets := p.Assets
	services := p.Services
	httpServer := p.HTTPServer

	app := application.New(application.Options{
		Name:        cfg.AppName,
		Description: "Bing Play TV",

		Services: services,

		Icon: []byte(p.AppIcon),

		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},

		Logger: newWailsLogger(logger.App),

		ShouldQuit: func() bool {
			return true
		},

		OnShutdown: func() {
			// 关闭顺序：HTTP 先停，避免退出过程中仍有请求访问已释放的资源。
			if httpServer != nil {
				if err := httpServer.Close(); err != nil {
					logger.Error.Error("关闭 http server 失败", "error", err)
				}
			}
		},
	})

	// TV 端只创建一个全屏窗口，加载前端根路由（前端会按 IS_TV 只渲染 TV 页面）。
	app.Window.NewWithOptions(
		application.WebviewWindowOptions{
			Title: cfg.AppName,
			URL:   "/",
		},
	)

	return app
}
