-- 回滚三表拆分：删除新表，恢复 resources_legacy 为 resources。
-- 注意：迁移后新写入的数据会随新表一起丢弃，回滚前应先备份。

DROP INDEX IF EXISTS idx_download_tasks_state;
DROP INDEX IF EXISTS idx_download_tasks_queue;
DROP INDEX IF EXISTS idx_download_tasks_retry;
DROP INDEX IF EXISTS idx_local_files_info_hash;
DROP INDEX IF EXISTS idx_local_files_state;

DROP TABLE IF EXISTS download_tasks;
DROP TABLE IF EXISTS local_files;
DROP TABLE IF EXISTS resources;

ALTER TABLE resources_legacy RENAME TO resources;

-- 恢复两个历史迁移创建的索引
CREATE UNIQUE INDEX IF NOT EXISTS idx_resources_info_hash
    ON resources(info_hash)
    WHERE info_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_resources_download_status ON resources(download_status);
CREATE INDEX IF NOT EXISTS idx_resources_local_status ON resources(local_status);
CREATE INDEX IF NOT EXISTS idx_resources_queue
    ON resources(download_status, priority DESC, create_time ASC);
CREATE INDEX IF NOT EXISTS idx_resources_last_play_time ON resources(last_play_time);
CREATE INDEX IF NOT EXISTS idx_resources_create_time ON resources(create_time);

CREATE INDEX IF NOT EXISTS idx_resources_task_state ON resources(task_state);
CREATE INDEX IF NOT EXISTS idx_resources_kind_state ON resources(kind, task_state);
CREATE INDEX IF NOT EXISTS idx_resources_task_queue
    ON resources(task_state, priority DESC, create_time ASC);
CREATE INDEX IF NOT EXISTS idx_resources_retry ON resources(task_state, next_retry_at);
