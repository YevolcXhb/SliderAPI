ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS allow_batch_image_generation TINYINT(1) NOT NULL DEFAULT false;

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN groups.allow_batch_image_generation IS '是否允许该分组使用批量图片生成能力';
