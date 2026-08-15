-- 为 redeem_codes 表添加备注字段

ALTER TABLE redeem_codes
ADD COLUMN IF NOT EXISTS notes TEXT DEFAULT NULL;

-- (migrated from COMMENT ON COLUMN) redeem_codes.notes