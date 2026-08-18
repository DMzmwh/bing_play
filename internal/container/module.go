package container

import (
	"go.uber.org/fx"
)

var Module = fx.Module(
	"container",

	fx.Provide(
		NewContainer,
	),
)
