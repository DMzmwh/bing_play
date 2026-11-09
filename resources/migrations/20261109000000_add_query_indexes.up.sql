-- 资源库 / 下载列表查询性能优化：补齐 ORDER BY 与 LIKE 覆盖索引。
-- 首次启动会自动创建；CREATE INDEX IF NOT EXISTS 保证幂等。

CREATE INDEX IF NOT EXISTS idx_resources_create_time ON resources(create_time DESC);
CREATE INDEX IF NOT EXISTS idx_resources_update_time ON resources(update_time DESC);
CREATE INDEX IF NOT EXISTS idx_resources_name ON resources(resource_name);

CREATE INDEX IF NOT EXISTS idx_download_tasks_update_time ON download_tasks(update_time DESC);
