package i18n

import (
	"go.uber.org/fx"
)

var Module = fx.Module(
	"i18n",

	fx.Provide(
		NewI18nService,
	),
)
