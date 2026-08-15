package server

import (
	"changeme/internal/container"
	"changeme/internal/server/handler"
	"net/http"
)

func (s *Server) RegisterRoutes(mux *http.ServeMux, container *container.Container) {
	videoHandler := handler.NewVideoHandler(container)
	mux.HandleFunc("/video/{id}", videoHandler.VideoLocal)
	mux.HandleFunc("/stream/{id}", videoHandler.Stream)
}
