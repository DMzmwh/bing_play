package db

import (
	"changeme/internal/config"
	"changeme/internal/logger"
	"database/sql"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
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
		panic(err)
	}
	DB = gdb
	return gdb
}
