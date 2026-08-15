-- Tighten OpenAI owned-account uniqueness: chatgpt_account_id is unique only
-- when chatgpt_user_id is absent (user identity takes precedence).

DROP INDEX IF EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq ON accounts;

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS owned_openai_chatgpt_account_id_no_user_key VARCHAR(255);

-- MariaDB cannot index generated columns that reference foreign-key columns;
-- uniqueness is enforced by the application layer.
CREATE INDEX IF NOT EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq
    ON accounts (owner_user_id, owned_openai_chatgpt_account_id_no_user_key);
