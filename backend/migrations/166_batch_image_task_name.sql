ALTER TABLE batch_image_jobs
    ADD COLUMN IF NOT EXISTS task_name VARCHAR(255) NOT NULL DEFAULT '';

UPDATE batch_image_jobs
SET task_name = DATE_FORMAT(CONVERT_TZ(created_at, '+00:00', '+08:00'), '%Y-%m-%d %H:%i:%s')
WHERE task_name = '';

CREATE INDEX IF NOT EXISTS batch_image_jobs_task_name_idx ON batch_image_jobs (task_name);

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN batch_image_jobs.task_name IS '用户可读的批量生图任务名称';