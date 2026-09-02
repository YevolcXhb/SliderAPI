ALTER TABLE shop_card_keys
    ADD COLUMN IF NOT EXISTS card_type VARCHAR(20) NOT NULL DEFAULT 'text',
    ADD COLUMN IF NOT EXISTS storage_provider VARCHAR(20),
    ADD COLUMN IF NOT EXISTS storage_key TEXT,
    ADD COLUMN IF NOT EXISTS original_filename VARCHAR(255),
    ADD COLUMN IF NOT EXISTS content_type VARCHAR(120),
    ADD COLUMN IF NOT EXISTS byte_size INTEGER,
    ADD COLUMN IF NOT EXISTS sha256 VARCHAR(64);

-- [MariaDB] PL/pgSQL DO block removed

CREATE INDEX IF NOT EXISTS idx_shop_card_keys_product_type_status
    ON shop_card_keys(product_id, card_type, status);
