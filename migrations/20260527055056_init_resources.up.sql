CREATE TABLE resources (
    id TEXT PRIMARY KEY,
    peer_id TEXT,
    file_id TEXT,
    file_name TEXT,
    file_size INTEGER,
    version INTEGER,
    timestamp INTEGER
);

CREATE INDEX idx_peer_id ON resources(peer_id);


CREATE TABLE download_task (
                               id                  INTEGER PRIMARY KEY AUTOINCREMENT,

    -- 资源信息
                               resource_id         TEXT,                   -- 服务端资源ID
                               resource_name       TEXT NOT NULL,          -- 视频名称
                               file_name           TEXT,                   -- 下载后的文件名
                               source_url          TEXT,                   -- 下载地址
                               cover_url           TEXT,                   -- 封面

    -- 本地文件
                               local_path          TEXT,                   -- 本地绝对路径
                               file_size           INTEGER DEFAULT 0,      -- 文件大小(byte)
                               downloaded_size     INTEGER DEFAULT 0,      -- 已下载大小
                               file_hash           TEXT,                   -- md5/sha256
                               mime_type           TEXT,

    -- 本地状态
                               local_status        INTEGER DEFAULT 0,
    -- 0 未下载
    -- 1 文件存在
    -- 2 文件不存在
    -- 3 文件损坏
    -- 4 已删除

    -- 下载状态
                               download_status     INTEGER DEFAULT 0,
    -- 0 未开始
    -- 1 等待
    -- 2 下载中
    -- 3 已暂停
    -- 4 已完成
    -- 5 下载失败
    -- 6 已取消

                               download_progress   REAL DEFAULT 0,         -- 0~100

                               download_speed      INTEGER DEFAULT 0,      -- byte/s

                               total_duration      INTEGER DEFAULT 0,      -- 视频总时长(秒)

    -- 播放信息
                               played              INTEGER DEFAULT 0,      -- 是否播放过

                               play_progress       INTEGER DEFAULT 0,      -- 播放到第几秒

                               play_percent        REAL DEFAULT 0,

                               play_count          INTEGER DEFAULT 0,

                               last_play_time      DATETIME,

                               finished            INTEGER DEFAULT 0,      -- 是否播放完成

    -- 收藏
                               favorite            INTEGER DEFAULT 0,

    -- 标签
                               tags                TEXT,

                               remark              TEXT,

    -- 创建更新时间
                               create_time         DATETIME DEFAULT CURRENT_TIMESTAMP,

                               update_time         DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_download_status ON download_task(download_status);

CREATE INDEX idx_local_status ON download_task(local_status);

CREATE INDEX idx_resource_id ON download_task(resource_id);

CREATE INDEX idx_last_play_time ON download_task(last_play_time);