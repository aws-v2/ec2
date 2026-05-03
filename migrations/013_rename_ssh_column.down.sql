-- Remove new column safely
ALTER TABLE instances
DROP COLUMN IF EXISTS private_sshkey;
-- Remove new column safely
ALTER TABLE instances DROP COLUMN IF EXISTS ssh;
 
-- Revert rename only if needed
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'instances'
          AND column_name = 'public_sshkey'
    ) THEN
        ALTER TABLE instances
        RENAME COLUMN public_sshkey TO ssh;
    END IF;
END $$;


-- Revert rename only if needed
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'instances'
          AND column_name = 'private_sshkey'
    ) THEN
        ALTER TABLE instances
        RENAME COLUMN private_sshkey TO ssh;
    END IF;
END $$;