package server

import (
	"changeme/internal/container"
	"changeme/internal/logger"
	"context"
	"net/http"
)

type Server struct {
	httpServer *http.Server
	mux        *http.ServeMux
}

func NewServer(container *container.Container) *Server {
	mux := http.NewServeMux()

	httpServer := &Server{
		mux: mux,
		httpServer: &http.Server{
			Addr:    "127.0.0.1:9090",
			Handler: mux,
		},
	}
	httpServer.RegisterRoutes(mux, container)
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
