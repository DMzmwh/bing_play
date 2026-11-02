-- 回滚电视直播相关表

DROP INDEX IF EXISTS idx_tv_channels_group;
DROP INDEX IF EXISTS idx_tv_channels_source;
DROP INDEX IF EXISTS idx_tv_channels_key;
DROP TABLE IF EXISTS tv_channels;

DROP INDEX IF EXISTS idx_tv_sources_url;
DROP TABLE IF EXISTS tv_sources;
