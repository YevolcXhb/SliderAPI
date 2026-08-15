-- Legacy user_external_identities backfill (PostgreSQL-only).
-- The legacy table is not created by any canonical migration, so on fresh
-- MariaDB installs this backfill is a no-op (matching PG's to_regclass guard).
SELECT 1;
