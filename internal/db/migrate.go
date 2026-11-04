package db

import (
	"database/sql"
	"os"
	"path/filepath"

	"github.com/bing/bing-play/internal/assets"
	"github.com/bing/bing-play/internal/db/models"

	"github.com/golang-migrate/migrate/v4"
	sqliteMigrate "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"
)

// RunMigrate 执行数据库迁移。
//
// 迁移文件随程序内置，保证自动更新只替换 exe 时也能拿到最新迁移；
// 不读取运行目录下的 migrations，避免外部文件与程序版本不匹配。
//
// 注意：golang-migrate 的 sqlite 驱动 Close() 会关闭传入的 *sql.DB，
// 而该连接随后还要交给 GORM 使用，因此这里不能调用 m.Close()；
// 连接统一由 InitDB 注册的 OnStop 释放。迁移实例本身不持有其它句柄。
func RunMigrate(db *sql.DB, resFS assets.FS) error {
	driver, err := sqliteMigrate.WithInstance(db, &sqliteMigrate.Config{})
	if err != nil {
		return err
	}

	source, err := iofs.New(resFS, assets.MigrationsDir)
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		return err
	}
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}

// NormalizeLocalFilePaths 把历史本地视频记录的 local_path 统一为文件所在目录。
//
// 旧实现把完整文件路径写入 local_path，与种子记录（下载目录）语义不一致。
// 旧记录的 file_name 恰好是 local_path 的最后一段，据此可安全地改为父目录。
// 该操作幂等，可重复执行。
func NormalizeLocalFilePaths(db *gorm.DB) error {
	var list []models.LocalFileRecord
	err := db.
		Select("id, local_path, file_name").
		Where("(info_hash IS NULL OR info_hash = '') AND local_path <> ''").
		Find(&list).
		Error
	if err != nil {
		return err
	}

	for i := range list {
		local := &list[i]
		if local.FileName == nil || *local.FileName == "" {
			continue
		}
		// local_path 已经是目录时无需处理，规则天然幂等。
		if info, statErr := os.Stat(local.LocalPath); statErr == nil && info.IsDir() {
			continue
		}
		if filepath.Base(local.LocalPath) != *local.FileName {
			continue
		}
		dir := filepath.Dir(local.LocalPath)
		if dir == local.LocalPath {
			continue
		}
		if err := db.Model(&models.LocalFileRecord{}).
			Where("id = ?", local.ID).
			Update("local_path", dir).
			Error; err != nil {
			return err
		}
	}

	return nil
}
