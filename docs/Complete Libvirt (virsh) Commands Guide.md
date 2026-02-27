# Complete Libvirt (virsh) Commands Guide

A comprehensive guide to managing VMs with libvirt/virsh commands.

## Table of Contents
1. [Basic VM Management](#basic-vm-management)
2. [VM Information & Monitoring](#vm-information--monitoring)
3. [Network Management](#network-management)
4. [Storage Management](#storage-management)
5. [Console & Display Access](#console--display-access)
6. [Snapshots](#snapshots)
7. [VM Configuration](#vm-configuration)
8. [Troubleshooting Commands](#troubleshooting-commands)

---

## Basic VM Management

### List VMs

```bash
# List all running VMs
sudo virsh list

# List all VMs (running and stopped)
sudo virsh list --all

# List only stopped VMs
sudo virsh list --inactive
```

**Example Output:**
```
 Id   Name            State
-------------------------------
 1    vm-i-08beb905   running
 -    vm-i-1cc0ef6e   shut off
```

### Start a VM

```bash
# Start a VM
sudo virsh start vm-i-08beb905

# Start and connect to console
sudo virsh start vm-i-08beb905 --console
```

### Stop a VM

```bash
# Graceful shutdown (sends ACPI shutdown signal)
sudo virsh shutdown vm-i-08beb905

# Force stop (like pulling the power plug)
sudo virsh destroy vm-i-08beb905

# Reboot VM
sudo virsh reboot vm-i-08beb905
```

**Important:** 
- `shutdown` - Graceful, lets OS shut down properly
- `destroy` - Immediate, doesn't wait for OS to shut down

### Autostart VMs

```bash
# Enable autostart (VM starts on host boot)
sudo virsh autostart vm-i-08beb905

# Disable autostart
sudo virsh autostart --disable vm-i-08beb905

# Check autostart status
sudo virsh dominfo vm-i-08beb905 | grep Autostart
```

### Delete/Remove VMs

```bash
# Stop the VM first
sudo virsh destroy vm-i-08beb905

# Remove VM definition (doesn't delete disk files)
sudo virsh undefine vm-i-08beb905

# Remove VM and its storage
sudo virsh undefine vm-i-08beb905 --remove-all-storage

# Manual cleanup of leftover files
sudo rm /var/lib/libvirt/images/vm-i-08beb905*
```

---

## VM Information & Monitoring

### Get VM Details

```bash
# Basic VM information
sudo virsh dominfo vm-i-08beb905

# Show VM state
sudo virsh domstate vm-i-08beb905

# Get VM ID
sudo virsh domid vm-i-08beb905

# Get VM UUID
sudo virsh domuuid vm-i-08beb905
```

**Example `dominfo` Output:**
```
Id:             1
Name:           vm-i-08beb905
UUID:           abc123...
OS Type:        hvm
State:          running
CPU(s):         1
CPU time:       15.2s
Max memory:     2097152 KiB
Used memory:    2097152 KiB
Persistent:     yes
Autostart:      disable
```

### Network Information

```bash
# Get VM IP address
sudo virsh domifaddr vm-i-08beb905

# Get detailed network interface info
sudo virsh domiflist vm-i-08beb905

# Show all network interfaces with addresses
sudo virsh domifaddr vm-i-08beb905 --source lease
```

**Example `domifaddr` Output:**
```
 Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
 vnet0      52:54:00:b0:df:ce    ipv4         192.168.122.74/24
```

### Resource Usage

```bash
# Show CPU stats
sudo virsh cpu-stats vm-i-08beb905

# Show memory stats
sudo virsh dommemstat vm-i-08beb905

# Show block device (disk) stats
sudo virsh domblkstat vm-i-08beb905 vda

# Show network interface stats
sudo virsh domifstat vm-i-08beb905 vnet0

# Real-time resource monitoring
sudo virt-top
```

### List VM Disks and Devices

```bash
# List all block devices (disks)
sudo virsh domblklist vm-i-08beb905

# Show disk info
sudo virsh domblkinfo vm-i-08beb905 vda
```

---

## Network Management

### List Networks

```bash
# List active networks
sudo virsh net-list

# List all networks (active and inactive)
sudo virsh net-list --all
```

### Manage Networks

```bash
# Start a network
sudo virsh net-start default

# Stop a network
sudo virsh net-destroy default

# Enable network autostart
sudo virsh net-autostart default

# Disable network autostart
sudo virsh net-autostart --disable default
```

### Network Information

```bash
# Show network details
sudo virsh net-info default

# Show network configuration (XML)
sudo virsh net-dumpxml default

# Show DHCP leases
sudo virsh net-dhcp-leases default
```

**Example DHCP leases:**
```
 Expiry Time           MAC address         Protocol   IP address          Hostname   Client ID
-------------------------------------------------------------------------------------------------------
 2025-10-14 10:30:45   52:54:00:b0:df:ce   ipv4       192.168.122.74/24   ubuntu     -
```

### Edit Network Configuration

```bash
# Edit network XML
sudo virsh net-edit default

# Define new network from XML file
sudo virsh net-define /path/to/network.xml

# Undefine (delete) network
sudo virsh net-undefine mynetwork
```

---

## Storage Management

### List Storage Pools

```bash
# List active storage pools
sudo virsh pool-list

# List all storage pools
sudo virsh pool-list --all
```

### Manage Storage Pools

```bash
# Start a storage pool
sudo virsh pool-start default

# Stop a storage pool
sudo virsh pool-destroy default

# Enable pool autostart
sudo virsh pool-autostart default

# Refresh pool (scan for new volumes)
sudo virsh pool-refresh default
```

### Storage Pool Information

```bash
# Show pool details
sudo virsh pool-info default

# Show pool configuration
sudo virsh pool-dumpxml default

# List volumes in a pool
sudo virsh vol-list default
```

### Manage Storage Volumes

```bash
# Create a new volume (disk)
sudo virsh vol-create-as default myvm-disk.qcow2 10G --format qcow2

# Delete a volume
sudo virsh vol-delete myvm-disk.qcow2 default

# Show volume info
sudo virsh vol-info /var/lib/libvirt/images/vm-i-08beb905.qcow2

# Clone a volume
sudo virsh vol-clone --pool default base-image.qcow2 new-vm.qcow2

# Resize a volume
sudo virsh vol-resize vm-disk.qcow2 20G --pool default
```

### Direct Disk Operations with qemu-img

```bash
# Check disk info
qemu-img info /var/lib/libvirt/images/vm-i-08beb905.qcow2

# Create a new disk
qemu-img create -f qcow2 /var/lib/libvirt/images/newdisk.qcow2 20G

# Create disk from backing file (linked clone)
qemu-img create -f qcow2 -F qcow2 -b /var/lib/libvirt/images/base.qcow2 newvm.qcow2

# Resize disk
qemu-img resize /var/lib/libvirt/images/vm.qcow2 +10G

# Convert disk formats
qemu-img convert -f qcow2 -O raw input.qcow2 output.raw

# Check disk for errors
qemu-img check /var/lib/libvirt/images/vm.qcow2
```

---

## Console & Display Access

### Console Access

```bash
# Connect to VM console (serial)
sudo virsh console vm-i-08beb905

# Exit console: Press Ctrl + ]
```

**Tip:** If console doesn't work, the VM might not have a serial console configured.

### VNC Display

```bash
# Get VNC display info
sudo virsh vncdisplay vm-i-08beb905

# Connect with VNC client
vncviewer :0  # If output is :0

# Or use virt-viewer
virt-viewer vm-i-08beb905
```

### Take Screenshot

```bash
# Take screenshot of VM display
sudo virsh screenshot vm-i-08beb905 /tmp/vm-screenshot.ppm

# Convert to common format
convert /tmp/vm-screenshot.ppm /tmp/vm-screenshot.png
```

---

## Snapshots

### Create Snapshots

```bash
# Create snapshot
sudo virsh snapshot-create-as vm-i-08beb905 snapshot1 "My first snapshot"

# Create snapshot with description
sudo virsh snapshot-create-as vm-i-08beb905 \
  --name "before-update" \
  --description "Snapshot before system update"

# Create live snapshot (VM keeps running)
sudo virsh snapshot-create-as vm-i-08beb905 snapshot2 --live
```

### List and View Snapshots

```bash
# List all snapshots
sudo virsh snapshot-list vm-i-08beb905

# Show snapshot details
sudo virsh snapshot-info vm-i-08beb905 snapshot1

# Show current snapshot
sudo virsh snapshot-current vm-i-08beb905
```

### Restore and Manage Snapshots

```bash
# Revert to snapshot
sudo virsh snapshot-revert vm-i-08beb905 snapshot1

# Delete snapshot
sudo virsh snapshot-delete vm-i-08beb905 snapshot1

# Delete all snapshots
sudo virsh snapshot-delete vm-i-08beb905 --snapshotname snapshot1 --children
```

---

## VM Configuration

### View Configuration

```bash
# Dump VM XML configuration
sudo virsh dumpxml vm-i-08beb905

# Save XML to file
sudo virsh dumpxml vm-i-08beb905 > vm-config.xml

# View specific device info
sudo virsh dumpxml vm-i-08beb905 | grep -A 10 "disk"
```

### Edit Configuration

```bash
# Edit VM configuration (opens in editor)
sudo virsh edit vm-i-08beb905

# Define VM from XML file
sudo virsh define vm-config.xml

# Define and start VM
sudo virsh create vm-config.xml
```

### Change VM Resources

```bash
# Set memory (requires VM restart for permanent change)
sudo virsh setmem vm-i-08beb905 2048M --config

# Set max memory
sudo virsh setmaxmem vm-i-08beb905 4096M --config

# Set number of CPUs
sudo virsh setvcpus vm-i-08beb905 2 --config

# Hot-plug CPU (while running)
sudo virsh setvcpus vm-i-08beb905 4 --live

# Hot-plug memory (while running)
sudo virsh setmem vm-i-08beb905 4096M --live
```

**Note:** `--config` changes persistent config (survives reboot), `--live` affects running VM only.

### Attach/Detach Devices

```bash
# Attach disk
sudo virsh attach-disk vm-i-08beb905 \
  /var/lib/libvirt/images/extra-disk.qcow2 \
  vdb --driver qemu --subdriver qcow2

# Detach disk
sudo virsh detach-disk vm-i-08beb905 vdb

# Attach network interface
sudo virsh attach-interface vm-i-08beb905 network default \
  --model virtio --mac 52:54:00:12:34:56

# Detach network interface
sudo virsh detach-interface vm-i-08beb905 network --mac 52:54:00:12:34:56
```

---

## Troubleshooting Commands

### Check Libvirt Status

```bash
# Check libvirtd service status
sudo systemctl status libvirtd

# Restart libvirtd
sudo systemctl restart libvirtd

# Check libvirt version
sudo virsh version

# Test connection
sudo virsh uri
```

### Logs and Debugging

```bash
# View VM logs
sudo tail -f /var/log/libvirt/qemu/vm-i-08beb905.log

# System logs
sudo journalctl -u libvirtd -f

# Enable debug logging
sudo virsh log --level debug
```

### Force Operations

```bash
# Force shutdown (hard reset)
sudo virsh reset vm-i-08beb905

# Suspend VM (pause)
sudo virsh suspend vm-i-08beb905

# Resume VM
sudo virsh resume vm-i-08beb905

# Save VM state to file
sudo virsh save vm-i-08beb905 /tmp/vm-state.img

# Restore VM from saved state
sudo virsh restore /tmp/vm-state.img
```

### Kill Stuck Processes

```bash
# Find virsh console processes
ps aux | grep "virsh console"

# Kill stuck console
sudo pkill -9 -f "virsh console vm-i-08beb905"

# Force undefine (if normal undefine fails)
sudo virsh undefine vm-i-08beb905 --nvram --managed-save
```

### Network Troubleshooting

```bash
# Restart network
sudo virsh net-destroy default
sudo virsh net-start default

# Check bridge interface
ip addr show virbr0

# Check iptables rules
sudo iptables -L -n -v -t nat

# Test connectivity from host
ping 192.168.122.74
```

### Storage Troubleshooting

```bash
# Check disk permissions
ls -lh /var/lib/libvirt/images/

# Fix permissions
sudo chown libvirt-qemu:kvm /var/lib/libvirt/images/*

# Check disk space
df -h /var/lib/libvirt/images/

# Verify disk integrity
qemu-img check /var/lib/libvirt/images/vm.qcow2
```

---

## Quick Reference Cheat Sheet

```bash
# VM Management
sudo virsh list --all                    # List all VMs
sudo virsh start <vm-name>               # Start VM
sudo virsh shutdown <vm-name>            # Graceful shutdown
sudo virsh destroy <vm-name>             # Force stop
sudo virsh undefine <vm-name>            # Delete VM

# VM Info
sudo virsh dominfo <vm-name>             # VM details
sudo virsh domifaddr <vm-name>           # Get IP address
sudo virsh console <vm-name>             # Connect to console

# Network
sudo virsh net-list --all                # List networks
sudo virsh net-start default             # Start network
sudo virsh net-dhcp-leases default       # DHCP leases

# Storage
sudo virsh pool-list --all               # List storage pools
sudo virsh vol-list default              # List volumes
qemu-img info <disk-path>                # Disk information

# Snapshots
sudo virsh snapshot-create-as <vm> <name>  # Create snapshot
sudo virsh snapshot-list <vm>              # List snapshots
sudo virsh snapshot-revert <vm> <name>     # Restore snapshot

# Configuration
sudo virsh dumpxml <vm-name>             # View config
sudo virsh edit <vm-name>                # Edit config
```

---

## Common Use Cases

### 1. Clone a VM

```bash
# Method 1: Using virt-clone
sudo virt-clone --original vm-i-08beb905 \
  --name vm-clone \
  --file /var/lib/libvirt/images/vm-clone.qcow2

# Method 2: Manual clone
sudo virsh dumpxml vm-i-08beb905 > clone.xml
# Edit clone.xml (change name, UUID, MAC address)
qemu-img create -f qcow2 -b base.qcow2 clone.qcow2
sudo virsh define clone.xml
```

### 2. Migrate VM to Another Host

```bash
# Export VM
sudo virsh dumpxml vm-i-08beb905 > vm.xml
sudo cp /var/lib/libvirt/images/vm-i-08beb905.qcow2 /backup/

# On new host
sudo virsh define vm.xml
sudo cp /backup/vm-i-08beb905.qcow2 /var/lib/libvirt/images/
sudo virsh start vm-i-08beb905
```

### 3. Resize VM Disk

```bash
# Stop VM
sudo virsh shutdown vm-i-08beb905

# Resize disk
sudo qemu-img resize /var/lib/libvirt/images/vm-i-08beb905.qcow2 +10G

# Start VM
sudo virsh start vm-i-08beb905

# Inside VM, resize filesystem
sudo growpart /dev/vda 1
sudo resize2fs /dev/vda1
```

---

This guide covers 95% of common libvirt operations. Save it for reference! 📚