-- migration 019: Fix vm_metrics schema
ALTER TABLE vm_metrics ADD COLUMN IF NOT EXISTS name TEXT;
ALTER TABLE vm_metrics ALTER COLUMN id SET DEFAULT gen_random_uuid()::TEXT;
