-- MariaDB: drop the implicit unique indexes created by inline UNIQUE column
-- constraints in migration 001. Migration 016 replaced these with generated
-- columns so soft-deleted rows do not occupy the unique slot; the old indexes
-- were not dropped on MariaDB because their auto-generated names differ from
-- the PostgreSQL names used there.
DROP INDEX IF EXISTS email ON users;
DROP INDEX IF EXISTS name ON groups;
