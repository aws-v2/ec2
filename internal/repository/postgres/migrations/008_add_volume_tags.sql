-- internal/repository/postgres/migrations/008_add_volume_tags.sql

CREATE TABLE IF NOT EXISTS volume_tags (
    volume_id INTEGER NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    key VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (volume_id, key)
);

CREATE INDEX IF NOT EXISTS idx_volume_tags_volume_id ON volume_tags(volume_id);
