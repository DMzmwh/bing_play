package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bing/bing-play/internal/db/models"
	"github.com/golang-migrate/migrate/v4"
	sqliteMigrate "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

// TestNormalizeLocalFilePaths 校验历史本地视频的 local_path 从完整文件路径
// 归一化为文件所在目录，且对已是目录的记录保持幂等。
func TestNormalizeLocalFilePaths(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	err = gdb.Exec(`
CREATE TABLE local_files (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    info_hash  TEXT,
    local_path TEXT,
    file_name  TEXT
);
`).Error
	if err != nil {
		t.Fatalf("创建 local_files 表失败: %v", err)
	}

	dir := t.TempDir()
	fileName := "Movie.mp4"
	filePath := filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("写入视频文件失败: %v", err)
	}

	insert := func(localPath, name string) {
		t.Helper()
		if err := gdb.Exec(
			"INSERT INTO local_files (info_hash, local_path, file_name) VALUES (NULL, ?, ?)",
			localPath, name,
		).Error; err != nil {
			t.Fatalf("插入记录失败: %v", err)
		}
	}

	// 旧记录：local_path 存的是完整文件路径
	insert(filePath, fileName)
	// 新记录：local_path 已经是目录
	insert(dir, fileName)
	// 文件名与路径末段不一致：保守起见不处理
	insert(filePath, "other.mp4")

	if err := NormalizeLocalFilePaths(gdb); err != nil {
		t.Fatalf("归一化失败: %v", err)
	}

	var rows []models.LocalFileRecord
	if err := gdb.Select("id, local_path").Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("查询结果失败: %v", err)
	}
	want := []string{dir, dir, filePath}
	if len(rows) != len(want) {
		t.Fatalf("记录数 = %d, want %d", len(rows), len(want))
	}
	for i := range want {
		if rows[i].LocalPath != want[i] {
			t.Errorf("第 %d 条 local_path = %q, want %q", i+1, rows[i].LocalPath, want[i])
		}
	}
}

// TestBaselineMigration 校验发布基线迁移能在空库上创建全部运行期表、
// 写入内置 IPTV 订阅源，并且可以完整回滚。
func TestBaselineMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	driver, err := sqliteMigrate.WithInstance(sqlDB, &sqliteMigrate.Config{})
	if err != nil {
		t.Fatalf("初始化迁移驱动失败: %v", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://../../resources/migrations", "sqlite", driver)
	if err != nil {
		t.Fatalf("加载迁移文件失败: %v", err)
	}

	if err := m.Up(); err != nil {
		t.Fatalf("执行基线迁移失败: %v", err)
	}

	countTable := func(name string) int64 {
		t.Helper()
		var n int64
		if err := sqlDB.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name,
		).Scan(&n); err != nil {
			t.Fatalf("查询表 %s 失败: %v", name, err)
		}
		return n
	}

	tables := []string{"resources", "download_tasks", "local_files", "app_settings", "tv_sources", "tv_channels"}
	for _, name := range tables {
		if got := countTable(name); got != 1 {
			t.Fatalf("表 %s 不存在", name)
		}
	}
	if got := countTable("resources_legacy"); got != 0 {
		t.Fatal("基线迁移不应创建 resources_legacy")
	}

	var (
		name    string
		url     string
		enabled int
	)
	if err := sqlDB.QueryRow(`SELECT name, url, enabled FROM tv_sources`).Scan(&name, &url, &enabled); err != nil {
		t.Fatalf("读取内置订阅源失败: %v", err)
	}
	if name != "IPTV 合集" || enabled != 1 || !strings.Contains(url, "iptv_collection.m3u") {
		t.Fatalf("内置订阅源 = %q %q enabled=%d", name, url, enabled)
	}

	if err := m.Down(); err != nil {
		t.Fatalf("回滚全部迁移失败: %v", err)
	}
	for _, name := range tables {
		if got := countTable(name); got != 0 {
			t.Fatalf("回滚后表 %s 仍存在", name)
		}
	}

	if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
		t.Fatalf("关闭迁移实例失败: source=%v db=%v", sourceErr, dbErr)
	}
}
