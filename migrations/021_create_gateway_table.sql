CREATE TABLE IF NOT EXISTS gateway_ports(
    gateway_id TEXT NOT NULL,   -- also the hostid
    port INTEGER NOT NULL,
    vm_id TEXT NULL DEFAULT '',
    status  TEXT,
    PRIMARY KEY (gateway_id,port)
);



 
-- INSERT INTO gateway_ports(gateway_id, port,status) VALUES('gateway-vps-1', generate_series(40000, 40100), 'AVAILABLE');