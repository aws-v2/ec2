-- internal/repository/postgres/migrations/006_add_instance_tags.sql

CREATE TABLE IF NOT EXISTS instance_tags (
    instance_id VARCHAR(20) NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    key VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (instance_id, key)
);

CREATE INDEX IF NOT EXISTS idx_instance_tags_instance_id ON instance_tags(instance_id);
