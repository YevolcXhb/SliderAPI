-- Seed a default group for fresh installs.
INSERT INTO groups (name, description, created_at, updated_at)
SELECT 'default', 'Default group', CURRENT_TIMESTAMP(6), CURRENT_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM groups);
