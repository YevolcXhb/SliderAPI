-- Legacy user_external_identities safety reports (PostgreSQL-only).
-- The legacy table is not created by any canonical migration, so this backfill
-- is skipped on MariaDB (matching PG's to_regclass guard).
SELECT 1;

ALTER TABLE auth_identities
    ADD CONSTRAINT auth_identities_metadata_is_object_check
    CHECK (JSON_TYPE(metadata) = 'object');

ALTER TABLE auth_identity_channels
    ADD CONSTRAINT auth_identity_channels_metadata_is_object_check
    CHECK (JSON_TYPE(metadata) = 'object');

ALTER TABLE auth_identity_migration_reports
    ADD CONSTRAINT auth_identity_migration_reports_details_is_object_check
    CHECK (JSON_TYPE(details) = 'object');

