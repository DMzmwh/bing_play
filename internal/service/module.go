package server

import (
	"changeme/internal/server/handler"

	"go.uber.org/fx"
)

var Module = fx.Module(
	"server",

	fx.Provide(
		NewServer,
		handler.NewVideoHandler,
	),
)
