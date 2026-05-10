-- migration 015: Add ssh_user column to hosts table
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS ssh_user TEXT;
