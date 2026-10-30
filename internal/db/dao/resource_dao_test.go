package dao

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bing/bing-play/internal/db/models"
	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
)

// newTestResourceDAO 创建一个内存 SQLite 的 ResourceDAO。
func newTestResourceDAO(t *testing.T) *ResourceDAO {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	err = db.Exec(`
CREATE TABLE resources (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    resource_name   TEXT,
    file_name       TEXT,
    source_url      TEXT,
    cover_url       TEXT,
    info_hash       TEXT,
    torrent_path    TEXT,
    total_size      INTEGER DEFAULT 0,
    local_path      TEXT,
    file_size       INTEGER DEFAULT 0,
    downloaded_size INTEGER DEFAULT 0,
    mime_type       TEXT,
    resource_type   TEXT,
    local_status    TEXT DEFAULT 'not_downloaded',
    download_status TEXT DEFAULT 'queued',
    priority        INTEGER DEFAULT 0,
    retry_count     INTEGER DEFAULT 0,
    mate_retry_count INTEGER DEFAULT 3,
    error_message   TEXT,
    total_duration  INTEGER DEFAULT 0,
    played          INTEGER DEFAULT 0,
    play_progress   INTEGER DEFAULT 0,
    play_count      INTEGER DEFAULT 0,
    last_play_time  DATETIME,
    finished        INTEGER DEFAULT 0,
    favorite        INTEGER DEFAULT 0,
    tags            TEXT,
    remark          TEXT,
    create_time     DATETIME,
    update_time     DATETIME,
    completed_time  DATETIME
);
CREATE UNIQUE INDEX idx_resources_info_hash ON resources(info_hash) WHERE info_hash IS NOT NULL;
`).Error
	if err != nil {
		t.Fatalf("创建 resources 表失败: %v", err)
	}

	return NewResourceDAO(db)
}

func TestSaveCrawledResourcesDedup(t *testing.T) {
	d := newTestResourceDAO(t)

	nameA, nameB := "资源A", "资源B"
	now := time.Now()
	list := []*models.Resource{
		{InfoHash: "hash-1", ResourceName: &nameA, CreateTime: &now, UpdateTime: &now},
		{InfoHash: "hash-2", ResourceName: &nameB, CreateTime: &now, UpdateTime: &now},
	}

	if err := d.SaveCrawledResources(list); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	// 同一批 InfoHash 再写一次，唯一索引冲突应被忽略而不是报错。
	if err := d.SaveCrawledResources(list); err != nil {
		t.Fatalf("重复写入失败: %v", err)
	}

	var count int64
	if err := d.db.Model(&models.Resource{}).Count(&count).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("resources 行数 = %d, want 2", count)
	}
}

func TestSaveCrawledResourcesEmpty(t *testing.T) {
	d := newTestResourceDAO(t)

	if err := d.SaveCrawledResources(nil); err != nil {
		t.Fatalf("空列表不应报错: %v", err)
	}
}
