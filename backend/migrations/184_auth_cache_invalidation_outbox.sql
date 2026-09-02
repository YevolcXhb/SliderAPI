-- Durable, transactionally-enqueued API-key auth cache invalidation.
-- cache_key is always SHA-256 hex; plaintext credentials never leave api_keys.
--
-- [MariaDB 重写]
--   1. CHECK (cache_key ~ '^..$')  -> CHECK (cache_key REGEXP '^..$')
--   2. 部分索引 WHERE 已移除（MariaDB 不支持）。
--   3. plpgsql 函数 -> 保留的 SHA 助手用 MariaDB FUNCTION；触发器逻辑内联进各 CREATE TRIGGER。
--   4. encode(sha256(convert_to(k,'UTF8')),'hex') -> LOWER(SHA2(k,256))。
--   5. "AFTER UPDATE OR DELETE" 组合触发器 -> 拆成 per-op 触发器（MariaDB 每事件一个）。
--   6. IS DISTINCT FROM a,b -> NOT (a <=> b)；PERFORM f() -> 直接 INSERT/调用。

CREATE TABLE IF NOT EXISTS auth_cache_invalidation_outbox (
    id            BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    cache_key     CHAR(64) NOT NULL CHECK (cache_key REGEXP '^[0-9a-f]{64}$'),
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    available_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    delivery_stage SMALLINT NOT NULL DEFAULT 0 CHECK (delivery_stage IN (0, 1)),
    attempts      INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error    TEXT,
    claimed_at    DATETIME(6),
    claimed_by    TEXT
);

CREATE INDEX IF NOT EXISTS idx_auth_cache_invalidation_outbox_available
    ON auth_cache_invalidation_outbox (available_at, id);
CREATE INDEX IF NOT EXISTS idx_auth_cache_invalidation_outbox_lease
    ON auth_cache_invalidation_outbox (claimed_at);
CREATE INDEX IF NOT EXISTS idx_auth_cache_invalidation_outbox_cache_key
    ON auth_cache_invalidation_outbox (cache_key);
CREATE INDEX IF NOT EXISTS idx_auth_cache_invalidation_outbox_created_at
    ON auth_cache_invalidation_outbox (created_at);

-- 共享助手：把明文 key 的 SHA-256 hex 入队。
DROP FUNCTION IF EXISTS enqueue_auth_cache_invalidation;
CREATE FUNCTION enqueue_auth_cache_invalidation(raw_key TEXT)
RETURNS INT
MODIFIES SQL DATA
BEGIN
    IF raw_key IS NULL OR raw_key = '' THEN
        RETURN 0;
    END IF;
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    VALUES (LOWER(SHA2(raw_key, 256)));
    RETURN 1;
END;

-- api_keys: UPDATE 与 DELETE 各一个触发器。
DROP TRIGGER IF EXISTS trg_api_keys_auth_cache_invalidation_upd;
CREATE TRIGGER trg_api_keys_auth_cache_invalidation_upd
AFTER UPDATE ON api_keys
FOR EACH ROW
BEGIN
    IF NOT (OLD.`key` <=> NEW.`key`)
       OR NOT (OLD.status <=> NEW.status)
       OR NOT (OLD.deleted_at <=> NEW.deleted_at)
       OR NOT (OLD.user_id <=> NEW.user_id)
       OR NOT (OLD.group_id <=> NEW.group_id)
       OR NOT (OLD.ip_whitelist <=> NEW.ip_whitelist)
       OR NOT (OLD.ip_blacklist <=> NEW.ip_blacklist)
       OR NOT (OLD.expires_at <=> NEW.expires_at) THEN
        DO enqueue_auth_cache_invalidation(OLD.`key`);
        IF NEW.deleted_at IS NULL AND NOT (NEW.`key` <=> OLD.`key`) THEN
            DO enqueue_auth_cache_invalidation(NEW.`key`);
        END IF;
    END IF;
END;

DROP TRIGGER IF EXISTS trg_api_keys_auth_cache_invalidation_del;
CREATE TRIGGER trg_api_keys_auth_cache_invalidation_del
AFTER DELETE ON api_keys
FOR EACH ROW
BEGIN
    DO enqueue_auth_cache_invalidation(OLD.`key`);
END;

-- users: UPDATE 与 DELETE。
DROP TRIGGER IF EXISTS trg_users_auth_cache_invalidation_upd;
CREATE TRIGGER trg_users_auth_cache_invalidation_upd
AFTER UPDATE ON users
FOR EACH ROW
BEGIN
    IF NOT ((OLD.status <=> NEW.status)
        AND (OLD.role <=> NEW.role)
        AND (OLD.deleted_at <=> NEW.deleted_at)) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT LOWER(SHA2(k.`key`, 256))
        FROM api_keys AS k
        WHERE k.user_id = OLD.id AND k.deleted_at IS NULL AND k.`key` <> '';
    END IF;
END;

DROP TRIGGER IF EXISTS trg_users_auth_cache_invalidation_del;
CREATE TRIGGER trg_users_auth_cache_invalidation_del
AFTER DELETE ON users
FOR EACH ROW
BEGIN
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT LOWER(SHA2(k.`key`, 256))
    FROM api_keys AS k
    WHERE k.user_id = OLD.id AND k.deleted_at IS NULL AND k.`key` <> '';
END;

-- groups: UPDATE 与 DELETE。
DROP TRIGGER IF EXISTS trg_groups_auth_cache_invalidation_upd;
CREATE TRIGGER trg_groups_auth_cache_invalidation_upd
AFTER UPDATE ON `groups`
FOR EACH ROW
BEGIN
    IF NOT ((OLD.status <=> NEW.status)
        AND (OLD.is_exclusive <=> NEW.is_exclusive)
        AND (OLD.deleted_at <=> NEW.deleted_at)) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT LOWER(SHA2(k.`key`, 256))
        FROM api_keys AS k
        WHERE k.group_id = OLD.id AND k.deleted_at IS NULL AND k.`key` <> '';
    END IF;
END;

DROP TRIGGER IF EXISTS trg_groups_auth_cache_invalidation_del;
CREATE TRIGGER trg_groups_auth_cache_invalidation_del
AFTER DELETE ON `groups`
FOR EACH ROW
BEGIN
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT LOWER(SHA2(k.`key`, 256))
    FROM api_keys AS k
    WHERE k.group_id = OLD.id AND k.deleted_at IS NULL AND k.`key` <> '';
END;

-- user_allowed_groups: INSERT / UPDATE / DELETE 各一个（仅当关联 group 为独占分组时入队）。
DROP TRIGGER IF EXISTS trg_uag_auth_cache_invalidation_ins;
CREATE TRIGGER trg_uag_auth_cache_invalidation_ins
AFTER INSERT ON user_allowed_groups
FOR EACH ROW
BEGIN
    IF EXISTS (SELECT 1 FROM `groups` g WHERE g.id = NEW.group_id AND g.is_exclusive = TRUE) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT LOWER(SHA2(k.`key`, 256))
        FROM api_keys AS k
        WHERE k.user_id = NEW.user_id AND k.group_id = NEW.group_id AND k.deleted_at IS NULL AND k.`key` <> '';
    END IF;
END;

DROP TRIGGER IF EXISTS trg_uag_auth_cache_invalidation_del;
CREATE TRIGGER trg_uag_auth_cache_invalidation_del
AFTER DELETE ON user_allowed_groups
FOR EACH ROW
BEGIN
    IF EXISTS (SELECT 1 FROM `groups` g WHERE g.id = OLD.group_id AND g.is_exclusive = TRUE) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT LOWER(SHA2(k.`key`, 256))
        FROM api_keys AS k
        WHERE k.user_id = OLD.user_id AND k.group_id = OLD.group_id AND k.deleted_at IS NULL AND k.`key` <> '';
    END IF;
END;

DROP TRIGGER IF EXISTS trg_uag_auth_cache_invalidation_upd;
CREATE TRIGGER trg_uag_auth_cache_invalidation_upd
AFTER UPDATE ON user_allowed_groups
FOR EACH ROW
BEGIN
    IF NOT (OLD.user_id <=> NEW.user_id) OR NOT (OLD.group_id <=> NEW.group_id) THEN
        -- 旧组合失效
        IF EXISTS (SELECT 1 FROM `groups` g WHERE g.id = OLD.group_id AND g.is_exclusive = TRUE) THEN
            INSERT INTO auth_cache_invalidation_outbox (cache_key)
            SELECT LOWER(SHA2(k.`key`, 256))
            FROM api_keys AS k
            WHERE k.user_id = OLD.user_id AND k.group_id = OLD.group_id AND k.deleted_at IS NULL AND k.`key` <> '';
        END IF;
        -- 新组合失效
        IF EXISTS (SELECT 1 FROM `groups` g WHERE g.id = NEW.group_id AND g.is_exclusive = TRUE) THEN
            INSERT INTO auth_cache_invalidation_outbox (cache_key)
            SELECT LOWER(SHA2(k.`key`, 256))
            FROM api_keys AS k
            WHERE k.user_id = NEW.user_id AND k.group_id = NEW.group_id AND k.deleted_at IS NULL AND k.`key` <> '';
        END IF;
    END IF;
END;

-- 表注释（MariaDB COMMENT ON 已禁用）：
-- auth_cache_invalidation_outbox: Durable cross-instance auth cache invalidations; cache_key is SHA-256 hex.
