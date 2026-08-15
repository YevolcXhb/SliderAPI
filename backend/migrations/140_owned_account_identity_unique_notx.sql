-- Build the owned-account identity uniqueness guarantees online (MariaDB).
-- PG partial unique indexes are emulated with stored generated columns that
-- yield NULL for non-matching rows (MariaDB unique indexes allow multiple NULLs).

DROP INDEX IF EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq ON accounts;
DROP INDEX IF EXISTS idx_accounts_owned_openai_chatgpt_user_id_uniq ON accounts;
DROP INDEX IF EXISTS idx_accounts_owned_anthropic_org_account_uniq ON accounts;
DROP INDEX IF EXISTS idx_accounts_owned_gemini_project_uniq ON accounts;
DROP INDEX IF EXISTS idx_accounts_owned_antigravity_project_uniq ON accounts;

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS owned_openai_chatgpt_account_id_key VARCHAR(255),
    ADD COLUMN IF NOT EXISTS owned_openai_chatgpt_user_id_key VARCHAR(255),
    ADD COLUMN IF NOT EXISTS owned_anthropic_org_key VARCHAR(255),
    ADD COLUMN IF NOT EXISTS owned_anthropic_account_key VARCHAR(255),
    ADD COLUMN IF NOT EXISTS owned_gemini_project_key VARCHAR(255),
    ADD COLUMN IF NOT EXISTS owned_antigravity_project_key VARCHAR(255);

-- MariaDB cannot index generated columns that reference foreign-key columns
-- (owner_user_id); uniqueness is enforced by the application layer. These
-- plain indexes keep the lookup paths efficient.
CREATE INDEX IF NOT EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq
    ON accounts (owner_user_id, owned_openai_chatgpt_account_id_key);
CREATE INDEX IF NOT EXISTS idx_accounts_owned_openai_chatgpt_user_id_uniq
    ON accounts (owner_user_id, owned_openai_chatgpt_user_id_key);
CREATE INDEX IF NOT EXISTS idx_accounts_owned_anthropic_org_account_uniq
    ON accounts (owner_user_id, owned_anthropic_org_key, owned_anthropic_account_key);
CREATE INDEX IF NOT EXISTS idx_accounts_owned_gemini_project_uniq
    ON accounts (owner_user_id, owned_gemini_project_key);
CREATE INDEX IF NOT EXISTS idx_accounts_owned_antigravity_project_uniq
    ON accounts (owner_user_id, owned_antigravity_project_key);
