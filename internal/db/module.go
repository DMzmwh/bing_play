package db

import (
	"github.com/bing/bing-play/internal/db/dao"

	"go.uber.org/fx"
)

var Module = fx.Module(
	"db",

	fx.Provide(
		InitDB,
		dao.NewResourceDAO,
	),
)
