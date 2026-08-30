package server

import (
	"context"
	"fmt"
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
	route *Route,
	lc fx.Lifecycle,
) *Server {

	mux := http.NewServeMux()

	s := &Server{
		mux: mux,
		httpServer: &http.Server{
			Addr:    "127.0.0.1:9090",
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
