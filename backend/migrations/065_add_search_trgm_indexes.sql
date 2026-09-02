-- 为模糊搜索建立索引。
-- [MariaDB 重写] PostgreSQL 使用 pg_trgm + GIN 三元组索引做子串模糊匹配；
-- MariaDB 无 pg_trgm，改用 InnoDB FULLTEXT 索引（MATCH ... AGAINST）。
-- FULLTEXT 与 trigram 语义不同（按词而非任意子串），但同为"模糊检索加速"用途，
-- 应用层的 LIKE '%x%' 兜底查询仍可用；此处仅为常见前缀/词匹配加速。
-- 若目标列已存在同名 FULLTEXT 索引，IF NOT EXISTS 保证幂等。

CREATE FULLTEXT INDEX IF NOT EXISTS idx_users_email_ft ON users (email);
CREATE FULLTEXT INDEX IF NOT EXISTS idx_users_username_ft ON users (username);
CREATE FULLTEXT INDEX IF NOT EXISTS idx_users_notes_ft ON users (notes);

CREATE FULLTEXT INDEX IF NOT EXISTS idx_accounts_name_ft ON accounts (name);

CREATE FULLTEXT INDEX IF NOT EXISTS idx_api_keys_key_ft ON api_keys (`key`);
CREATE FULLTEXT INDEX IF NOT EXISTS idx_api_keys_name_ft ON api_keys (name);
