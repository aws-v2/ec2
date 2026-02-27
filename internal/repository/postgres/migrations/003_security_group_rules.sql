-- internal/repository/postgres/migrations/003_security_group_rules.sql

-- Rule types: inbound, outbound
CREATE TABLE IF NOT EXISTS security_group_rules (
    id SERIAL PRIMARY KEY,
    security_group_id INTEGER NOT NULL REFERENCES security_groups(id) ON DELETE CASCADE,
    type VARCHAR(10) NOT NULL, -- 'inbound' or 'outbound'
    protocol VARCHAR(20) NOT NULL, -- 'tcp', 'udp', 'icmp', 'all'
    from_port INTEGER,
    to_port INTEGER,
    source_dest_cidr TEXT, -- source for inbound, destination for outbound
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Many-to-many relationship between instances and security groups
CREATE TABLE IF NOT EXISTS instance_security_groups (
    instance_id VARCHAR(20) NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    security_group_id INTEGER NOT NULL REFERENCES security_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (instance_id, security_group_id)
);

CREATE INDEX IF NOT EXISTS idx_sg_rules_sg_id ON security_group_rules(security_group_id);
CREATE INDEX IF NOT EXISTS idx_isg_instance_id ON instance_security_groups(instance_id);
