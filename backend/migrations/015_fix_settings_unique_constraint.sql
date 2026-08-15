-- 015_fix_settings_unique_constraint.sql
-- 修复 settings 表 key 字段缺失的唯一约束
-- 此约束是 ON CONFLICT ("key") DO UPDATE 语句所必需的

-- 检查并添加唯一约束（如果不存在）
-- settings.key already has a column-level UNIQUE index created by 005_schema_parity.sql;
-- no additional constraint is needed on MariaDB.
