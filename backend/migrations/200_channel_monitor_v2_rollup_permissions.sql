-- Runtime aggregator reads/writes the fixed rollup caches.
-- [MariaDB 重写] PostgreSQL 的 "GRANT ... ON TABLE a,b,c TO CURRENT_USER" 多表授权语法
--   MariaDB 不支持；执行迁移的账号本就是库属主/超级用户，对本库所有表已有全部权限，故 no-op。
--   若使用最小权限的独立运行时账号，请在部署脚本里对该账号逐表授权（四张 *_rollup 表）。
DO NULL;
