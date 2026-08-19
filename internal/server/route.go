package server

import (
	"net/http"

	"go.uber.org/fx"
)

type RouteRegistrar interface {
	RegisterRoutes(mux *http.ServeMux)
}

type RouteOut struct {
	fx.Out
	Handler RouteRegistrar `group:"routes"`
}

type RouteParams struct {
	fx.In
	Handlers []RouteRegistrar `group:"routes"`
}

type Route struct {
	handlers []RouteRegistrar
}

func NewRoute(params RouteParams) *Route {

	return &Route{
		handlers: params.Handlers,
	}
}

func (r *Route) Register(mux *http.ServeMux) {
	for _, handler := range r.handlers {
		handler.RegisterRoutes(mux)
	}
}
