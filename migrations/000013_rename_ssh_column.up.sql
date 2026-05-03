-- Ensure new column exists
ALTER TABLE instances
ADD COLUMN IF NOT EXISTS private_sshkey TEXT;
ALTER TABLE instances
ADD COLUMN IF NOT EXISTS public_sshkey TEXT;

ALTER TABLE instances
DROP COLUMN IF EXISTS ssh;

ALTER TABLE instances DROP COLUMN IF EXISTS ssh;

-- If old column exists under legacy name, rename it safely
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'instances'
          AND column_name = 'ssh'
    ) THEN
        ALTER TABLE instances
        RENAME COLUMN ssh TO public_sshkey;
    END IF;
END $$;