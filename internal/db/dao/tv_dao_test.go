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

// newTestTvDAO 创建一个内存 SQLite 的 TvDAO。
func newTestTvDAO(t *testing.T) *TvDAO {
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
CREATE TABLE tv_sources (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL,
    enabled       INTEGER NOT NULL DEFAULT 1,
    last_sync_at  DATETIME,
    last_error    TEXT NOT NULL DEFAULT '',
    etag          TEXT NOT NULL DEFAULT '',
    last_modified TEXT NOT NULL DEFAULT '',
    create_time   DATETIME DEFAULT CURRENT_TIMESTAMP,
    update_time   DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_tv_sources_url ON tv_sources(url);
CREATE TABLE tv_channels (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id   INTEGER NOT NULL,
    channel_key TEXT NOT NULL,
    tvg_id      TEXT NOT NULL DEFAULT '',
    tvg_name    TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    logo        TEXT NOT NULL DEFAULT '',
    group_title TEXT NOT NULL DEFAULT '',
    stream_url  TEXT NOT NULL,
    duration    INTEGER NOT NULL DEFAULT -1,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
    update_time DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_tv_channels_key ON tv_channels(channel_key);
`).Error
	if err != nil {
		t.Fatalf("创建 tv 表失败: %v", err)
	}

	return NewTvDAO(db)
}

func TestTvDAOReplaceChannels(t *testing.T) {
	d := newTestTvDAO(t)

	source := models.TVSource{Name: "测试源", URL: "https://example.com/list.m3u", Enabled: true}
	if err := d.db.Create(&source).Error; err != nil {
		t.Fatalf("写入订阅源失败: %v", err)
	}

	now := time.Now()
	channel := func(key, name string, order int) models.TVChannel {
		return models.TVChannel{
			SourceID:   source.ID,
			ChannelKey: key,
			Name:       name,
			StreamURL:  "https://example.com/" + key + ".m3u8",
			Duration:   -1,
			SortOrder:  order,
			CreateTime: &now,
			UpdateTime: &now,
		}
	}

	first := []models.TVChannel{
		channel("key-a", "频道 A", 0),
		channel("key-b", "频道 B", 1),
		channel("key-a", "频道 A", 2),
	}
	if err := d.ReplaceChannels(source.ID, first); err != nil {
		t.Fatalf("首次写入频道失败: %v", err)
	}

	list, err := d.ListChannels()
	if err != nil {
		t.Fatalf("查询频道失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("相同 channel_key 应去重，期望 2 条，实际 %d 条", len(list))
	}

	second := []models.TVChannel{
		channel("key-a", "频道 A 新名称", 0),
		channel("key-c", "频道 C", 1),
	}
	if err := d.ReplaceChannels(source.ID, second); err != nil {
		t.Fatalf("二次同步失败: %v", err)
	}

	list, err = d.ListChannels()
	if err != nil {
		t.Fatalf("查询频道失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("二次同步后期望 2 条，实际 %d 条", len(list))
	}

	var names []string
	for _, item := range list {
		names = append(names, item.Name)
	}
	if !contains(names, "频道 A 新名称") || !contains(names, "频道 C") {
		t.Fatalf("二次同步后频道名称不符合预期: %v", names)
	}

	counts, err := d.CountChannelsBySource()
	if err != nil {
		t.Fatalf("统计频道失败: %v", err)
	}
	if counts[source.ID] != 2 {
		t.Fatalf("统计频道数错误: %d", counts[source.ID])
	}
}

func TestTvDAOListEnabledSources(t *testing.T) {
	d := newTestTvDAO(t)

	sources := []models.TVSource{
		{Name: "启用", URL: "https://example.com/a.m3u", Enabled: true},
		{Name: "停用", URL: "https://example.com/b.m3u", Enabled: false},
	}
	if err := d.db.Create(&sources).Error; err != nil {
		t.Fatalf("写入订阅源失败: %v", err)
	}

	list, err := d.ListEnabledSources()
	if err != nil {
		t.Fatalf("查询订阅源失败: %v", err)
	}
	if len(list) != 1 || list[0].Name != "启用" {
		t.Fatalf("期望只返回启用的订阅源: %+v", list)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
