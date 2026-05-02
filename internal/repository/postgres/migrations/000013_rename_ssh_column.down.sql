-- Remove the added column
ALTER TABLE instances
DROP COLUMN private_sshkey;

-- Rename column back
ALTER TABLE instances
RENAME COLUMN public_sshkey TO ssh;