-- internal/repository/postgres/migrations/004_storage_enhancements.sql

-- Add storage related columns to instances table with non-null defaults
ALTER TABLE instances ADD COLUMN IF NOT EXISTS root_volume_id VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN IF NOT EXISTS storage_size INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS storage_type VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN IF NOT EXISTS device_name VARCHAR(50) NOT NULL DEFAULT '';

-- Ensure existing NULL values (if any) are cleared
UPDATE instances SET root_volume_id = '' WHERE root_volume_id IS NULL;
UPDATE instances SET storage_size = 0 WHERE storage_size IS NULL;
UPDATE instances SET storage_type = '' WHERE storage_type IS NULL;
UPDATE instances SET device_name = '' WHERE device_name IS NULL;

-- Correct columns if they were created without NOT NULL in a previous run
ALTER TABLE instances ALTER COLUMN root_volume_id SET DEFAULT '';
ALTER TABLE instances ALTER COLUMN root_volume_id SET NOT NULL;
ALTER TABLE instances ALTER COLUMN storage_size SET DEFAULT 0;
ALTER TABLE instances ALTER COLUMN storage_size SET NOT NULL;
ALTER TABLE instances ALTER COLUMN storage_type SET DEFAULT '';
ALTER TABLE instances ALTER COLUMN storage_type SET NOT NULL;
ALTER TABLE instances ALTER COLUMN device_name SET DEFAULT '';
ALTER TABLE instances ALTER COLUMN device_name SET NOT NULL;
