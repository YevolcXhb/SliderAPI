-- 020_add_temp_unschedulable.sql
-- 添加临时不可调度功能相关字段

-- 添加临时不可调度状态解除时间字段
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS temp_unschedulable_until DATETIME(6);

-- 添加临时不可调度原因字段（用于排障和审计）
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS temp_unschedulable_reason text;

-- 添加索引以优化调度查询性能
CREATE INDEX IF NOT EXISTS idx_accounts_temp_unschedulable_until ON accounts(temp_unschedulable_until);  -- [MariaDB] 去掉部分索引 WHERE

-- 添加注释说明字段用途
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN accounts.temp_unschedulable_until IS '临时不可调度状态解除时间，当触发临时不可调度规则时设置（基于错误码或错误描述关键词）';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN accounts.temp_unschedulable_reason IS '临时不可调度原因，记录触发临时不可调度的具体原因（用于排障和审计）';
