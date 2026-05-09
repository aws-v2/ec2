-- migration 013: Add host based system for VM provisioning

CREATE TABLE IF NOT EXISTS hosts (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    ip TEXT NOT NULL,
    cpu_total INTEGER NOT NULL,
    cpu_used INTEGER NOT NULL DEFAULT 0,
    ram_total INTEGER NOT NULL, -- in MB
    ram_free INTEGER NOT NULL,  -- in MB
    disk_total INTEGER NOT NULL, -- in GB
    disk_free INTEGER NOT NULL,  -- in GB
    status TEXT NOT NULL DEFAULT 'active',
    last_heartbeat TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for fast selection of free resources
CREATE INDEX IF NOT EXISTS idx_hosts_resources ON hosts (ram_free DESC, cpu_used ASC, disk_free DESC);
