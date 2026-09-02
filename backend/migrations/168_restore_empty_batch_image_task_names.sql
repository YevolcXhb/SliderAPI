UPDATE batch_image_jobs
SET task_name = DATE_FORMAT(CONVERT_TZ(created_at, '+00:00', '+08:00'), '%Y-%m-%d %H:%i:%s')
WHERE task_name = '';

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN batch_image_jobs.task_name IS '用户填写的批量生图任务名称；提交时为空则默认写入当前时间';