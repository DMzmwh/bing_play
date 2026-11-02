package tv

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
		if err := db.Create(&models.TVChannel{
			SourceID:   source.ID,
			ChannelKey: "k1",
			Name:       "频道A",
			StreamURL:  "http://example.com/a.m3u8",
		}).Error; err != nil {
			t.Fatalf("写入频道失败: %v", err)
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

	t.Run("已同步过但本地无频道按最小间隔重试", func(t *testing.T) {
		s, _, db := newTestService(t)

		source := models.TVSource{Name: "测试源", URL: "https://example.com/a.m3u", Enabled: true}
		if err := db.Create(&source).Error; err != nil {
			t.Fatalf("写入订阅源失败: %v", err)
		}
		if err := db.Model(&source).Update("last_sync_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatalf("更新同步时间失败: %v", err)
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
		for key, id := range map[string]int64{"k1": recent.ID, "k2": older.ID} {
			if err := db.Create(&models.TVChannel{
				SourceID:   id,
				ChannelKey: key,
				Name:       "频道",
				StreamURL:  "http://example.com/" + key + ".m3u8",
			}).Error; err != nil {
				t.Fatalf("写入频道失败: %v", err)
			}
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

	// 已有频道数据，源才是真正“已同步”的状态。
	if err := db.Create(&models.TVChannel{
		SourceID:   source.ID,
		ChannelKey: "k1",
		Name:       "频道A",
		StreamURL:  "http://example.com/a.m3u8",
	}).Error; err != nil {
		t.Fatalf("写入频道失败: %v", err)
	}

	// client 为 nil：有频道且未到期的源必须被跳过，否则这里会触发请求。
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

// TestSyncRetriesNeverSyncedSource 校验从未成功同步过的订阅源首次失败后会继续重试，
// 而不是把失败时间写入 last_sync_at 后干等 6 小时。
func TestSyncRetriesNeverSyncedSource(t *testing.T) {
	s, _, db := newTestService(t)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:-1,频道A\nhttp://example.com/a.m3u8\n")
	}))
	defer server.Close()

	source := models.TVSource{Name: "IPTV 合集", URL: server.URL, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("写入订阅源失败: %v", err)
	}
	s.client = server.Client()

	// 第一次请求失败：从未成功同步过的源不能写入 last_sync_at。
	if _, err := s.SyncAll(context.Background()); err != nil {
		t.Fatalf("首次同步失败: %v", err)
	}
	var first models.TVSource
	if err := db.First(&first, source.ID).Error; err != nil {
		t.Fatalf("读取订阅源失败: %v", err)
	}
	if first.LastSyncAt != nil {
		t.Fatalf("首次失败后 last_sync_at = %v, want nil", first.LastSyncAt)
	}
	if first.LastError != ErrorCodeStatus {
		t.Fatalf("首次失败错误码 = %q, want %q", first.LastError, ErrorCodeStatus)
	}

	// 失败后应立即可再次同步（调度器按最小间隔重试），第二次成功。
	if _, err := s.SyncAll(context.Background()); err != nil {
		t.Fatalf("重试同步失败: %v", err)
	}
	var second models.TVSource
	if err := db.First(&second, source.ID).Error; err != nil {
		t.Fatalf("读取订阅源失败: %v", err)
	}
	if second.LastSyncAt == nil {
		t.Fatal("重试成功后 last_sync_at 仍为空")
	}
	if second.LastError != "" {
		t.Fatalf("重试成功后 last_error = %q, want 空", second.LastError)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("请求次数 = %d, want 2", got)
	}
	if delay := s.nextSyncDelay(); delay < 5*time.Hour {
		t.Fatalf("首次成功后的刷新间隔 = %v, want 约 6 小时", delay)
	}
}

// TestSyncRefreshRetriesLimited 校验已成功同步过的订阅源定时刷新失败后，
// 会按 minSyncDelay 重试 maxRefreshRetries 次，仍未成功才恢复 6 小时刷新。
func TestSyncRefreshRetriesLimited(t *testing.T) {
	s, _, db := newTestService(t)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	syncedAt := time.Now().Add(-7 * time.Hour).Truncate(time.Second)
	source := models.TVSource{Name: "测试源", URL: server.URL, Enabled: true, LastSyncAt: &syncedAt}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("写入订阅源失败: %v", err)
	}
	// 已有频道数据，用于验证刷新失败时仍保留旧频道并恢复 6 小时节奏。
	if err := db.Create(&models.TVChannel{
		SourceID:   source.ID,
		ChannelKey: "k1",
		Name:       "频道A",
		StreamURL:  "http://example.com/a.m3u8",
	}).Error; err != nil {
		t.Fatalf("写入频道失败: %v", err)
	}
	s.client = server.Client()

	// 首次刷新失败：保留旧的 last_sync_at，让调度器按最小间隔重试。
	if _, err := s.SyncAll(context.Background()); err != nil {
		t.Fatalf("首次刷新失败: %v", err)
	}
	var afterFirst models.TVSource
	if err := db.First(&afterFirst, source.ID).Error; err != nil {
		t.Fatalf("读取订阅源失败: %v", err)
	}
	if afterFirst.LastSyncAt == nil || !afterFirst.LastSyncAt.Equal(syncedAt) {
		t.Fatalf("刷新失败后 last_sync_at = %v, want 保持不变 %v", afterFirst.LastSyncAt, syncedAt)
	}

	// 剩余 maxRefreshRetries 次重试全部失败后，才更新同步时间并恢复 6 小时节奏。
	for i := 0; i < maxRefreshRetries; i++ {
		if _, err := s.SyncAll(context.Background()); err != nil {
			t.Fatalf("第 %d 次重试失败: %v", i+1, err)
		}
	}
	if got := calls.Load(); got != int32(1+maxRefreshRetries) {
		t.Fatalf("请求次数 = %d, want %d", got, 1+maxRefreshRetries)
	}

	var final models.TVSource
	if err := db.First(&final, source.ID).Error; err != nil {
		t.Fatalf("读取订阅源失败: %v", err)
	}
	if final.LastSyncAt == nil || !final.LastSyncAt.After(syncedAt) {
		t.Fatalf("重试耗尽后 last_sync_at = %v, want 晚于 %v", final.LastSyncAt, syncedAt)
	}
	if delay := s.nextSyncDelay(); delay < 5*time.Hour {
		t.Fatalf("重试耗尽后的刷新间隔 = %v, want 约 6 小时", delay)
	}
}

// TestSyncAllFetchesWhenNoChannels 校验本地没有频道数据时，即使未到刷新时间也会强制全量拉取，
// 且不发送条件请求头，避免服务端返回 304 导致频道一直为空。
func TestSyncAllFetchesWhenNoChannels(t *testing.T) {
	s, _, db := newTestService(t)

	var sawConditional atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			sawConditional.Store(true)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:-1,频道A\nhttp://example.com/a.m3u8\n")
	}))
	defer server.Close()

	syncedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	source := models.TVSource{
		Name:         "IPTV 合集",
		URL:          server.URL,
		Enabled:      true,
		LastSyncAt:   &syncedAt,
		ETag:         `W/"etag-1"`,
		LastModified: "Mon, 02 Nov 2026 12:00:00 GMT",
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("写入订阅源失败: %v", err)
	}
	s.client = server.Client()

	// 本地没有频道时，调度器按最小间隔补拉。
	if delay := s.nextSyncDelay(); delay != minSyncDelay {
		t.Fatalf("等待时间 = %v, want %v", delay, minSyncDelay)
	}

	statuses, err := s.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("同步失败: %v", err)
	}
	if sawConditional.Load() {
		t.Fatal("本地没有频道时不应发送条件请求头")
	}
	if len(statuses) != 1 || statuses[0].ChannelCount != 1 {
		t.Fatalf("同步后状态 = %+v, want 1 个频道", statuses)
	}

	var got models.TVSource
	if err := db.First(&got, source.ID).Error; err != nil {
		t.Fatalf("读取订阅源失败: %v", err)
	}
	if got.LastSyncAt == nil || !got.LastSyncAt.After(syncedAt) {
		t.Fatalf("同步时间 = %v, want 晚于 %v", got.LastSyncAt, syncedAt)
	}
}
