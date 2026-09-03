-- 295_enlarge_settings_value_to_longtext.sql
-- Upgrade settings.value from TEXT (64KB cap) to LONGTEXT (4GB cap).
--
-- Site logo and similar fields are stored as base64 data URLs in
-- settings.value. A base64-encoded image easily exceeds the 64KB TEXT
-- limit, causing "Data too long for column" -> 500 internal error on save.
--
-- Idempotent: ALTER TABLE ... MODIFY COLUMN is a no-op when the column is
-- already the target type (MariaDB does not error).
ALTER TABLE settings MODIFY COLUMN value LONGTEXT NOT NULL;
