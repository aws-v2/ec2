-- internal/repository/postgres/migrations/005_update_volume_fields.sql

-- Add columns name, type, availability_zone to volumes table
ALTER TABLE volumes ADD COLUMN IF NOT EXISTS name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE volumes ADD COLUMN IF NOT EXISTS type VARCHAR(50) NOT NULL DEFAULT 'gp3';
ALTER TABLE volumes ADD COLUMN IF NOT EXISTS availability_zone VARCHAR(50) NOT NULL DEFAULT 'us-east-1a';

-- Optionally, we can set historical data to be more meaningful if needed.
