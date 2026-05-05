-- Add vpc_id to instances table with default to avoid scan errors
ALTER TABLE instances ADD COLUMN IF NOT EXISTS vpc_id VARCHAR(255) DEFAULT '' NOT NULL;

-- Update any existing NULLs just in case (though DEFAULT NOT NULL should handle it for new columns)
UPDATE instances SET vpc_id = '' WHERE vpc_id IS NULL;

-- Why this is being implemented: to allow EC2 to know each instance’s default VPC, 
-- enabling consistent VPC assignments for other services (RDS, multi-instance setups) 
-- in future phases.




-- Migration: create VPC and network allocation tables
-- Run this on the ec2-api database

CREATE TABLE IF NOT EXISTS vpcs (
    id          VARCHAR(36)  PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,
    cidr_block  VARCHAR(18)  NOT NULL UNIQUE,   -- enforces no CIDR collisions at DB level
    bridge_name VARCHAR(15)  NOT NULL UNIQUE,   -- Linux interface name limit
    gateway_ip  VARCHAR(15)  NOT NULL,
    tenant_id   VARCHAR(36)  NOT NULL,
    status      VARCHAR(20)  NOT NULL DEFAULT 'pending',
    is_default  BOOLEAN      NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Only one default VPC per tenant
CREATE UNIQUE INDEX IF NOT EXISTS idx_vpcs_tenant_default
    ON vpcs (tenant_id)
    WHERE is_default = true AND status != 'deleted';

CREATE INDEX IF NOT EXISTS idx_vpcs_tenant_id ON vpcs (tenant_id);

-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS network_allocations (
    id           VARCHAR(36) PRIMARY KEY,
    vpc_id       VARCHAR(36) NOT NULL REFERENCES vpcs(id),
    instance_id  VARCHAR(36) NOT NULL UNIQUE,   -- one IP per instance
    ip_address   VARCHAR(15) NOT NULL,
    allocated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_net_alloc_vpc_id      ON network_allocations (vpc_id);
CREATE INDEX IF NOT EXISTS idx_net_alloc_instance_id ON network_allocations (instance_id);

-- Ensure no two instances in the same VPC share an IP
CREATE UNIQUE INDEX IF NOT EXISTS idx_net_alloc_vpc_ip
    ON network_allocations (vpc_id, ip_address);