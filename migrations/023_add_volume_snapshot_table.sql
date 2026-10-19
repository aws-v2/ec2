CREATE TABLE IF NOT EXISTS volume_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    volume_id INTEGER NOT NULL,

    name VARCHAR(255) NOT NULL,
    description TEXT,

    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',

    size BIGINT NOT NULL,

    snapshot_path TEXT,

    sha256 VARCHAR(64),

    storage_key TEXT,

    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_volume_snapshots_volume
        FOREIGN KEY (volume_id)
        REFERENCES volumes(id)
        ON DELETE CASCADE
);








CREATE TABLE IF NOT EXISTS golden_images (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    description         TEXT,
    user_id             TEXT NOT NULL,
    source_instance_id  TEXT,
    host_id             TEXT,
    status              TEXT NOT NULL DEFAULT 'pending',
    no_reboot           BOOLEAN NOT NULL DEFAULT FALSE,
    consistency         TEXT,
    disk_path           TEXT,
    size_bytes          BIGINT DEFAULT 0,
    sha256              TEXT,
    error_message       TEXT,
    created_at          TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_images_user_id ON golden_images (user_id);
CREATE INDEX IF NOT EXISTS idx_images_source_instance_id ON golden_images (source_instance_id);
CREATE INDEX IF NOT EXISTS idx_images_status ON golden_images (status);
CREATE INDEX IF NOT EXISTS idx_images_deleted_at ON golden_images (deleted_at);




  ALTER TABLE golden_images
      ALTER COLUMN description   SET DEFAULT '',
      ALTER COLUMN host_id       SET DEFAULT '',
      ALTER COLUMN consistency   SET DEFAULT '',
      ALTER COLUMN disk_path     SET DEFAULT '',
      ALTER COLUMN sha256        SET DEFAULT '',
      ALTER COLUMN error_message SET DEFAULT '';

  UPDATE golden_images SET
      description   = COALESCE(description, ''),
      host_id       = COALESCE(host_id, ''),
      consistency   = COALESCE(consistency, ''),
      disk_path     = COALESCE(disk_path, ''),
      sha256        = COALESCE(sha256, ''),
      error_message = COALESCE(error_message, '');

  ALTER TABLE golden_images
      ALTER COLUMN description   SET NOT NULL,
      ALTER COLUMN host_id       SET NOT NULL,
      ALTER COLUMN consistency   SET NOT NULL,
      ALTER COLUMN disk_path     SET NOT NULL,
      ALTER COLUMN sha256        SET NOT NULL,
      ALTER COLUMN error_message SET NOT NULL;