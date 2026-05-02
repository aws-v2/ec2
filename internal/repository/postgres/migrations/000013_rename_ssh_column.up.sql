-- Rename existing column
ALTER TABLE instances
RENAME COLUMN ssh TO public_sshkey;

-- Add new column
ALTER TABLE instances
ADD COLUMN private_sshkey TEXT;