-- Fix private groups: set scope and owner_user_id for existing private groups
UPDATE groups
SET scope = 'user_private', owner_user_id = CAST(SUBSTRING(name, 10, INSTR(SUBSTRING(name, 10), '-') - 1) AS UNSIGNED)
WHERE name LIKE 'private-u%-%'
  AND (scope IS NULL OR scope = '' OR scope = 'public');
