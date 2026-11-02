package db

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/logger"

	"github.com/libtnb/sqlite"
	"go.uber.org/fx"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

var DB *gorm.DB

func InitDB(lc fx.Lifecycle, cfg *config.Config) *gorm.DB {
	/////////////////////////////////////////////////////
	// 原生 sql.DB
	/////////////////////////////////////////////////////

	logger.App.Info(cfg.DBPath + "-----------------")
	dbPath := cfg.DBPath
	dir := filepath.Dir(dbPath)
	err := os.MkdirAll(
		dir,
		0755,
	)
	if err != nil {
		panic(err)
	}

	sqlDB, err := sql.Open(
		"sqlite",
		dbPath,
	)
	if err != nil {
		panic(err)
	}
	/////////////////////////////////////////////////////
	// SQLite优化
	/////////////////////////////////////////////////////
	sqlDB.Exec(`PRAGMA journal_mode=WAL;`)
	sqlDB.Exec(`PRAGMA busy_timeout=5000;`)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	// 应用退出时关闭底层连接，让 SQLite 正常收尾（WAL 落盘、释放文件句柄）。
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			logger.App.Info("关闭数据库连接")
			if err := sqlDB.Close(); err != nil {
				logger.Error.Error("关闭数据库连接失败", slog.Any("err", err))
			}
			return nil
		},
	})
	/////////////////////////////////////////////////////
	// migration
	/////////////////////////////////////////////////////
	err = RunMigrate(sqlDB)
	if err != nil {
		logger.Error.Error("发生错误", slog.Any("err", err))
		panic(err)
	}
	/////////////////////////////////////////////////////
	// GORM
	/////////////////////////////////////////////////////
	gdb, err := gorm.Open(
		sqlite.Dialector{
			Conn: sqlDB,
		},
		&gorm.Config{},
	)
	if err != nil {
		// App 普通业务日志 Info/Debug/Warn
		//logger.App.Info("程序启动成功", slog.String("version", "1.0.0"))
		//logger.App.Warn("警告信息", slog.Int("code", 123))
		// Error 只输出错误级别
		logger.Error.Error("发生错误", slog.Any("err", err))
		panic(err)
	}
	DB = gdb

	// 历史本地视频记录的 local_path 存的是完整文件路径，统一为文件所在目录。
	if err := NormalizeLocalFilePaths(gdb); err != nil {
		logger.Error.Error("本地视频路径归一化失败", slog.Any("err", err))
	}

	return gdb
}
