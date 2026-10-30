package db

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/logger"

	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

var DB *gorm.DB

func InitDB(cfg *config.Config) *gorm.DB {
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
	return gdb
}
