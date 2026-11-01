package server

import (
	"context"
	"fmt"
	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/logger"
	"net"
	"net/http"

	"go.uber.org/fx"
)

type Server struct {
	httpServer *http.Server
	mux        *http.ServeMux
}

func NewServer(
	cfg *config.Config,
	route *Route,
	lc fx.Lifecycle,
) *Server {

	mux := http.NewServeMux()

	s := &Server{
		mux: mux,
		httpServer: &http.Server{
			Addr:    cfg.ServerAddr,
			Handler: mux,
		},
	}

	fmt.Println("开启 http server----1------------")
	// 注册路由
	route.Register(mux)

	lc.Append(fx.Hook{
		// Fx启动阶段
		OnStart: func(ctx context.Context) error {

			logger.App.Info(
				"开启 http server",
				"addr",
				s.httpServer.Addr,
			)

			// 提前监听端口
			// 可以让启动错误返回给 Fx
			listener, err := net.Listen(
				"tcp",
				s.httpServer.Addr,
			)

			if err != nil {
				logger.Error.Error(
					"http server listen failed",
					"error",
					err,
				)

				return err
			}

			go func() {

				err := s.httpServer.Serve(listener)

				if err != nil &&
					err != http.ErrServerClosed {

					logger.Error.Error(
						"http server failed",
						"error",
						err,
					)
				}

			}()

			return nil
		},

		// Fx停止阶段
		OnStop: func(ctx context.Context) error {

			logger.App.Info(
				"关闭 http server",
			)

			err := s.httpServer.Shutdown(ctx)

			if err != nil {

				logger.Error.Error(
					"http server shutdown failed",
					"error",
					err,
				)

			}

			return err
		},
	})
	return s
}

// 获取路由
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// Close 立即关闭监听与所有活动连接。
//
// 应用退出时由 Wails 的 OnShutdown 先调用，保证 HTTP 请求先停止，
// 之后才关闭下载引擎等底层资源，避免退出过程中仍有请求访问已释放的资源。
func (s *Server) Close() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
}
