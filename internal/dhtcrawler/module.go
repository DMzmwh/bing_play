package dhtcrawler

import (
	"go.uber.org/fx"
)

var Module = fx.Module(
	"dhtcrawler",

	fx.Provide(
		NewService,
	),
)
