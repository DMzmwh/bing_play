package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bing/bing-play/internal/db/models"
	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
)

// TestNormalizeLocalResourcePaths 校验历史本地视频的 local_path 从完整文件路径
// 归一化为文件所在目录，且对已是目录的记录保持幂等。
func TestNormalizeLocalResourcePaths(t *testing.T) {
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
CREATE TABLE resources (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       INTEGER DEFAULT 0,
    local_path TEXT,
    file_name  TEXT
);
`).Error
	if err != nil {
		t.Fatalf("创建 resources 表失败: %v", err)
	}

	dir := t.TempDir()
	fileName := "Movie.mp4"
	filePath := filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("写入视频文件失败: %v", err)
	}

	insert := func(kind models.Kind, localPath, name string) {
		t.Helper()
		if err := gdb.Exec(
			"INSERT INTO resources (kind, local_path, file_name) VALUES (?, ?, ?)",
			int8(kind), localPath, name,
		).Error; err != nil {
			t.Fatalf("插入记录失败: %v", err)
		}
	}

	// 旧记录：local_path 存的是完整文件路径
	insert(models.KindLocal, filePath, fileName)
	// 新记录：local_path 已经是目录
	insert(models.KindLocal, dir, fileName)
	// 文件名与路径末段不一致：保守起见不处理
	insert(models.KindLocal, filePath, "other.mp4")

	if err := NormalizeLocalResourcePaths(gdb); err != nil {
		t.Fatalf("归一化失败: %v", err)
	}

	var rows []models.Resource
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
