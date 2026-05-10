-- migration 017: Add ssh_private_key column to hosts table
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS ssh_private_key TEXT;
