-- usage_logs monthly partition bootstrap (PostgreSQL-only).
-- MariaDB does not use the PG declarative partition bootstrap; the base schema
-- keeps usage_logs unpartitioned (same as the PG default path). If partitioning
-- is ever needed, apply a dedicated MariaDB partition plan.
SELECT 1;
