-- [MariaDB migration] Registration alias dedup（existsByEmailAliasWithClient）用去点邮箱形式探测 users。
-- [MariaDB 重写] MariaDB 不支持表达式索引；改用 STORED 生成列 + 普通索引。
-- 应用层对该列做等值/前缀探测即可命中。
ALTER TABLE users
    ADD COLUMN email_dot_stripped VARCHAR(320)
    AS (REPLACE(LOWER(TRIM(email)), '.', '')) STORED;

CREATE INDEX IF NOT EXISTS idx_users_email_dot_stripped
    ON users (email_dot_stripped);
