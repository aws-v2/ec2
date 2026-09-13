CREATE TABLE IF NOT EXISTS gateway_ports(
    gateway_id TEXT NOT NULL,   -- also the hostid
    port INTEGER NOT NULL,
    vm_id TEXT NULL DEFAULT '',
    status  TEXT,
    PRIMARY KEY (gateway_id,port)
);

ALTER TABLE instances ADD COLUMN IF NOT EXISTS public_port VARCHAR(50) NOT NULL DEFAULT '';

 
-- INSERT INTO gateway_ports(gateway_id, port,status) VALUES('18:60:24:4f:4a:13:root:6d617274696e', generate_series(40000, 40100), 'AVAILABLE');






-- for vm in sagemaker-vm-a90dfc4c; do     echo "Removing $vm";     virsh destroy sagemaker-vm-a90dfc4c  2>/dev/null;     virsh undefine sagemaker-vm-a90dfc4c --remove-all-storage --nvram; done
