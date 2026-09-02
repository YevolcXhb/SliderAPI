-- 渠道定价倍率字段。
-- [MariaDB 重写] pg_constraint 判断 -> information_schema.check_constraints；NUMERIC 保持不变。
ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS fast_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS flex_multiplier NUMERIC(12,6);

ALTER TABLE channel_pricing_intervals
    ADD COLUMN IF NOT EXISTS input_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS output_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS cache_write_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS cache_read_multiplier NUMERIC(12,6);

DROP PROCEDURE IF EXISTS _mig228_add_checks;
CREATE PROCEDURE _mig228_add_checks()
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_model_pricing_fast_multiplier_positive') THEN
        ALTER TABLE channel_model_pricing ADD CONSTRAINT channel_model_pricing_fast_multiplier_positive CHECK (fast_multiplier IS NULL OR fast_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_model_pricing_flex_multiplier_positive') THEN
        ALTER TABLE channel_model_pricing ADD CONSTRAINT channel_model_pricing_flex_multiplier_positive CHECK (flex_multiplier IS NULL OR flex_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_pricing_intervals_input_multiplier_positive') THEN
        ALTER TABLE channel_pricing_intervals ADD CONSTRAINT channel_pricing_intervals_input_multiplier_positive CHECK (input_multiplier IS NULL OR input_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_pricing_intervals_output_multiplier_positive') THEN
        ALTER TABLE channel_pricing_intervals ADD CONSTRAINT channel_pricing_intervals_output_multiplier_positive CHECK (output_multiplier IS NULL OR output_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_pricing_intervals_cache_write_multiplier_positive') THEN
        ALTER TABLE channel_pricing_intervals ADD CONSTRAINT channel_pricing_intervals_cache_write_multiplier_positive CHECK (cache_write_multiplier IS NULL OR cache_write_multiplier > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_schema = DATABASE() AND constraint_name = 'channel_pricing_intervals_cache_read_multiplier_positive') THEN
        ALTER TABLE channel_pricing_intervals ADD CONSTRAINT channel_pricing_intervals_cache_read_multiplier_positive CHECK (cache_read_multiplier IS NULL OR cache_read_multiplier > 0);
    END IF;
END;
CALL _mig228_add_checks();
DROP PROCEDURE _mig228_add_checks;

-- 列注释（MariaDB COMMENT ON 已禁用，保留说明）：
-- channel_model_pricing.fast_multiplier: Fast/priority tier multiplier
-- channel_model_pricing.flex_multiplier: Flex tier multiplier
-- channel_pricing_intervals.{input,output,cache_write,cache_read}_multiplier: interval multipliers
