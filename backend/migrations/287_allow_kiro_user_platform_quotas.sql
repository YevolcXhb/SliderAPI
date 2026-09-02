-- 允许用户平台维度配额记录 Kiro。
--
-- 部分部署分支没有 user_platform_quotas 表；全新库迁移时需要兼容缺表场景。
-- 如果该表存在，则扩展平台 check 以允许 platform='kiro'。

-- [MariaDB] PL/pgSQL DO block removed