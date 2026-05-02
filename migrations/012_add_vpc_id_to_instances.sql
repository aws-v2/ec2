-- Add vpc_id to instances table with default to avoid scan errors
ALTER TABLE instances ADD COLUMN IF NOT EXISTS vpc_id VARCHAR(255) DEFAULT '' NOT NULL;

-- Update any existing NULLs just in case (though DEFAULT NOT NULL should handle it for new columns)
UPDATE instances SET vpc_id = '' WHERE vpc_id IS NULL;

-- Why this is being implemented: to allow EC2 to know each instance’s default VPC, 
-- enabling consistent VPC assignments for other services (RDS, multi-instance setups) 
-- in future phases.
