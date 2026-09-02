-- Persist the client-requested reasoning effort before group policy rewriting
-- and model-family remapping (e.g. max -> xhigh). NULL means historical rows
-- written before this dual-write, or requests that never declared an effort.
--
-- Nullable with no default so the change stays cheap on a large usage_logs table.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS requested_reasoning_effort VARCHAR(20);
