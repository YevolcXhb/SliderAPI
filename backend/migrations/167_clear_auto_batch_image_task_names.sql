UPDATE batch_image_jobs
SET task_name = ''
WHERE task_name = DATE_FORMAT(CONVERT_TZ(created_at, '+00:00', '+08:00'), '%Y-%m-%d %H:%i:%s');

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN batch_image_jobs.task_name IS '用户填写的批量生图任务名称；为空时用户侧显示未填写';