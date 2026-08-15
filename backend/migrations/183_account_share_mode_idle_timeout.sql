-- account_share_memberships is not created by any canonical migration;
-- on fresh MariaDB installs this migration is a no-op (matches PG's to_regclass guard).
SELECT 1;
