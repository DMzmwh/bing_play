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

// legacyResourceVersion 三表拆分迁移之前的最后一个版本号。
const legacyResourceVersion = 20261102120000

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

// TestSplitResourceTablesMigration 校验三表拆分迁移：
//   - 旧 resources 保留为 resources_legacy，便于回滚与对账；
//   - resources 只回填带 InfoHash 的目录记录；
//   - download_tasks 只回填触发过下载的种子任务（缺失行等价于 Discovered）；
//   - local_files 回填本地视频与已有下载任务的种子，且不产生孤儿记录；
//   - 回滚后旧 resources 表与行数恢复。
func TestSplitResourceTablesMigration(t *testing.T) {
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

	// 先迁移到拆分前的版本，写入一批旧数据，再执行三表拆分迁移。
	if err := m.Migrate(legacyResourceVersion); err != nil {
		t.Fatalf("迁移到拆分前版本失败: %v", err)
	}

	exec := func(query string) {
		t.Helper()
		if _, err := sqlDB.Exec(query); err != nil {
			t.Fatalf("写入旧数据失败: %v\n%s", err, query)
		}
	}

	// 只在爬虫目录出现过的种子，没有下载任务。
	exec(`
INSERT INTO resources (
    resource_name, info_hash, source_url, torrent_path, total_size, resource_type,
    kind, task_state, download_status, local_status
) VALUES (
    '爬虫资源', 'crawl-hash', 'magnet:?xt=urn:btih:crawl-hash', '/tmp/crawl.torrent', 1024, 'video',
    0, 0, 'queued', 'not_downloaded'
);`)

	// 已完成的种子任务，带下载进度与本地文件。
	exec(`
INSERT INTO resources (
    resource_name, file_name, info_hash, source_url, torrent_path, total_size,
    local_path, file_size, downloaded_size, mime_type, resource_type,
    kind, task_state, local_state, local_loss_reason,
    state_version, attempt, max_attempts, priority,
    download_status, local_status, verified_at, update_time
) VALUES (
    '已下载资源', 'movie.mkv', 'torrent-hash', 'magnet:?xt=urn:btih:torrent-hash', '/tmp/torrent.torrent', 4096,
    '/downloads', 2048, 4096, 'video/x-matroska', 'video',
    0, 6, 2, 0,
    3, 1, 3, 5,
    'completed', 'exists', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
);`)

	// 本地拖入的视频，没有 InfoHash。
	exec(`
INSERT INTO resources (
    resource_name, file_name, info_hash, local_path, file_size, mime_type, resource_type,
    kind, task_state, local_state, local_loss_reason, played, play_progress,
    download_status, local_status
) VALUES (
    '本地视频', 'local.mp4', NULL, '/videos', 512, 'video/mp4', 'video',
    1, 6, 2, 0, 1, 30,
    'completed', 'exists'
);`)

	// 历史脏数据：InfoHash 为空字符串，没有任务也没有本地文件。
	exec(`
INSERT INTO resources (resource_name, info_hash, kind, task_state, download_status, local_status)
VALUES ('空哈希资源', '', 0, 0, 'queued', 'not_downloaded');`)

	if err := m.Up(); err != nil {
		t.Fatalf("执行三表拆分迁移失败: %v", err)
	}

	gdb, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{})
	if err != nil {
		t.Fatalf("初始化 GORM 失败: %v", err)
	}

	count := func(query string) int64 {
		t.Helper()
		var n int64
		if err := gdb.Raw(query).Scan(&n).Error; err != nil {
			t.Fatalf("统计失败: %v\n%s", err, query)
		}
		return n
	}

	if got := count("SELECT COUNT(*) FROM resources_legacy"); got != 4 {
		t.Fatalf("旧表行数 = %d, want 4", got)
	}
	if got := count("SELECT COUNT(*) FROM resources"); got != 2 {
		t.Fatalf("resources 行数 = %d, want 2（只保留带 InfoHash 的目录记录）", got)
	}
	if got := count("SELECT COUNT(*) FROM download_tasks"); got != 1 {
		t.Fatalf("download_tasks 行数 = %d, want 1（只保留触发过下载的任务）", got)
	}
	if got := count("SELECT COUNT(*) FROM local_files"); got != 2 {
		t.Fatalf("local_files 行数 = %d, want 2（本地视频 + 已完成种子）", got)
	}

	// 孤儿检查：任务要有对应资源，本地文件要有对应任务。
	if got := count(`
SELECT COUNT(*) FROM download_tasks dt
LEFT JOIN resources r ON r.info_hash = dt.info_hash
WHERE r.info_hash IS NULL`); got != 0 {
		t.Fatalf("存在没有资源元数据的下载任务: %d", got)
	}
	if got := count(`
SELECT COUNT(*) FROM local_files lf
LEFT JOIN download_tasks dt ON lf.info_hash = dt.info_hash
WHERE lf.info_hash IS NOT NULL AND dt.info_hash IS NULL`); got != 0 {
		t.Fatalf("存在没有下载任务的种子本地文件: %d", got)
	}

	// 任务展示快照不依赖 resources 表。
	var task models.DownloadTaskRecord
	if err := gdb.Where("info_hash = ?", "torrent-hash").First(&task).Error; err != nil {
		t.Fatalf("查询下载任务失败: %v", err)
	}
	if task.Name == nil || *task.Name != "已下载资源" {
		t.Fatalf("任务名称快照 = %v, want 已下载资源", task.Name)
	}
	if task.TotalSize == nil || *task.TotalSize != 4096 {
		t.Fatalf("任务总大小快照 = %v, want 4096", task.TotalSize)
	}
	if task.TaskState != models.TaskCompleted || task.StateVersion != 3 || task.Priority == nil || *task.Priority != 5 {
		t.Fatalf("任务状态回填错误: %+v", task)
	}

	var local models.LocalFileRecord
	if err := gdb.Where("info_hash IS NULL").First(&local).Error; err != nil {
		t.Fatalf("查询本地视频失败: %v", err)
	}
	if local.FileName == nil || *local.FileName != "local.mp4" {
		t.Fatalf("本地视频文件名 = %v, want local.mp4", local.FileName)
	}
	if local.ResourceName == nil || *local.ResourceName != "本地视频" {
		t.Fatalf("本地视频分组名 = %v, want 本地视频", local.ResourceName)
	}

	// 回滚后旧表与行数恢复，新表消失。
	if err := m.Steps(-1); err != nil {
		t.Fatalf("回滚三表拆分迁移失败: %v", err)
	}
	if got := count("SELECT COUNT(*) FROM resources"); got != 4 {
		t.Fatalf("回滚后 resources 行数 = %d, want 4", got)
	}
	if gdb.Migrator().HasTable("resources_legacy") {
		t.Fatal("回滚后不应保留 resources_legacy")
	}
	if gdb.Migrator().HasTable("download_tasks") || gdb.Migrator().HasTable("local_files") {
		t.Fatal("回滚后不应保留新三表")
	}
}
