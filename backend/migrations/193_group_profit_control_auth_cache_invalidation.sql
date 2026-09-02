-- Profit-control / pricing / peak-window fields 也是 API-key auth 快照的一部分。
-- 再次扩展 groups UPDATE 失效触发器，作为带外编辑的持久兜底。
-- [MariaDB 重写] 基于 186 的最新触发器体，追加利润/定价/峰时字段判断；DROP + CREATE 重建。
DROP TRIGGER IF EXISTS trg_groups_auth_cache_invalidation_upd;
CREATE TRIGGER trg_groups_auth_cache_invalidation_upd
AFTER UPDATE ON `groups`
FOR EACH ROW
BEGIN
    IF NOT ((OLD.status <=> NEW.status)
        AND (OLD.is_exclusive <=> NEW.is_exclusive)
        AND (OLD.allow_image_generation <=> NEW.allow_image_generation)
        AND (OLD.platform <=> NEW.platform)
        AND (OLD.subscription_type <=> NEW.subscription_type)
        AND (OLD.rate_multiplier <=> NEW.rate_multiplier)
        AND (OLD.peak_rate_enabled <=> NEW.peak_rate_enabled)
        AND (OLD.peak_start <=> NEW.peak_start)
        AND (OLD.peak_end <=> NEW.peak_end)
        AND (OLD.peak_rate_multiplier <=> NEW.peak_rate_multiplier)
        AND (OLD.profit_control_enabled <=> NEW.profit_control_enabled)
        AND (OLD.profit_min_margin <=> NEW.profit_min_margin)
        AND (OLD.profit_safety_buffer <=> NEW.profit_safety_buffer)
        AND (OLD.deleted_at <=> NEW.deleted_at)) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT LOWER(SHA2(k.`key`, 256))
        FROM api_keys AS k
        WHERE k.group_id = OLD.id AND k.deleted_at IS NULL AND k.`key` <> '';
    END IF;
END;
