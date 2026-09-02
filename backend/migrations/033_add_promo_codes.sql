-- 创建注册优惠码表
CREATE TABLE IF NOT EXISTS promo_codes (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(32) NOT NULL UNIQUE,
    bonus_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    max_uses INT NOT NULL DEFAULT 0,
    used_count INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    expires_at DATETIME(6) DEFAULT NULL,
    notes TEXT DEFAULT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

-- 创建优惠码使用记录表
CREATE TABLE IF NOT EXISTS promo_code_usages (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    promo_code_id BIGINT NOT NULL REFERENCES promo_codes(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bonus_amount DECIMAL(20,8) NOT NULL,
    used_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE(promo_code_id, user_id)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_promo_codes_status ON promo_codes(status);
CREATE INDEX IF NOT EXISTS idx_promo_codes_expires_at ON promo_codes(expires_at);
CREATE INDEX IF NOT EXISTS idx_promo_code_usages_promo_code_id ON promo_code_usages(promo_code_id);
CREATE INDEX IF NOT EXISTS idx_promo_code_usages_user_id ON promo_code_usages(user_id);

-- [MariaDB: COMMENT ON 已禁用] COMMENT ON TABLE promo_codes IS '注册优惠码';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON TABLE promo_code_usages IS '优惠码使用记录';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN promo_codes.max_uses IS '最大使用次数，0表示无限制';
-- [MariaDB: COMMENT ON 已禁用] COMMENT ON COLUMN promo_codes.status IS '状态: active, disabled';
