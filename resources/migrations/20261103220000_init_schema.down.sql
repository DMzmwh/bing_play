-- 回滚初始基线：删除全部运行期表。
DROP TABLE IF EXISTS tv_channels;
DROP TABLE IF EXISTS tv_sources;
DROP TABLE IF EXISTS app_settings;
DROP TABLE IF EXISTS local_files;
DROP TABLE IF EXISTS download_tasks;
DROP TABLE IF EXISTS resources;
