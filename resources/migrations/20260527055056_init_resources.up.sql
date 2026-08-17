CREATE TABLE resources (
    -- =========================================================
    -- 主键
    -- =========================================================

                           id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    -- 资源主键，自增 ID


    -- =========================================================
    -- 资源信息
    -- =========================================================

                           resource_name       TEXT,
    -- 资源名称，例如电影名称、视频名称

                           file_name           TEXT,
    -- 文件名称，例如 movie.mkv

                           source_url          TEXT,
    -- 资源来源地址，例如磁力链接、HTTP 地址等

                           cover_url           TEXT,
    -- 资源封面图片地址


    -- =========================================================
    -- Torrent 信息
    -- =========================================================

                           info_hash           TEXT,
    -- Torrent InfoHash，用于唯一标识一个 Torrent

                           torrent_path        TEXT,
    -- 本地 .torrent 文件路径

                           total_size          INTEGER DEFAULT 0,
    -- Torrent 总大小，单位：Byte


    -- =========================================================
    -- 本地文件
    -- =========================================================

                           local_path          TEXT,
    -- 下载文件在本地的绝对路径

                           file_size           INTEGER DEFAULT 0,
    -- 本地文件实际大小，单位：Byte

                           downloaded_size     INTEGER DEFAULT 0,
    -- 已经下载的数据量，单位：Byte

                           mime_type           TEXT,
    -- 文件 MIME 类型，例如 video/mp4、video/x-matroska


    -- =========================================================
    -- 本地文件状态
    -- =========================================================

                           local_status        TEXT DEFAULT 'not_downloaded',
    -- 本地文件状态
    --
    -- not_downloaded : 尚未下载
    -- exists         : 文件存在且正常
    -- missing        : 数据库记录存在，但本地文件不存在
    -- corrupted      : 本地文件存在，但文件损坏
    -- deleted        : 用户主动删除了本地文件


    -- =========================================================
    -- 下载状态
    -- =========================================================

                           download_status     TEXT DEFAULT 'queued',
    -- 下载任务状态
    --
    -- fetching_meta_info : 正在获取 Torrent 元信息
    -- queued             : 等待下载
    -- downloading        : 下载中
    -- paused             : 已暂停
    -- completed          : 下载完成
    -- failed             : 下载失败
    -- cancelled          : 已取消


    -- =========================================================
    -- 下载控制
    -- =========================================================

                           priority            INTEGER DEFAULT 0,
    -- 下载优先级，数值越大优先级越高

                           retry_count         INTEGER DEFAULT 0,
    -- 当前任务已经自动重试的次数

                           mate_retry_count     INTEGER DEFAULT 3,
    -- 最大自动重试次数

                           error_message       TEXT,
    -- 最近一次下载失败的错误信息


    -- =========================================================
    -- 播放信息
    -- =========================================================

                           total_duration      INTEGER DEFAULT 0,
    -- 媒体总时长，单位：秒

                           played              INTEGER DEFAULT 0,
    -- 是否曾经播放过
    -- 0：未播放
    -- 1：已播放

                           play_progress       INTEGER DEFAULT 0,
    -- 当前播放进度，单位：秒

                           play_count          INTEGER DEFAULT 0,
    -- 播放次数

                           last_play_time      DATETIME,
    -- 最后一次播放时间

                           finished            INTEGER DEFAULT 0,
    -- 是否已经播放完成
    -- 0：未完成
    -- 1：已完成


    -- =========================================================
    -- 收藏 / 标签
    -- =========================================================

                           favorite            INTEGER DEFAULT 0,
    -- 是否收藏
    -- 0：未收藏
    -- 1：已收藏

                           tags                TEXT,
    -- 资源标签，可以保存 JSON，例如 ["电影","动作"]

                           remark              TEXT,
    -- 用户备注信息


    -- =========================================================
    -- 时间
    -- =========================================================

                           create_time         DATETIME DEFAULT CURRENT_TIMESTAMP,
    -- 资源创建时间

                           update_time         DATETIME DEFAULT CURRENT_TIMESTAMP,
    -- 资源最后更新时间

                           completed_time      DATETIME
    -- 下载完成时间，未完成时为 NULL
);


-- =============================================================
-- 索引（完全原样复制，无改动）
-- =============================================================

-- =============================================================
-- Torrent InfoHash 唯一索引
-- =============================================================

CREATE UNIQUE INDEX idx_resources_info_hash
    ON resources(info_hash)
    WHERE info_hash IS NOT NULL;
-- 同一个 Torrent 不允许重复添加
-- NULL 不参与唯一约束


-- =============================================================
-- 下载状态索引
-- =============================================================

CREATE INDEX idx_resources_download_status
    ON resources(download_status);
-- 用于快速查询：
-- queued
-- downloading
-- paused
-- completed
-- failed
-- 等状态


-- =============================================================
-- 本地文件状态索引
-- =============================================================

CREATE INDEX idx_resources_local_status
    ON resources(local_status);
-- 用于快速查询本地文件状态


-- =============================================================
-- 下载队列复合索引
-- =============================================================

CREATE INDEX idx_resources_queue
    ON resources(
                 download_status,
                 priority DESC,
                 create_time ASC
        );
-- 用于下载队列查询：
--
-- WHERE download_status = 'queued'
-- ORDER BY priority DESC, create_time ASC
--
-- 优先级高的任务先下载
-- 优先级相同时，创建时间早的任务先下载


-- =============================================================
-- 最近播放时间索引
-- =============================================================

CREATE INDEX idx_resources_last_play_time
    ON resources(last_play_time);
-- 用于查询最近播放的资源


-- =============================================================
-- 创建时间索引
-- =============================================================

CREATE INDEX idx_resources_create_time
    ON resources(create_time);
-- 用于按照创建时间查询、排序资源
