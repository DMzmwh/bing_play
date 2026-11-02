-- 把单表 resources 拆分成交付职责更清晰的三张表：
--   resources      : 资源元数据（爬虫目录）
--   download_tasks : 下载任务状态（以 info_hash 为主键）
--   local_files    : 本地文件 / 播放项（保留自增 ID 作为播放身份）
--
-- 旧表重命名为 resources_legacy 并保留一个版本，便于回滚与对账。
-- 业务代码此后只读写新三张表。

-- =========================================================
-- 1. 旧表退场，释放索引名称
-- =========================================================

ALTER TABLE resources RENAME TO resources_legacy;

DROP INDEX IF EXISTS idx_resources_info_hash;
DROP INDEX IF EXISTS idx_resources_download_status;
DROP INDEX IF EXISTS idx_resources_local_status;
DROP INDEX IF EXISTS idx_resources_queue;
DROP INDEX IF EXISTS idx_resources_last_play_time;
DROP INDEX IF EXISTS idx_resources_create_time;
DROP INDEX IF EXISTS idx_resources_task_state;
DROP INDEX IF EXISTS idx_resources_kind_state;
DROP INDEX IF EXISTS idx_resources_task_queue;
DROP INDEX IF EXISTS idx_resources_retry;

-- =========================================================
-- 2. 新表
-- =========================================================

CREATE TABLE resources (
    info_hash       TEXT PRIMARY KEY,
    -- Torrent InfoHash，资源目录的唯一标识；本地视频不写入该表

    resource_name   TEXT,
    source_url      TEXT,
    cover_url       TEXT,
    total_size      INTEGER DEFAULT 0,
    torrent_path    TEXT,
    resource_type   TEXT,

    favorite        INTEGER DEFAULT 0,
    tags            TEXT,
    remark          TEXT,

    create_time     DATETIME DEFAULT CURRENT_TIMESTAMP,
    update_time     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE download_tasks (
    info_hash       TEXT PRIMARY KEY,
    -- 下载任务只按 InfoHash 关联，不使用自增 ID

    name            TEXT,
    -- 以下四个字段为展示快照，下载列表不依赖 resources 表
    source_url      TEXT,
    torrent_path    TEXT,
    total_size      INTEGER DEFAULT 0,

    task_state      INTEGER NOT NULL DEFAULT 0,
    state_version   INTEGER NOT NULL DEFAULT 0,
    attempt         INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 3,
    error_code      TEXT NOT NULL DEFAULT '',
    next_retry_at   DATETIME,
    state_changed_at DATETIME,

    priority        INTEGER DEFAULT 0,
    downloaded_size INTEGER DEFAULT 0,
    save_path       TEXT NOT NULL DEFAULT '',

    update_time     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE local_files (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    -- 本地文件 / 播放项的唯一身份，播放接口依赖该 ID

    info_hash         TEXT,
    -- 种子资源关联 InfoHash；本地拖入的视频为 NULL

    resource_name     TEXT,
    local_state       INTEGER NOT NULL DEFAULT 0,
    local_loss_reason INTEGER NOT NULL DEFAULT 0,
    local_path        TEXT NOT NULL DEFAULT '',
    file_name         TEXT,
    file_size         INTEGER DEFAULT 0,
    mime_type         TEXT,

    verified_at       DATETIME,
    local_checked_at  DATETIME,

    total_duration    INTEGER DEFAULT 0,
    played            INTEGER DEFAULT 0,
    play_progress     INTEGER DEFAULT 0,
    play_count        INTEGER DEFAULT 0,
    last_play_time    DATETIME,
    finished          INTEGER DEFAULT 0,

    create_time       DATETIME DEFAULT CURRENT_TIMESTAMP,
    update_time       DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- =========================================================
-- 3. 新表索引
-- =========================================================

CREATE INDEX idx_download_tasks_state ON download_tasks(task_state);
CREATE INDEX idx_download_tasks_queue ON download_tasks(task_state, priority DESC, update_time ASC);
CREATE INDEX idx_download_tasks_retry ON download_tasks(task_state, next_retry_at);

CREATE INDEX idx_local_files_info_hash ON local_files(info_hash);
CREATE INDEX idx_local_files_state ON local_files(local_state);

-- =========================================================
-- 4. 回填
-- =========================================================

-- 资源目录：只保留带 InfoHash 的种子资源
INSERT INTO resources (
    info_hash, resource_name, source_url, cover_url, total_size,
    torrent_path, resource_type, favorite, tags, remark,
    create_time, update_time
)
SELECT
    info_hash, resource_name, source_url, cover_url, COALESCE(total_size, 0),
    torrent_path, resource_type, COALESCE(favorite, 0), tags, remark,
    create_time, update_time
FROM resources_legacy
WHERE info_hash IS NOT NULL AND info_hash <> '';

-- 下载任务：缺失行等价于 Discovered，因此只回填触发过下载的状态
INSERT INTO download_tasks (
    info_hash, name, source_url, torrent_path, total_size,
    task_state, state_version, attempt, max_attempts, error_code,
    next_retry_at, state_changed_at, priority, downloaded_size, save_path,
    update_time
)
SELECT
    info_hash, resource_name, source_url, torrent_path, COALESCE(total_size, 0),
    task_state, COALESCE(state_version, 0), COALESCE(attempt, 0), COALESCE(max_attempts, 3),
    COALESCE(error_code, ''),
    next_retry_at, state_changed_at, COALESCE(priority, 0), COALESCE(downloaded_size, 0),
    COALESCE(local_path, ''),
    update_time
FROM resources_legacy
WHERE kind = 0
  AND info_hash IS NOT NULL AND info_hash <> ''
  AND task_state <> 0;

-- 本地文件：本地视频 + 已有下载任务的种子（任务创建时即建行）
INSERT INTO local_files (
    info_hash, resource_name, local_state, local_loss_reason, local_path,
    file_name, file_size, mime_type, verified_at, local_checked_at,
    total_duration, played, play_progress, play_count, last_play_time, finished,
    create_time, update_time
)
SELECT
    CASE WHEN info_hash IS NULL OR info_hash = '' THEN NULL ELSE info_hash END,
    resource_name,
    COALESCE(local_state, 0),
    COALESCE(local_loss_reason, 0),
    COALESCE(local_path, ''),
    file_name, file_size, mime_type, verified_at, local_checked_at,
    total_duration, played, play_progress, play_count, last_play_time, finished,
    create_time, update_time
FROM resources_legacy
WHERE kind = 1
   OR (kind = 0
       AND info_hash IS NOT NULL AND info_hash <> ''
       AND task_state <> 0);
