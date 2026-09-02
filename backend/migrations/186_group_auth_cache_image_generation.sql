-- Group image-generation permission is part of the API-key auth snapshot.
-- 扩展 184 的 groups 失效触发器，纳入 allow_image_generation 字段变化。
-- [MariaDB 重写] PG 用 CREATE OR REPLACE FUNCTION 复用触发器函数；MariaDB 无独立触发器函数，
-- 改为 DROP + CREATE 重建 groups UPDATE 触发器（DELETE 触发器保持 184 不变）。
DROP TRIGGER IF EXISTS trg_groups_auth_cache_invalidation_upd;
CREATE TRIGGER trg_groups_auth_cache_invalidation_upd
AFTER UPDATE ON `groups`
FOR EACH ROW
BEGIN
    IF NOT ((OLD.status <=> NEW.status)
        AND (OLD.is_exclusive <=> NEW.is_exclusive)
        AND (OLD.allow_image_generation <=> NEW.allow_image_generation)
        AND (OLD.deleted_at <=> NEW.deleted_at)) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT LOWER(SHA2(k.`key`, 256))
        FROM api_keys AS k
        WHERE k.group_id = OLD.id AND k.deleted_at IS NULL AND k.`key` <> '';
    END IF;
END;
