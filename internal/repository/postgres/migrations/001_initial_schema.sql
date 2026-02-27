-- internal/repository/postgres/migrations/001_initial_schema.sql

CREATE TABLE IF NOT EXISTS instances (
    id VARCHAR(20) PRIMARY KEY,
    vm_name VARCHAR(100) NOT NULL,
    image VARCHAR(100) NOT NULL,
    cpu INTEGER NOT NULL,
    ram INTEGER NOT NULL,
    ssh_key TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,
    ip VARCHAR(45),
    public_ip VARCHAR(100),
    proxmox_id INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS volumes (
    id SERIAL PRIMARY KEY,
    volume_name VARCHAR(100) NOT NULL,
    size INTEGER NOT NULL,
    format VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL,
    attached_to VARCHAR(20),
    device_path TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS snapshots (
    id SERIAL PRIMARY KEY,
    instance_id VARCHAR(20) NOT NULL REFERENCES instances(id),
    name VARCHAR(100) NOT NULL,
    description TEXT,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ssh_keys (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    public_key TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ip_allocations (
    id SERIAL PRIMARY KEY,
    instance_id VARCHAR(20) REFERENCES instances(id),
    public_ip VARCHAR(45) NOT NULL,
    private_ip VARCHAR(45),
    port_mappings TEXT, -- JSON or comma-separated
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS security_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    rules TEXT, -- JSON representation of rules
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS templates (
    id SERIAL PRIMARY KEY,
    instance_id VARCHAR(20) NOT NULL REFERENCES instances(id),
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_instances_status ON instances(status);
CREATE INDEX IF NOT EXISTS idx_instances_created_at ON instances(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_volumes_status ON volumes(status);
CREATE INDEX IF NOT EXISTS idx_snapshots_instance_id ON snapshots(instance_id);
CREATE INDEX IF NOT EXISTS idx_ssh_keys_name ON ssh_keys(name);
CREATE INDEX IF NOT EXISTS idx_ip_allocations_instance_id ON ip_allocations(instance_id);
CREATE INDEX IF NOT EXISTS idx_security_groups_name ON security_groups(name);
CREATE INDEX IF NOT EXISTS idx_templates_name ON templates(name);