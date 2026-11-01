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
    kind            INTEGER DEFAULT 0,
    task_state      INTEGER DEFAULT 0,
    local_state     INTEGER DEFAULT 0,
    local_loss_reason INTEGER DEFAULT 0,
    state_version   INTEGER DEFAULT 0,
    attempt         INTEGER DEFAULT 0,
    max_attempts    INTEGER DEFAULT 3,
    error_code      TEXT DEFAULT '',
    next_retry_at   DATETIME,
    state_changed_at DATETIME,
    verified_at     DATETIME,
    local_checked_at DATETIME,
    priority        INTEGER DEFAULT 0,
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
    update_time     DATETIME
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

func TestListCrawled(t *testing.T) {
	d := newTestResourceDAO(t)

	base := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	sizes := map[string]int64{"hash-1": 100, "hash-2": 300, "hash-3": 200}
	names := map[string]string{"hash-1": "资源一", "hash-2": "资源二", "hash-3": "资源三"}

	list := make([]*models.Resource, 0, len(sizes))
	for hash, size := range sizes {
		created := base.Add(time.Duration(sizes[hash]) * time.Minute)
		name, totalSize := names[hash], size
		list = append(list, &models.Resource{
			InfoHash:     hash,
			ResourceName: &name,
			TotalSize:    &totalSize,
			CreateTime:   &created,
			UpdateTime:   &created,
		})
	}
	if err := d.SaveCrawledResources(list); err != nil {
		t.Fatalf("写入爬取资源失败: %v", err)
	}

	// 本地视频没有 InfoHash，不应出现在爬取资源列表里。
	localName := "本地视频"
	localTime := base
	if err := d.SaveLocalResource(&models.Resource{
		ResourceName: &localName,
		CreateTime:   &localTime,
		UpdateTime:   &localTime,
	}); err != nil {
		t.Fatalf("写入本地资源失败: %v", err)
	}

	all, total, err := d.ListCrawled(CrawledListQuery{SortBy: "create_time", SortOrder: "asc", Limit: 20})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("total = %d, len = %d, want 3", total, len(all))
	}

	bySize, total, err := d.ListCrawled(CrawledListQuery{SortBy: "total_size", SortOrder: "desc", Limit: 20})
	if err != nil {
		t.Fatalf("按体积排序失败: %v", err)
	}
	if total != 3 || bySize[0].InfoHash != "hash-2" {
		t.Fatalf("按体积倒序首条 = %q, want hash-2", bySize[0].InfoHash)
	}

	filtered, total, err := d.ListCrawled(CrawledListQuery{Keyword: "hash-2", Limit: 20})
	if err != nil {
		t.Fatalf("关键词查询失败: %v", err)
	}
	if total != 1 || len(filtered) != 1 || filtered[0].InfoHash != "hash-2" {
		t.Fatalf("关键词查询结果 = %v, total = %d", filtered, total)
	}

	page, total, err := d.ListCrawled(CrawledListQuery{SortBy: "create_time", SortOrder: "desc", Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("分页查询失败: %v", err)
	}
	if total != 3 || len(page) != 1 {
		t.Fatalf("分页 total = %d, len = %d, want 3/1", total, len(page))
	}
}

func TestGetByInfoHashAndIDs(t *testing.T) {
	d := newTestResourceDAO(t)

	now := time.Now()
	nameA, nameB := "资源A", "资源B"
	if err := d.Save(&models.Resource{
		InfoHash:     "hash-1",
		ResourceName: &nameA,
		CreateTime:   &now,
		UpdateTime:   &now,
	}); err != nil {
		t.Fatalf("写入 hash-1 失败: %v", err)
	}
	if err := d.Save(&models.Resource{
		InfoHash:     "hash-2",
		ResourceName: &nameB,
		CreateTime:   &now,
		UpdateTime:   &now,
	}); err != nil {
		t.Fatalf("写入 hash-2 失败: %v", err)
	}

	got, err := d.GetByInfoHash("hash-1")
	if err != nil {
		t.Fatalf("按 InfoHash 查询失败: %v", err)
	}
	if got == nil || got.InfoHash != "hash-1" {
		t.Fatalf("按 InfoHash 查询结果 = %v", got)
	}

	missing, err := d.GetByInfoHash("hash-missing")
	if err != nil {
		t.Fatalf("查询不存在的 InfoHash 不应报错: %v", err)
	}
	if missing != nil {
		t.Fatalf("不存在的 InfoHash 应返回 nil, got %v", missing)
	}

	empty, err := d.GetByInfoHash("")
	if err != nil || empty != nil {
		t.Fatalf("空 InfoHash 应返回 nil, got %v, err %v", empty, err)
	}

	list, err := d.GetByIDs([]int64{got.ID})
	if err != nil {
		t.Fatalf("按 ID 查询失败: %v", err)
	}
	if len(list) != 1 || list[0].InfoHash != "hash-1" {
		t.Fatalf("按 ID 查询结果 = %v", list)
	}

	none, err := d.GetByIDs(nil)
	if err != nil || none != nil {
		t.Fatalf("空 ID 列表应返回 nil, got %v, err %v", none, err)
	}
}

func TestListPlayable(t *testing.T) {
	d := newTestResourceDAO(t)

	now := time.Now()
	fileName := func(s string) *string { return &s }
	localPath := `D:\downloads`

	mk := func(hash string, kind models.Kind, task models.TaskState, local models.LocalState, file *string) *models.Resource {
		name := hash
		return &models.Resource{
			InfoHash:     hash,
			ResourceName: &name,
			FileName:     file,
			LocalPath:    localPath,
			Kind:         kind,
			TaskState:    task,
			LocalState:   local,
			CreateTime:   &now,
			UpdateTime:   &now,
		}
	}

	cases := []struct {
		resource *models.Resource
		want     bool
	}{
		// 本地视频：可用
		{mk("", models.KindLocal, models.TaskCompleted, models.LocalPresent, fileName("local.mp4")), true},
		// 下载中 / 校验中的视频：支持边下边播
		{mk("hash-downloading", models.KindTorrent, models.TaskDownloading, models.LocalPartial, fileName("downloading.mkv")), true},
		{mk("hash-verifying", models.KindTorrent, models.TaskVerifying, models.LocalPartial, fileName("verifying.mp4")), true},
		// 已完成且本地齐备
		{mk("hash-completed", models.KindTorrent, models.TaskCompleted, models.LocalPresent, fileName("completed.mp4")), true},
		// 已完成但文件丢失 / 损坏不可播放
		{mk("hash-lost", models.KindTorrent, models.TaskCompleted, models.LocalMissing, fileName("lost.mp4")), false},
		{mk("hash-corrupted", models.KindTorrent, models.TaskCompleted, models.LocalCorrupted, fileName("corrupted.mp4")), false},
		// 暂停、排队中的任务没有活跃下载上下文
		{mk("hash-paused", models.KindTorrent, models.TaskPaused, models.LocalPartial, fileName("paused.mp4")), false},
		{mk("hash-queued", models.KindTorrent, models.TaskQueued, models.LocalNone, fileName("queued.mp4")), false},
		// 非视频种子不进入播放列表
		{mk("hash-archive", models.KindTorrent, models.TaskDownloading, models.LocalPartial, fileName("archive.zip")), false},
		{mk("hash-no-file", models.KindTorrent, models.TaskDownloading, models.LocalPartial, nil), false},
	}

	wantCount := 0
	for _, c := range cases {
		if err := d.Save(c.resource); err != nil {
			t.Fatalf("写入资源 %q 失败: %v", c.resource.InfoHash, err)
		}
		if c.want {
			wantCount++
		}
	}

	playable, err := d.ListPlayable()
	if err != nil {
		t.Fatalf("查询播放列表失败: %v", err)
	}
	if len(playable) != wantCount {
		t.Fatalf("播放列表数量 = %d, want %d (%v)", len(playable), wantCount, playable)
	}

	got := make(map[string]bool, len(playable))
	for _, item := range playable {
		key := item.InfoHash
		if item.Kind == models.KindLocal {
			key = "local"
		}
		got[key] = true
	}
	for _, c := range cases {
		key := c.resource.InfoHash
		if c.resource.Kind == models.KindLocal {
			key = "local"
		}
		if c.want && !got[key] {
			t.Errorf("应出现在播放列表的资源 %q 缺失", key)
		}
		if !c.want && got[key] {
			t.Errorf("不应出现在播放列表的资源 %q 出现了", key)
		}
	}
}

func TestListDownloadTasks(t *testing.T) {
	d := newTestResourceDAO(t)

	now := time.Now()
	mk := func(hash string, state models.TaskState, minute int) *models.Resource {
		name := hash
		created := now.Add(time.Duration(minute) * time.Minute)
		return &models.Resource{
			InfoHash:     hash,
			ResourceName: &name,
			Kind:         models.KindTorrent,
			TaskState:    state,
			LocalState:   models.LocalNone,
			CreateTime:   &created,
			UpdateTime:   &created,
		}
	}

	// 爬虫原始记录（Discovered）与本地视频不应出现在下载任务列表。
	if err := d.Save(mk("hash-raw", models.TaskDiscovered, 0)); err != nil {
		t.Fatalf("写入原始记录失败: %v", err)
	}
	localName := "本地视频"
	if err := d.SaveLocalResource(&models.Resource{
		ResourceName: &localName,
		Kind:         models.KindLocal,
		TaskState:    models.TaskCompleted,
		LocalState:   models.LocalPresent,
		CreateTime:   &now,
		UpdateTime:   &now,
	}); err != nil {
		t.Fatalf("写入本地视频失败: %v", err)
	}

	// 下载中、已完成、已取消三条任务应全部返回。
	states := []models.TaskState{
		models.TaskDownloading,
		models.TaskCompleted,
		models.TaskCancelled,
	}
	for i, state := range states {
		hash := "hash-task-" + string(rune('a'+i))
		if err := d.Save(mk(hash, state, i+1)); err != nil {
			t.Fatalf("写入下载任务失败: %v", err)
		}
	}

	tasks, err := d.ListDownloadTasks()
	if err != nil {
		t.Fatalf("查询下载任务失败: %v", err)
	}
	if len(tasks) != len(states) {
		t.Fatalf("下载任务数 = %d, want %d (%v)", len(tasks), len(states), tasks)
	}

	byState, err := d.ListByTaskStates([]models.TaskState{models.TaskDownloading})
	if err != nil {
		t.Fatalf("按状态查询失败: %v", err)
	}
	if len(byState) != 1 || byState[0].InfoHash != "hash-task-a" {
		t.Fatalf("按状态查询结果 = %v", byState)
	}

	none, err := d.ListByTaskStates(nil)
	if err != nil || none != nil {
		t.Fatalf("空状态列表应返回 nil, got %v, err %v", none, err)
	}

	completed, err := d.ListCompletedTasks()
	if err != nil {
		t.Fatalf("查询已完成任务失败: %v", err)
	}
	if len(completed) != 1 || completed[0].InfoHash != "hash-task-b" {
		t.Fatalf("已完成任务结果 = %v", completed)
	}
}

// TestListPendingTasks 待下载列表以数据库为事实源，必须包含所有未完成状态，
// 排除仅爬虫发现的记录、已完成任务与本地视频。
func TestListPendingTasks(t *testing.T) {
	d := newTestResourceDAO(t)

	now := time.Now()
	mk := func(hash string, state models.TaskState) *models.Resource {
		name := hash
		return &models.Resource{
			InfoHash:     hash,
			ResourceName: &name,
			Kind:         models.KindTorrent,
			TaskState:    state,
			LocalState:   models.LocalNone,
			CreateTime:   &now,
			UpdateTime:   &now,
		}
	}

	pendingStates := []models.TaskState{
		models.TaskPreparing, models.TaskQueued, models.TaskDownloading,
		models.TaskPaused, models.TaskVerifying, models.TaskRetryWait,
		models.TaskFailed, models.TaskCancelled,
	}
	for i, state := range pendingStates {
		if err := d.Save(mk("hash-pending-"+string(rune('a'+i)), state)); err != nil {
			t.Fatalf("写入待下载任务 %v 失败: %v", state, err)
		}
	}

	if err := d.Save(mk("hash-raw", models.TaskDiscovered)); err != nil {
		t.Fatalf("写入爬虫记录失败: %v", err)
	}
	if err := d.Save(mk("hash-done", models.TaskCompleted)); err != nil {
		t.Fatalf("写入已完成任务失败: %v", err)
	}
	localName := "本地视频"
	if err := d.SaveLocalResource(&models.Resource{
		ResourceName: &localName,
		Kind:         models.KindLocal,
		TaskState:    models.TaskFailed,
		LocalState:   models.LocalMissing,
		CreateTime:   &now,
		UpdateTime:   &now,
	}); err != nil {
		t.Fatalf("写入本地视频失败: %v", err)
	}

	rows, err := d.ListPendingTasks()
	if err != nil {
		t.Fatalf("查询待下载任务失败: %v", err)
	}
	if len(rows) != len(pendingStates) {
		t.Fatalf("待下载任务数 = %d, want %d (%v)", len(rows), len(pendingStates), rows)
	}

	got := make(map[string]models.TaskState, len(rows))
	for _, row := range rows {
		got[row.InfoHash] = row.TaskState
	}
	for i, state := range pendingStates {
		hash := "hash-pending-" + string(rune('a'+i))
		if got[hash] != state {
			t.Fatalf("状态 %v 的任务未保留在待下载列表: %v", state, got)
		}
	}
	for _, hash := range []string{"hash-raw", "hash-done"} {
		if _, ok := got[hash]; ok {
			t.Fatalf("任务 %q 不应出现在待下载列表", hash)
		}
	}
}
