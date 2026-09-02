-- Add generated-image billing size audit metadata.
-- `image_size` remains the canonical billing tier used for cost calculation.
-- [MariaDB 重写] pg_constraint 判断 -> information_schema.check_constraints；去掉 PG 的 NOT VALID。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS image_input_size VARCHAR(32);
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS image_output_size VARCHAR(32);
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS image_size_source VARCHAR(16);
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS image_size_breakdown JSON;

DROP PROCEDURE IF EXISTS _mig136_add_checks;
CREATE PROCEDURE _mig136_add_checks()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_schema = DATABASE() AND constraint_name = 'usage_logs_image_size_source_check'
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_image_size_source_check
            CHECK (
                image_size_source IS NULL
                OR image_size_source IN ('output', 'input', 'default', 'legacy')
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_schema = DATABASE() AND constraint_name = 'usage_logs_image_billing_size_check'
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_image_billing_size_check
            CHECK (
                image_count <= 0
                OR (
                    image_size IS NOT NULL
                    AND image_size IN ('1K', '2K', '4K', 'mixed')
                )
            );
    END IF;
END;
CALL _mig136_add_checks();
DROP PROCEDURE _mig136_add_checks;
