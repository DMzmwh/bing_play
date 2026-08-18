package server

import (
	"changeme/internal/container"
	"changeme/internal/logger"
	"context"
	"net/http"

	"go.uber.org/fx"
)

type Server struct {
	httpServer *http.Server
	mux        *http.ServeMux
}

func NewServer(container *container.Container,
	lc fx.Lifecycle) *Server {
	mux := http.NewServeMux()

	httpServer := &Server{
		mux: mux,
		httpServer: &http.Server{
			Addr:    "127.0.0.1:9090",
			Handler: mux,
		},
	}
	httpServer.RegisterRoutes(mux, container)

	lc.Append(fx.Hook{

		OnStart: func(ctx context.Context) error {
			logger.App.Info("开启httpserver项目")
			httpServer.Start()
			return nil
		},

		OnStop: func(ctx context.Context) error {
			logger.App.Info("关闭httpserver项目")
			return httpServer.Shutdown(ctx)
		},
	})

	return httpServer
}

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

func (s *Server) Start() {
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error.Error(
				"http server failed",
				"error", err,
			)
		}
	}()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
