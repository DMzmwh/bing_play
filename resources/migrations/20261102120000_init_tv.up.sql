-- =============================================================
-- 电视直播：订阅源与频道表
-- =============================================================
-- 订阅源按 M3U 地址保存，频道数据同步落库后由前端直接读取。
-- 对应 Go 模型 internal/db/models.TVSource、models.TVChannel。
-- =============================================================

CREATE TABLE tv_sources (
    -- =========================================================
    -- 主键
    -- =========================================================

    id              INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    -- 订阅源主键，自增 ID


    -- =========================================================
    -- 订阅信息
    -- =========================================================

    name            TEXT     NOT NULL DEFAULT '',
    -- 订阅源名称，用于界面展示

    url             TEXT     NOT NULL,
    -- M3U 订阅地址，唯一，避免重复录入同一订阅源

    enabled         INTEGER  NOT NULL DEFAULT 1,
    -- 是否启用：1 启用，0 停用；停用后不参与定时同步


    -- =========================================================
    -- 同步状态
    -- =========================================================

    last_sync_at    DATETIME,
    -- 最近一次同步时间，首次同步前为 NULL

    last_error      TEXT     NOT NULL DEFAULT '',
    -- 最近一次同步失败的 i18n 消息 ID，成功时为空

    etag            TEXT     NOT NULL DEFAULT '',
    -- 服务端返回的 ETag，用于条件请求，减少重复下载

    last_modified   TEXT     NOT NULL DEFAULT '',
    -- 服务端返回的 Last-Modified，用于条件请求


    -- =========================================================
    -- 时间
    -- =========================================================

    create_time     DATETIME DEFAULT CURRENT_TIMESTAMP,
    -- 创建时间

    update_time     DATETIME DEFAULT CURRENT_TIMESTAMP
    -- 最后更新时间
);

CREATE UNIQUE INDEX idx_tv_sources_url ON tv_sources (url);


CREATE TABLE tv_channels (
    -- =========================================================
    -- 主键
    -- =========================================================

    id              INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    -- 频道主键，自增 ID


    -- =========================================================
    -- 归属与标识
    -- =========================================================

    source_id       INTEGER  NOT NULL,
    -- 所属订阅源 ID，对应 tv_sources.id

    channel_key     TEXT     NOT NULL,
    -- 频道唯一标识：tvg-id + tvg-name + 显示名 + 播放地址的 sha1
    -- 同一订阅源重复同步时按该列 upsert，避免产生重复频道

    tvg_id          TEXT     NOT NULL DEFAULT '',
    -- EPG 频道标识，例如 572136

    tvg_name        TEXT     NOT NULL DEFAULT '',
    -- 频道标准名称，例如 CCTV-11


    -- =========================================================
    -- 展示信息
    -- =========================================================

    name            TEXT     NOT NULL DEFAULT '',
    -- 用户看到的频道显示名，取自 #EXTINF 逗号后的内容

    logo            TEXT     NOT NULL DEFAULT '',
    -- 台标图片地址

    group_title     TEXT     NOT NULL DEFAULT '',
    -- 频道分组，例如「中国大陆 · 央视」


    -- =========================================================
    -- 播放信息
    -- =========================================================

    stream_url      TEXT     NOT NULL,
    -- 直播流地址，HLS 通常为 .m3u8

    duration        INTEGER  NOT NULL DEFAULT -1,
    -- #EXTINF 中的节目时长，直播固定为 -1

    sort_order      INTEGER  NOT NULL DEFAULT 0,
    -- 在同一订阅源中的原始顺序，用于稳定排序


    -- =========================================================
    -- 时间
    -- =========================================================

    create_time     DATETIME DEFAULT CURRENT_TIMESTAMP,
    -- 创建时间

    update_time     DATETIME DEFAULT CURRENT_TIMESTAMP
    -- 最后更新时间
);

CREATE UNIQUE INDEX idx_tv_channels_key ON tv_channels (channel_key);
CREATE INDEX idx_tv_channels_source ON tv_channels (source_id);
CREATE INDEX idx_tv_channels_group ON tv_channels (group_title, sort_order);


-- =============================================================
-- 内置默认订阅源
-- =============================================================

INSERT INTO tv_sources (name, url)
VALUES ('IPTV 合集', 'https://raw.githubusercontent.com/cs3306/IPTV-Sources/main/data/output/iptv_collection.m3u');
