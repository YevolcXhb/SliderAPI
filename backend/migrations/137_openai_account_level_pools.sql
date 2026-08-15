ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS account_level VARCHAR(20) NOT NULL DEFAULT 'unknown';

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS required_account_level VARCHAR(20) NOT NULL DEFAULT '';

ALTER TABLE accounts
    ADD CONSTRAINT accounts_account_level_check
    CHECK (account_level IN ('unknown', 'free', 'plus', 'pro'));

ALTER TABLE groups
    ADD CONSTRAINT groups_required_account_level_check
    CHECK (required_account_level IN ('', 'free', 'plus', 'pro'));


CREATE INDEX IF NOT EXISTS idx_accounts_account_level
    ON accounts (account_level);

CREATE INDEX IF NOT EXISTS idx_accounts_platform_account_level
    ON accounts (platform, account_level);

CREATE INDEX IF NOT EXISTS idx_groups_required_account_level
    ON groups (required_account_level);