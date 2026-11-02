package tv

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
)

// newTestService 创建使用内存 SQLite 的 Service，不启动同步任务。
func newTestService(t *testing.T) (*Service, *dao.TvDAO, *gorm.DB) {
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

	tvDAO := dao.NewTvDAO(db)
	return &Service{dao: tvDAO}, tvDAO, db
}

// TestNextSyncDelay 校验下一次请求时间基于各源上次同步时间计算。
func TestNextSyncDelay(t *testing.T) {
	t.Run("未满 6 小时只等待剩余时间", func(t *testing.T) {
		s, _, db := newTestService(t)

		source := models.TVSource{Name: "测试源", URL: "https://example.com/a.m3u", Enabled: true}
		if err := db.Create(&source).Error; err != nil {
			t.Fatalf("写入订阅源失败: %v", err)
		}
		syncedAt := time.Now().Add(-time.Hour)
		if err := db.Model(&source).Update("last_sync_at", syncedAt).Error; err != nil {
			t.Fatalf("更新同步时间失败: %v", err)
		}

		delay := s.nextSyncDelay()
		if delay < 4*time.Hour+50*time.Minute || delay > 5*time.Hour+10*time.Minute {
			t.Fatalf("剩余等待时间 = %v, want 约 5 小时", delay)
		}
	})

	t.Run("从未同步过按最小间隔重试", func(t *testing.T) {
		s, _, db := newTestService(t)

		source := models.TVSource{Name: "测试源", URL: "https://example.com/a.m3u", Enabled: true}
		if err := db.Create(&source).Error; err != nil {
			t.Fatalf("写入订阅源失败: %v", err)
		}

		if delay := s.nextSyncDelay(); delay != minSyncDelay {
			t.Fatalf("等待时间 = %v, want %v", delay, minSyncDelay)
		}
	})

	t.Run("多个源取最早到期时间", func(t *testing.T) {
		s, _, db := newTestService(t)

		recent := models.TVSource{Name: "源A", URL: "https://example.com/a.m3u", Enabled: true}
		older := models.TVSource{Name: "源B", URL: "https://example.com/b.m3u", Enabled: true}
		if err := db.Create(&recent).Error; err != nil {
			t.Fatalf("写入订阅源 A 失败: %v", err)
		}
		if err := db.Create(&older).Error; err != nil {
			t.Fatalf("写入订阅源 B 失败: %v", err)
		}
		if err := db.Model(&recent).Update("last_sync_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatalf("更新源 A 同步时间失败: %v", err)
		}
		if err := db.Model(&older).Update("last_sync_at", time.Now().Add(-5*time.Hour-30*time.Minute)).Error; err != nil {
			t.Fatalf("更新源 B 同步时间失败: %v", err)
		}

		delay := s.nextSyncDelay()
		if delay < 25*time.Minute || delay > 35*time.Minute {
			t.Fatalf("等待时间 = %v, want 约 30 分钟", delay)
		}
	})
}

// TestSyncAllSkipsRecent 校验重启后未满 6 小时的订阅源不会重新请求。
func TestSyncAllSkipsRecent(t *testing.T) {
	s, _, db := newTestService(t)

	source := models.TVSource{Name: "测试源", URL: "https://example.com/a.m3u", Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("写入订阅源失败: %v", err)
	}

	syncedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := db.Model(&source).Update("last_sync_at", syncedAt).Error; err != nil {
		t.Fatalf("更新同步时间失败: %v", err)
	}

	// client 为 nil：未到期的源必须被跳过，否则这里会触发请求。
	if _, err := s.SyncAll(context.Background()); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	var got time.Time
	if err := db.Raw("SELECT last_sync_at FROM tv_sources WHERE id = ?", source.ID).Scan(&got).Error; err != nil {
		t.Fatalf("读取同步时间失败: %v", err)
	}
	if !got.Equal(syncedAt) {
		t.Fatalf("同步时间 = %v, want 保持 %v", got, syncedAt)
	}
}

func TestSnapshotCache(t *testing.T) {
	s, d, db := newTestService(t)

	source := models.TVSource{Name: "测试源", URL: "https://example.com/list.m3u", Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("写入订阅源失败: %v", err)
	}

	now := time.Now()
	build := func(key, name, logo string) models.TVChannel {
		return models.TVChannel{
			SourceID:   source.ID,
			ChannelKey: key,
			Name:       name,
			Logo:       logo,
			GroupTitle: "综合",
			StreamURL:  "https://example.com/" + key + ".m3u8",
			Duration:   -1,
			CreateTime: &now,
			UpdateTime: &now,
		}
	}

	if err := d.ReplaceChannels(source.ID, []models.TVChannel{
		build("key-a", "频道A", "https://example.com/a.png"),
		build("key-b", "频道B", ""),
	}); err != nil {
		t.Fatalf("写入频道失败: %v", err)
	}

	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if len(snapshot.Channels) != 2 || len(snapshot.Sources) != 1 {
		t.Fatalf("快照长度 = %d/%d, want 2/1", len(snapshot.Channels), len(snapshot.Sources))
	}
	if snapshot.Sources[0].ChannelCount != 2 {
		t.Fatalf("频道计数 = %d, want 2", snapshot.Sources[0].ChannelCount)
	}
	// 台标字段必须保留
	if snapshot.Channels[0].Logo == "" {
		t.Fatalf("快照中的台标丢失: %+v", snapshot.Channels[0])
	}

	// 直接改库后，未刷新前快照仍沿用旧数据（说明读取没有查库）。
	if err := d.ReplaceChannels(source.ID, []models.TVChannel{
		build("key-a", "频道A", "https://example.com/a.png"),
		build("key-b", "频道B", ""),
		build("key-c", "频道C", ""),
	}); err != nil {
		t.Fatalf("二次写入频道失败: %v", err)
	}
	cached, err := s.Snapshot()
	if err != nil {
		t.Fatalf("读取缓存快照失败: %v", err)
	}
	if len(cached.Channels) != 2 {
		t.Fatalf("未刷新前快照长度 = %d, want 2", len(cached.Channels))
	}

	// 同步完成后刷新快照应读到最新数据。
	s.refreshSnapshot()
	updated, err := s.Snapshot()
	if err != nil {
		t.Fatalf("读取刷新后的快照失败: %v", err)
	}
	if len(updated.Channels) != 3 {
		t.Fatalf("刷新后快照长度 = %d, want 3", len(updated.Channels))
	}
	if updated.Sources[0].ChannelCount != 3 {
		t.Fatalf("刷新后频道计数 = %d, want 3", updated.Sources[0].ChannelCount)
	}
}
