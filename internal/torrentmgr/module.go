package torrentmgr

import (
	"go.uber.org/fx"
)

var Module = fx.Module(
	"torrentmgr",

	fx.Provide(
		NewManager,
	),
)
