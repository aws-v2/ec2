-- internal/repository/postgres/migrations/011_add_template_config_fields.sql

ALTER TABLE templates ADD COLUMN IF NOT EXISTS image VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS cpu INTEGER NOT NULL DEFAULT 0;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS ram INTEGER NOT NULL DEFAULT 0;
