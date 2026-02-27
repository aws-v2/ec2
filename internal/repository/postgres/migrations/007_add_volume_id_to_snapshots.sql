-- internal/repository/postgres/migrations/007_add_volume_id_to_snapshots.sql

ALTER TABLE snapshots ADD COLUMN IF NOT EXISTS volume_id INTEGER REFERENCES volumes(id) ON DELETE CASCADE;
ALTER TABLE snapshots ALTER COLUMN instance_id DROP NOT NULL;
CREATE INDEX IF NOT EXISTS idx_snapshots_volume_id ON snapshots(volume_id);
