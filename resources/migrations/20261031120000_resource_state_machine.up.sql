-- 资源双轴状态机：新增状态列并回填旧状态
-- 旧列（download_status / local_status / retry_count / mate_retry_count / error_message / completed_time）
-- 保留一个版本，便于回滚与排查；业务代码只读写新列。

-- =========================================================
-- 1. 新增列
-- =========================================================

ALTER TABLE resources ADD COLUMN kind INTEGER NOT NULL DEFAULT 0;
ALTER TABLE resources ADD COLUMN task_state INTEGER NOT NULL DEFAULT 0;
ALTER TABLE resources ADD COLUMN local_state INTEGER NOT NULL DEFAULT 0;
ALTER TABLE resources ADD COLUMN local_loss_reason INTEGER NOT NULL DEFAULT 0;
ALTER TABLE resources ADD COLUMN state_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE resources ADD COLUMN attempt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE resources ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 3;
ALTER TABLE resources ADD COLUMN error_code TEXT NOT NULL DEFAULT '';
ALTER TABLE resources ADD COLUMN next_retry_at DATETIME;
ALTER TABLE resources ADD COLUMN state_changed_at DATETIME;
ALTER TABLE resources ADD COLUMN verified_at DATETIME;
ALTER TABLE resources ADD COLUMN local_checked_at DATETIME;

-- =========================================================
-- 2. 回填 TaskState / LocalState
-- =========================================================

-- 本地视频：没有 InfoHash 且旧下载状态为 completed
UPDATE resources
SET kind = 1,
    task_state = 6,
    local_state = CASE
        WHEN local_status = 'corrupted' THEN 4
        WHEN local_status = 'missing' OR local_status = 'deleted' THEN 3
        ELSE 2
    END
WHERE download_status = 'completed'
  AND (info_hash IS NULL OR info_hash = '');

-- 种子资源：按旧 download_status 映射 TaskState
UPDATE resources
SET task_state = CASE download_status
        WHEN 'download_meta_info' THEN 1
        WHEN 'queued' THEN 2
        WHEN 'downloading' THEN 3
        WHEN 'paused' THEN 4
        WHEN 'completed' THEN 6
        WHEN 'failed' THEN 8
        WHEN 'cancelled' THEN 9
        ELSE 0
    END,
    local_state = CASE local_status
        WHEN 'exists' THEN 2
        WHEN 'missing' THEN 3
        WHEN 'corrupted' THEN 4
        WHEN 'deleted' THEN 3
        ELSE 0
    END,
    local_loss_reason = CASE
        WHEN local_status = 'deleted' THEN 3
        WHEN local_status = 'missing' THEN 1
        WHEN local_status = 'corrupted' THEN 2
        ELSE 0
    END,
    attempt = COALESCE(retry_count, 0),
    max_attempts = CASE
        WHEN mate_retry_count IS NULL OR mate_retry_count <= 0 THEN 3
        ELSE mate_retry_count
    END,
    error_code = COALESCE(error_message, ''),
    state_changed_at = COALESCE(update_time, CURRENT_TIMESTAMP),
    verified_at = completed_time,
    next_retry_at = NULL,
    local_checked_at = NULL
WHERE kind = 0;

-- =========================================================
-- 3. 修正非法状态对（确保满足 validPair 不变量）
-- =========================================================

-- 已完成但没有任何本地文件：视为文件丢失
UPDATE resources SET local_state = 3, local_loss_reason = 1
WHERE task_state = 6 AND local_state = 0;

-- 校验中必须是部分数据
UPDATE resources SET local_state = 1
WHERE task_state = 5 AND local_state = 0;

-- 下载中 / 暂停 / 重试等待不允许"文件齐全"
UPDATE resources SET local_state = 1
WHERE task_state IN (3, 4, 7) AND local_state = 2;

-- 发现 / 准备 / 排队不允许已有本地数据
UPDATE resources SET local_state = 0
WHERE task_state IN (0, 1, 2) AND local_state <> 0;

-- 本地视频状态恒为 已完成
UPDATE resources SET task_state = 6
WHERE kind = 1;

-- =========================================================
-- 4. 索引
-- =========================================================

CREATE INDEX IF NOT EXISTS idx_resources_task_state ON resources(task_state);
CREATE INDEX IF NOT EXISTS idx_resources_kind_state ON resources(kind, task_state);
CREATE INDEX IF NOT EXISTS idx_resources_task_queue ON resources(task_state, priority DESC, create_time ASC);
CREATE INDEX IF NOT EXISTS idx_resources_retry ON resources(task_state, next_retry_at);
