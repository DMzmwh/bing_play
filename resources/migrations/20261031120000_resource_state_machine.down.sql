-- 回滚资源双轴状态机：删除新增列与索引
-- 旧列未被删除，回滚后业务代码需同时回退到旧版本。

DROP INDEX IF EXISTS idx_resources_retry;
DROP INDEX IF EXISTS idx_resources_task_queue;
DROP INDEX IF EXISTS idx_resources_kind_state;
DROP INDEX IF EXISTS idx_resources_task_state;

ALTER TABLE resources DROP COLUMN local_checked_at;
ALTER TABLE resources DROP COLUMN verified_at;
ALTER TABLE resources DROP COLUMN state_changed_at;
ALTER TABLE resources DROP COLUMN next_retry_at;
ALTER TABLE resources DROP COLUMN error_code;
ALTER TABLE resources DROP COLUMN max_attempts;
ALTER TABLE resources DROP COLUMN attempt;
ALTER TABLE resources DROP COLUMN state_version;
ALTER TABLE resources DROP COLUMN local_loss_reason;
ALTER TABLE resources DROP COLUMN local_state;
ALTER TABLE resources DROP COLUMN task_state;
ALTER TABLE resources DROP COLUMN kind;
