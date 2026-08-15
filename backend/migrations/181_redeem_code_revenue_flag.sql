ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS count_as_revenue BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_redeem_codes_revenue_used_at
    ON redeem_codes (used_at);