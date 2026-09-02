-- Backfill system-managed Codex fingerprint seeds for enabled OpenAI OAuth accounts.
-- Idempotent: valid canonical seeds are preserved on rerun.
-- [MariaDB 重写] jsonb_set -> JSON_SET；to_jsonb(UUID()::text) -> UUID()（JSON_SET 自动按字符串标量存）；
--   extra->>'k' -> JSON_UNQUOTE(JSON_EXTRACT)；btrim -> TRIM；~ regex -> REGEXP。
UPDATE accounts
SET extra = JSON_SET(
    COALESCE(extra, JSON_OBJECT()),
    '$.codex_fingerprint_seed',
    UUID()
)
WHERE deleted_at IS NULL
  AND platform = 'openai'
  AND type = 'oauth'
  AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.codex_fingerprint_mode')), '') IN ('device', 'session', 'full')
  AND (
      JSON_UNQUOTE(JSON_EXTRACT(extra, '$.codex_fingerprint_seed')) IS NULL
      OR TRIM(JSON_UNQUOTE(JSON_EXTRACT(extra, '$.codex_fingerprint_seed'))) = ''
      OR NOT (
          JSON_UNQUOTE(JSON_EXTRACT(extra, '$.codex_fingerprint_seed')) REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
          AND JSON_UNQUOTE(JSON_EXTRACT(extra, '$.codex_fingerprint_seed')) <> '00000000-0000-0000-0000-000000000000'
      )
  );
