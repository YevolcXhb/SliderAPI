ALTER TABLE batch_image_jobs
    ADD COLUMN IF NOT EXISTS downloaded_at DATETIME(6),
    ADD COLUMN IF NOT EXISTS user_deleted_at DATETIME(6);

CREATE INDEX IF NOT EXISTS batch_image_jobs_downloaded_at_idx ON batch_image_jobs (downloaded_at);
CREATE INDEX IF NOT EXISTS batch_image_jobs_user_deleted_at_idx ON batch_image_jobs (user_deleted_at);

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN batch_image_jobs.downloaded_at IS '用户首次成功下载批量图片 ZIP 的时间';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN batch_image_jobs.user_deleted_at IS '用户侧删除/隐藏任务记录的时间；账务记录仍保留';
