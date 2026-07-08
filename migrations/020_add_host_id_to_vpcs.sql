-- Add host_id to vpcs table to track which compute host owns a VPC's network
-- This enables VPC affinity: new VMs are preferentially scheduled on the same
-- host as the tenant's existing VPC, avoiding unnecessary network duplication.

ALTER TABLE vpcs ADD COLUMN IF NOT EXISTS host_id TEXT NOT NULL DEFAULT '';



--- create the database auth_db2,iam_db2