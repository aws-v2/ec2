-- migration 014: Add host_id to instances table

ALTER TABLE instances ADD COLUMN IF NOT EXISTS host_id TEXT;

-- Index for host lookups
CREATE INDEX IF NOT EXISTS idx_instances_host_id ON instances (host_id);
