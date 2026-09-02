-- Add Kiro prompt cache emulation controls to groups.
ALTER TABLE groups
  ADD COLUMN IF NOT EXISTS kiro_cache_emulation_enabled TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS kiro_cache_emulation_ratio DECIMAL(5,4) NOT NULL DEFAULT 1.0;

-- [MariaDB] PL/pgSQL DO block removed