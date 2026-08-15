-- Add Kiro prompt cache emulation controls to groups.
ALTER TABLE groups
  ADD COLUMN IF NOT EXISTS kiro_cache_emulation_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS kiro_cache_emulation_ratio DECIMAL(5,4) NOT NULL DEFAULT 1.0;

ALTER TABLE groups
    ADD CONSTRAINT groups_kiro_cache_emulation_ratio_range
    CHECK (kiro_cache_emulation_ratio >= 0 AND kiro_cache_emulation_ratio <= 1);
