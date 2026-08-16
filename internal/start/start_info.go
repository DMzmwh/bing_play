package start

import (
	"changeme/internal/config"
	"changeme/internal/container"
	db2 "changeme/internal/db"
	"changeme/internal/i18n"
	"changeme/internal/logger"
	"changeme/internal/server"
	"context"
	"log/slog"

	"gorm.io/gorm"
)

type StartupApp struct {
	db         *gorm.DB
	httpServer *server.Server
}

func NewStartup(cfg *config.Config) *StartupApp {
	i18n, err := i18n.NewI18nService(
		"en",
	)
	if err != nil {
		logger.Error.Error(
			"i18n init error",
			slog.Any("error", err),
		)
		panic(err)
	}

	msg := i18n.T(
		"hello",
		map[string]interface{}{
			"Name": "张三",
		},
	)
	logger.App.Info(msg)

	// 初始化数据库
	db := db2.InitDB(cfg)
	// 初始化数据库 torrent
	//初始化容器
	cn := container.NewContainer(db, cfg, i18n)
	//初始化http服务
	httpServer := server.NewServer(cn)
	httpServer.Start()
	return &StartupApp{
		db:         db,
		httpServer: httpServer,
	}
}

func (app *StartupApp) Shutdown(ctx context.Context) {
	httpErr := app.httpServer.Shutdown(ctx)
	if httpErr != nil {
		logger.Error.Error(
			"http server stop error",
			"error", httpErr,
		)
	}
}
