package service

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/fx"
)

type ServiceOut struct {
	fx.Out

	Service application.Service `group:"wails-services"`
}

var Module = fx.Module(
	"service",

	fx.Provide(
		NewTorrentService,
		NewTorrentWailsService,

		NewGreetService,
		NewGreetWailsService,
	),
)
