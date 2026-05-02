-- internal/repository/postgres/migrations/009_add_size_to_snapshots.sql

ALTER TABLE snapshots ADD COLUMN IF NOT EXISTS size INTEGER NOT NULL DEFAULT 0;
