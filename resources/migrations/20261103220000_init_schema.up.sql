-- Bing Play 初始基线迁移（发布前把历史迁移合并为单文件）。
-- 包含运行所需的全部表、索引、内置 IPTV 订阅源，以及历史时间文本归一化。
--
-- 为兼容已存在的本地开发库，DDL 使用 IF NOT EXISTS、种子数据使用 INSERT OR IGNORE，
-- 因此同一份基线既能初始化空库，也能安全地把旧开发库升级到当前结构。
-- 注意：历史 resources_legacy 表仅用于旧版本回滚对账，基线不再创建。

CREATE TABLE IF NOT EXISTS resources (
    info_hash       TEXT PRIMARY KEY,
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

CREATE TABLE IF NOT EXISTS download_tasks (
    info_hash        TEXT PRIMARY KEY,
    name             TEXT,
    source_url       TEXT,
    torrent_path     TEXT,
    total_size       INTEGER DEFAULT 0,
    task_state       INTEGER NOT NULL DEFAULT 0,
    state_version    INTEGER NOT NULL DEFAULT 0,
    attempt          INTEGER NOT NULL DEFAULT 0,
    max_attempts     INTEGER NOT NULL DEFAULT 3,
    error_code       TEXT NOT NULL DEFAULT '',
    next_retry_at    DATETIME,
    state_changed_at DATETIME,
    priority         INTEGER DEFAULT 0,
    downloaded_size  INTEGER DEFAULT 0,
    save_path        TEXT NOT NULL DEFAULT '',
    update_time      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS local_files (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    info_hash         TEXT,
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

CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT NOT NULL PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tv_sources (
    id            INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
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

CREATE TABLE IF NOT EXISTS tv_channels (
    id          INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
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

CREATE INDEX IF NOT EXISTS idx_download_tasks_state ON download_tasks(task_state);
CREATE INDEX IF NOT EXISTS idx_download_tasks_queue ON download_tasks(task_state, priority DESC, update_time ASC);
CREATE INDEX IF NOT EXISTS idx_download_tasks_retry ON download_tasks(task_state, next_retry_at);
CREATE INDEX IF NOT EXISTS idx_local_files_info_hash ON local_files(info_hash);
CREATE INDEX IF NOT EXISTS idx_local_files_state ON local_files(local_state);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tv_sources_url ON tv_sources (url);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tv_channels_key ON tv_channels (channel_key);
CREATE INDEX IF NOT EXISTS idx_tv_channels_source ON tv_channels (source_id);
CREATE INDEX IF NOT EXISTS idx_tv_channels_group ON tv_channels (group_title, sort_order);

INSERT OR IGNORE INTO tv_sources (name, url)
VALUES ('IPTV 合集', 'https://raw.githubusercontent.com/cs3306/IPTV-Sources/main/data/output/iptv_collection.m3u');

-- 历史时间文本统一为秒级本地时间 "YYYY-MM-DD HH:MM:SS"：
-- 兼容 "YYYY-MM-DDTHH:MM:SS.fffffff+08:00" 与驱动默认的
-- "YYYY-MM-DD HH:MM:SS.fffffff +0800 CST m=+..." 两种格式。
-- 空库上无数据，旧开发库升级时会完成转换；已是标准格式的行会被跳过。
UPDATE resources
SET create_time = substr(replace(create_time, 'T', ' '), 1, 19)
WHERE create_time IS NOT NULL AND create_time <> ''
  AND create_time NOT LIKE '____-__-__ __:__:__';

UPDATE resources
SET update_time = substr(replace(update_time, 'T', ' '), 1, 19)
WHERE update_time IS NOT NULL AND update_time <> ''
  AND update_time NOT LIKE '____-__-__ __:__:__';

UPDATE download_tasks
SET next_retry_at = substr(replace(next_retry_at, 'T', ' '), 1, 19)
WHERE next_retry_at IS NOT NULL AND next_retry_at <> ''
  AND next_retry_at NOT LIKE '____-__-__ __:__:__';

UPDATE download_tasks
SET state_changed_at = substr(replace(state_changed_at, 'T', ' '), 1, 19)
WHERE state_changed_at IS NOT NULL AND state_changed_at <> ''
  AND state_changed_at NOT LIKE '____-__-__ __:__:__';

UPDATE download_tasks
SET update_time = substr(replace(update_time, 'T', ' '), 1, 19)
WHERE update_time IS NOT NULL AND update_time <> ''
  AND update_time NOT LIKE '____-__-__ __:__:__';

UPDATE local_files
SET verified_at = substr(replace(verified_at, 'T', ' '), 1, 19)
WHERE verified_at IS NOT NULL AND verified_at <> ''
  AND verified_at NOT LIKE '____-__-__ __:__:__';

UPDATE local_files
SET local_checked_at = substr(replace(local_checked_at, 'T', ' '), 1, 19)
WHERE local_checked_at IS NOT NULL AND local_checked_at <> ''
  AND local_checked_at NOT LIKE '____-__-__ __:__:__';

UPDATE local_files
SET last_play_time = substr(replace(last_play_time, 'T', ' '), 1, 19)
WHERE last_play_time IS NOT NULL AND last_play_time <> ''
  AND last_play_time NOT LIKE '____-__-__ __:__:__';

UPDATE local_files
SET create_time = substr(replace(create_time, 'T', ' '), 1, 19)
WHERE create_time IS NOT NULL AND create_time <> ''
  AND create_time NOT LIKE '____-__-__ __:__:__';

UPDATE local_files
SET update_time = substr(replace(update_time, 'T', ' '), 1, 19)
WHERE update_time IS NOT NULL AND update_time <> ''
  AND update_time NOT LIKE '____-__-__ __:__:__';

UPDATE tv_sources
SET last_sync_at = substr(replace(last_sync_at, 'T', ' '), 1, 19)
WHERE last_sync_at IS NOT NULL AND last_sync_at <> ''
  AND last_sync_at NOT LIKE '____-__-__ __:__:__';

UPDATE tv_sources
SET create_time = substr(replace(create_time, 'T', ' '), 1, 19)
WHERE create_time IS NOT NULL AND create_time <> ''
  AND create_time NOT LIKE '____-__-__ __:__:__';

UPDATE tv_sources
SET update_time = substr(replace(update_time, 'T', ' '), 1, 19)
WHERE update_time IS NOT NULL AND update_time <> ''
  AND update_time NOT LIKE '____-__-__ __:__:__';

UPDATE tv_channels
SET create_time = substr(replace(create_time, 'T', ' '), 1, 19)
WHERE create_time IS NOT NULL AND create_time <> ''
  AND create_time NOT LIKE '____-__-__ __:__:__';

UPDATE tv_channels
SET update_time = substr(replace(update_time, 'T', ' '), 1, 19)
WHERE update_time IS NOT NULL AND update_time <> ''
  AND update_time NOT LIKE '____-__-__ __:__:__';

UPDATE app_settings
SET updated_at = substr(replace(updated_at, 'T', ' '), 1, 19)
WHERE updated_at IS NOT NULL AND updated_at <> ''
  AND updated_at NOT LIKE '____-__-__ __:__:__';
