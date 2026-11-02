package tv

import "go.uber.org/fx"

var Module = fx.Module(
	"tv",

	fx.Provide(
		NewService,
	),
)
