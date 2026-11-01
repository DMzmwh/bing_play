package app

import (
	"embed"

	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/i18n"
	"github.com/bing/bing-play/internal/logger"
	"github.com/bing/bing-play/internal/server"
	"github.com/bing/bing-play/internal/service"
	"github.com/bing/bing-play/internal/utils/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"go.uber.org/fx"
)

const (
	// EventVideoOpened 后端打开本地视频成功后，把可播放的资源信息推给前端。
	EventVideoOpened = "video:opened"
	// EventVideoOpenFailed 后端打开本地视频失败时，把错误信息推给前端。
	EventVideoOpenFailed = "video:open-failed"
	// VideoDropAreaID 中间播放区域的元素 id，只有拖到该区域的文件才会被处理。
	VideoDropAreaID = "play-drop-area"
)

// VideoOpenedPayload 拖拽完成后推送给前端的当前视频。
// 播放列表由前端通过 VideoService.ListPlaylist 重新获取。
type VideoOpenedPayload struct {
	Current *service.OpenVideoResult `json:"current"`
}

var Version = "v1.0.1"

type WailsAppParams struct {
	fx.In

	Config       *config.Config
	Services     []application.Service `group:"wails-services"`
	Assets       embed.FS
	AppIcon      AppIcon
	VideoService *service.VideoService
	I18n         *i18n.I18nService
	HTTPServer   *server.Server
}

func NewWailsApp(p WailsAppParams) *application.App {
	cfg := p.Config
	assets := p.Assets
	services := p.Services
	videoService := p.VideoService
	i18nService := p.I18n
	httpServer := p.HTTPServer

	app := application.New(application.Options{
		Name:        cfg.AppName,
		Description: "A demo of using raw HTML & CSS",

		Services: services,

		// 窗口与任务栏图标：使用品牌图标，避免回退到 Wails 默认图标。
		Icon: []byte(p.AppIcon),

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
			// 关闭顺序：HTTP 先停，下载引擎等资源后停。
			// OnShutdown 早于 Wails 的 ServiceShutdown 与 Fx 的 OnStop，
			// 这里先断开监听和活动连接，避免退出过程中仍有请求访问已释放的资源。
			if httpServer != nil {
				if err := httpServer.Close(); err != nil {
					logger.Error.Error("关闭 http server 失败", "error", err)
				}
			}
		},

		PostShutdown: func() {
			logger.App.Info("应用准备退出3")
		},
	})

	win := app.Window.NewWithOptions(
		application.WebviewWindowOptions{
			Title:     cfg.AppName,
			Width:     1000,
			Height:    618,
			Frameless: true, // ✅ 核心：去掉系统标题栏、去掉窗口边框
			// 开启系统文件拖拽，前端才能拿到拖入文件的真实路径
			EnableFileDrop: true,
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

	// 系统拖拽完成后直接在后端处理：路径本来就在 Go 侧，
	// 不必再转给前端、让前端绕一圈 HTTP 请求回来。
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		files := event.Context().DroppedFiles()
		if len(files) == 0 {
			return
		}

		// 只处理拖到中间播放区域的文件，其它区域交给各自的业务
		details := event.Context().DropTargetDetails()
		if details == nil || details.ElementID != VideoDropAreaID {
			return
		}

		// 逐个登记拖入的文件：无效文件（文件夹、非视频等）只记录日志并跳过，
		// 不影响同一批次的其它视频；重复路径按资源 ID 去重。
		var (
			results  []*service.OpenVideoResult
			firstErr error
			seen     = make(map[int64]struct{}, len(files))
		)
		for _, file := range files {
			result, err := videoService.OpenLocalVideo(file)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				logger.App.Warn("打开拖入的视频失败", "path", file, "err", err)
				continue
			}
			if _, exists := seen[result.ID]; exists {
				continue
			}
			seen[result.ID] = struct{}{}
			results = append(results, result)
		}

		if len(results) == 0 {
			if firstErr == nil {
				firstErr = service.ErrVideoOpenFailed
			}
			app.Event.Emit(EventVideoOpenFailed, map[string]string{
				"message": i18nService.T(firstErr.Error(), nil),
			})
			return
		}

		app.Event.Emit(EventVideoOpened, VideoOpenedPayload{
			Current: results[0],
		})
	})

	updater.Init(app, Version)
	return app
}
