package db

import (
	"database/sql"

	"github.com/golang-migrate/migrate/v4"
	sqliteMigrate "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrate 执行数据库迁移。
//
// 注意：golang-migrate 的 sqlite 驱动 Close() 会关闭传入的 *sql.DB，
// 而该连接随后还要交给 GORM 使用，因此这里不能调用 m.Close()；
// 连接统一由 InitDB 注册的 OnStop 释放。迁移实例本身不持有其它句柄。
func RunMigrate(db *sql.DB) error {
	driver, err := sqliteMigrate.WithInstance(db, &sqliteMigrate.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithDatabaseInstance("file://resources/migrations", "sqlite", driver)
	if err != nil {
		return err
	}
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}
