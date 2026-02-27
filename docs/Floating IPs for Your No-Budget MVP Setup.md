# Dynamic DNS + Port Forwarding - Deep Dive


---

## Part 1: Understanding the Problem

### Your Current Situation

```
Your Home Network:
Router (ISP assigned IP: changes daily)
  └─ Your PC (192.168.1.100)
      └─ VM (192.168.122.111)

❌ Problems:
1. Your home IP changes every day (Dynamic IP)
2. Router blocks incoming connections (NAT/Firewall)
3. VM is in a private network (192.168.122.x)
4. No one can reach your PC/VMs from outside
```

### What We Need to Solve

```
✅ Solution Components:
1. Dynamic DNS → Tracks your changing home IP
2. Port Forwarding (Router) → Opens doors in your router
3. Port Forwarding (iptables) → Routes traffic to VMs
```

---

## Part 2: Dynamic DNS Explained

### What is Dynamic DNS?

Your home internet has a **dynamic IP** that changes:
- Today: `41.90.123.45`
- Tomorrow: `41.90.156.78`
- Next week: `41.90.200.12`

**Dynamic DNS** gives you a permanent name that always points to your current IP.

```
yourname.duckdns.org → Always points to your current home IP
```

### How It Works

```
Step 1: You register yourname.duckdns.org
Step 2: Install client on your PC that checks IP every 5 minutes
Step 3: Client tells DuckDNS: "My IP is now 41.90.123.45"
Step 4: DuckDNS updates: yourname.duckdns.org → 41.90.123.45
Step 5: When IP changes, client updates DuckDNS automatically
```

### Setup Dynamic DNS - Detailed

#### Option 1: DuckDNS (Recommended - Easiest)

**Step 1: Register**
```bash
# Go to: https://www.duckdns.org
# Sign in with Google/GitHub
# Choose a subdomain: "myec2cloud"
# You get: myec2cloud.duckdns.org
# Copy your token: abc123def456...
```

**Step 2: Install Update Script**

```bash
# Create directory
mkdir -p ~/duckdns
cd ~/duckdns

# Create update script
nano duck.sh
```

Paste this (replace YOUR-TOKEN and YOUR-SUBDOMAIN):
```bash
#!/bin/bash

# Your DuckDNS details
TOKEN="abc123def456"  # Your actual token
SUBDOMAIN="myec2cloud"  # Your chosen name

# Update DuckDNS with current IP
echo url="https://www.duckdns.org/update?domains=${SUBDOMAIN}&token=${TOKEN}&ip=" | curl -k -o ~/duckdns/duck.log -K -

# Log the update
echo "Updated at: $(date)" >> ~/duckdns/duck.log
```

```bash
# Make executable
chmod 700 duck.sh

# Test it
./duck.sh

# Check if it worked
cat duck.log
# Should say: OK
```

**Step 3: Auto-Update (Run Every 5 Minutes)**

```bash
# Open crontab
crontab -e

# Add this line at the bottom:
*/5 * * * * ~/duckdns/duck.sh >/dev/null 2>&1
```

**What this does:**
- Every 5 minutes, runs duck.sh
- duck.sh gets your current public IP
- Tells DuckDNS: "myec2cloud.duckdns.org now points to [current IP]"
- If IP changes, DuckDNS automatically updates

**Verify it's working:**
```bash
# Check your domain resolves to your IP
nslookup myec2cloud.duckdns.org

# Should show your public IP
# Compare with:
curl ifconfig.me
# Should match!
```

#### Option 2: No-IP (Alternative)

```bash
# Install No-IP client
cd /usr/local/src
sudo wget http://www.noip.com/client/linux/noip-duc-linux.tar.gz
sudo tar xzf noip-duc-linux.tar.gz
cd noip-2.1.9-1
sudo make
sudo make install

# Configure
sudo /usr/local/bin/noip2 -C

# Start
sudo /usr/local/bin/noip2
```

---

## Part 3: Router Port Forwarding Explained

### What is Port Forwarding?

Your router is like a security guard that blocks all incoming traffic. Port forwarding tells the router:

> "When someone knocks on port 2201, let them in and send them to my PC"

### Visual Example

```
WITHOUT Port Forwarding:
Internet User → Your Router (Port 2201) → ❌ BLOCKED

WITH Port Forwarding:
Internet User → Your Router (Port 2201) → ✅ Forwarded to PC (192.168.1.100:2201)
```

### How to Configure Router Port Forwarding

#### Find Your Router's Admin Page

```bash
# Get your router's IP (usually the gateway)
ip route | grep default

# Common router IPs:
# 192.168.1.1
# 192.168.0.1
# 10.0.0.1

# Open in browser:
http://192.168.1.1
```

#### Login Credentials

Common defaults (check your router's label):
- Username: `admin`, Password: `admin`
- Username: `admin`, Password: `password`
- Username: `admin`, Password: (blank)

#### Find Port Forwarding Section

Look for menu items called:
- **Port Forwarding**
- **Virtual Server**
- **NAT Forwarding**
- **Applications & Gaming**

#### Add Port Forwarding Rules

**Example: TP-Link Router**

```
Service Name: SSH-VM-2201
External Port: 2201
Internal IP: 192.168.1.100  (Your PC's local IP)
Internal Port: 2201
Protocol: TCP
Enable: ✅

Repeat for:
- 2202, 2203, 2204... (for multiple VMs)
- 8080 (for your API)
```

**Example: Netgear Router**

```
Service Name: VM-SSH-Ports
Starting Port: 2200
Ending Port: 2299
Server IP Address: 192.168.1.100
Enable: ✅
```

#### Get Your PC's Local IP

```bash
# Find your PC's IP on local network
ip addr show | grep "inet 192"

# Example output:
# inet 192.168.1.100/24
# This is your "Internal IP" for port forwarding
```

#### Set Static IP (Important!)

Your PC's local IP should NOT change. Configure static IP:

**Method 1: Router DHCP Reservation**
- Go to router admin
- Find "DHCP Reservation" or "Address Reservation"
- Add: MAC address of your PC → Always assign 192.168.1.100

**Method 2: Static IP on Ubuntu**

```bash
# Find your network interface
ip link show

# Edit netplan configuration
sudo nano /etc/netplan/01-netcfg.yaml
```

```yaml
network:
  version: 2
  ethernets:
    enp3s0:  # Your interface name
      dhcp4: no
      addresses:
        - 192.168.1.100/24
      gateway4: 192.168.1.1
      nameservers:
        addresses:
          - 8.8.8.8
          - 8.8.4.4
```

```bash
# Apply
sudo netplan apply
```

#### Test Router Port Forwarding

```bash
# From another network (use your phone's 4G/5G, turn off WiFi)
# Or use online tool: https://www.yougetsignal.com/tools/open-ports/

# Test if port is open
nc -zv myec2cloud.duckdns.org 2201

# Or with telnet
telnet myec2cloud.duckdns.org 2201

# Should connect (might show SSH banner)
```

---

## Part 4: iptables Port Forwarding Explained

### The Double Port Forward Problem

```
Internet → Router (forwards 2201) → Your PC (192.168.1.100:2201)
                                      ↓
                                    VM needs to receive this!
                                    (192.168.122.111:22)
```

Router forwards to your PC, but **PC needs to forward to VM**.

### What is iptables?

iptables is Linux's firewall. It controls:
- What traffic comes in
- What traffic goes out
- Where to send traffic (NAT/forwarding)

### iptables Tables Explained

```
iptables has different "tables" for different jobs:

1. nat table (Network Address Translation)
   - Changes source/destination IPs and ports
   - Used for port forwarding

2. filter table (Firewall rules)
   - Allow or block traffic
   - Default table

3. mangle table (Packet modification)
   - Advanced stuff, we don't need this
```

### iptables Chains Explained

Think of chains as **stages** packets go through:

```
Incoming Packet Journey:

1. PREROUTING (nat table)
   ↓ "Change destination before routing decision"
   ↓ This is where we do port forwarding!
   
2. Routing Decision
   ↓ "Is this for me or someone else?"
   
3. FORWARD (filter table)
   ↓ "Should I allow forwarding to VM?"
   
4. POSTROUTING (nat table)
   ↓ "Change source before sending out"
```

### The Rules We Need

#### Rule 1: DNAT (Destination NAT)

```bash
sudo iptables -t nat -A PREROUTING \
  -p tcp --dport 2201 \
  -j DNAT --to-destination 192.168.122.111:22
```

**What this does:**
```
BEFORE rule:
Packet arrives at PC:
  Destination: 192.168.1.100:2201

AFTER rule:
Packet modified:
  Destination: 192.168.122.111:22
  
Translation: "Traffic to port 2201? Send to VM's SSH port"
```

#### Rule 2: FORWARD (Allow)

```bash
sudo iptables -A FORWARD \
  -p tcp -d 192.168.122.111 --dport 22 \
  -j ACCEPT
```

**What this does:**
```
"Allow forwarding traffic TO VM on port 22"
Without this, packet is dropped by firewall
```

#### Rule 3: FORWARD (Allow return traffic)

```bash
sudo iptables -A FORWARD \
  -p tcp -s 192.168.122.111 --sport 22 \
  -j ACCEPT
```

**What this does:**
```
"Allow return traffic FROM VM back to user"
SSH is bidirectional, needs both directions
```

#### Rule 4: MASQUERADE (For VM internet access)

```bash
sudo iptables -t nat -A POSTROUTING \
  -o enp3s0 -j MASQUERADE
```

**What this does:**
```
When VM makes outbound requests (e.g., apt update):
  VM (192.168.122.111) → Changes to → Your PC's IP (192.168.1.100)
  
This allows VMs to access internet
Without it: VMs can't download anything
```

### Complete iptables Setup Script

```bash
sudo nano /usr/local/bin/setup-vm-forwarding.sh
```

```bash
#!/bin/bash
# VM Port Forwarding Management Script

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
EXTERNAL_INTERFACE="enp3s0"  # Change to your interface (find with: ip link show)

# Enable IP forwarding
enable_forwarding() {
    echo -e "${YELLOW}Enabling IP forwarding...${NC}"
    sudo sysctl -w net.ipv4.ip_forward=1
    echo "net.ipv4.ip_forward=1" | sudo tee -a /etc/sysctl.conf
    echo -e "${GREEN}✓ IP forwarding enabled${NC}"
}

# Setup masquerading for VM internet access
setup_masquerade() {
    echo -e "${YELLOW}Setting up masquerade...${NC}"
    
    # Get external interface
    EXTERNAL_INTERFACE=$(ip route | grep default | awk '{print $5}')
    
    # Check if rule already exists
    if sudo iptables -t nat -C POSTROUTING -o $EXTERNAL_INTERFACE -j MASQUERADE 2>/dev/null; then
        echo -e "${GREEN}✓ Masquerade rule already exists${NC}"
    else
        sudo iptables -t nat -A POSTROUTING -o $EXTERNAL_INTERFACE -j MASQUERADE
        echo -e "${GREEN}✓ Masquerade rule added${NC}"
    fi
}

# Add port forward for a VM
add_forward() {
    local PUBLIC_PORT=$1
    local VM_IP=$2
    local VM_PORT=${3:-22}  # Default to SSH port 22
    
    echo -e "${YELLOW}Adding forward: Port $PUBLIC_PORT → $VM_IP:$VM_PORT${NC}"
    
    # Check if rule already exists
    if sudo iptables -t nat -C PREROUTING -p tcp --dport $PUBLIC_PORT -j DNAT --to-destination $VM_IP:$VM_PORT 2>/dev/null; then
        echo -e "${YELLOW}⚠ Forward rule already exists${NC}"
        return 1
    fi
    
    # Add DNAT rule (change destination)
    sudo iptables -t nat -A PREROUTING -p tcp --dport $PUBLIC_PORT -j DNAT --to-destination $VM_IP:$VM_PORT
    
    # Add FORWARD rules (allow traffic)
    sudo iptables -A FORWARD -p tcp -d $VM_IP --dport $VM_PORT -j ACCEPT
    sudo iptables -A FORWARD -p tcp -s $VM_IP --sport $VM_PORT -j ACCEPT
    
    echo -e "${GREEN}✓ Port forward added${NC}"
    
    # Save rules
    save_rules
}

# Remove port forward for a VM
remove_forward() {
    local PUBLIC_PORT=$1
    local VM_IP=$2
    local VM_PORT=${3:-22}
    
    echo -e "${YELLOW}Removing forward: Port $PUBLIC_PORT → $VM_IP:$VM_PORT${NC}"
    
    # Remove DNAT rule
    sudo iptables -t nat -D PREROUTING -p tcp --dport $PUBLIC_PORT -j DNAT --to-destination $VM_IP:$VM_PORT 2>/dev/null
    
    # Remove FORWARD rules
    sudo iptables -D FORWARD -p tcp -d $VM_IP --dport $VM_PORT -j ACCEPT 2>/dev/null
    sudo iptables -D FORWARD -p tcp -s $VM_IP --sport $VM_PORT -j ACCEPT 2>/dev/null
    
    echo -e "${GREEN}✓ Port forward removed${NC}"
    
    # Save rules
    save_rules
}

# List all port forwards
list_forwards() {
    echo -e "${YELLOW}=== Current Port Forwards ===${NC}"
    echo -e "${YELLOW}NAT Rules:${NC}"
    sudo iptables -t nat -L PREROUTING -n -v --line-numbers | grep DNAT
    echo ""
    echo -e "${YELLOW}FORWARD Rules:${NC}"
    sudo iptables -L FORWARD -n -v --line-numbers | grep ACCEPT
}

# Save iptables rules
save_rules() {
    echo -e "${YELLOW}Saving iptables rules...${NC}"
    sudo mkdir -p /etc/iptables
    sudo iptables-save | sudo tee /etc/iptables/rules.v4 > /dev/null
    echo -e "${GREEN}✓ Rules saved to /etc/iptables/rules.v4${NC}"
}

# Restore iptables rules
restore_rules() {
    if [ -f /etc/iptables/rules.v4 ]; then
        echo -e "${YELLOW}Restoring iptables rules...${NC}"
        sudo iptables-restore < /etc/iptables/rules.v4
        echo -e "${GREEN}✓ Rules restored${NC}"
    else
        echo -e "${RED}✗ No saved rules found${NC}"
    fi
}

# Initialize (run once)
initialize() {
    echo -e "${YELLOW}=== Initializing Port Forwarding System ===${NC}"
    enable_forwarding
    setup_masquerade
    
    # Install iptables-persistent for auto-restore on boot
    echo -e "${YELLOW}Installing iptables-persistent...${NC}"
    sudo apt update
    sudo DEBIAN_FRONTEND=noninteractive apt install -y iptables-persistent
    
    save_rules
    echo -e "${GREEN}=== Initialization Complete ===${NC}"
}

# Show usage
usage() {
    echo "Usage: $0 {init|add|remove|list|save|restore}"
    echo ""
    echo "Commands:"
    echo "  init                     - Initialize forwarding system (run once)"
    echo "  add PORT VM_IP [VM_PORT] - Add port forward"
    echo "  remove PORT VM_IP        - Remove port forward"
    echo "  list                     - List all forwards"
    echo "  save                     - Save current rules"
    echo "  restore                  - Restore saved rules"
    echo ""
    echo "Examples:"
    echo "  $0 init"
    echo "  $0 add 2201 192.168.122.111 22"
    echo "  $0 remove 2201 192.168.122.111"
    echo "  $0 list"
}

# Main
case "$1" in
    init)
        initialize
        ;;
    add)
        if [ -z "$2" ] || [ -z "$3" ]; then
            echo -e "${RED}Error: Missing arguments${NC}"
            usage
            exit 1
        fi
        add_forward "$2" "$3" "$4"
        ;;
    remove)
        if [ -z "$2" ] || [ -z "$3" ]; then
            echo -e "${RED}Error: Missing arguments${NC}"
            usage
            exit 1
        fi
        remove_forward "$2" "$3" "$4"
        ;;
    list)
        list_forwards
        ;;
    save)
        save_rules
        ;;
    restore)
        restore_rules
        ;;
    *)
        usage
        exit 1
        ;;
esac
```

```bash
# Make executable
sudo chmod +x /usr/local/bin/setup-vm-forwarding.sh

# Initialize (run once)
sudo /usr/local/bin/setup-vm-forwarding.sh init

# Test adding a forward
sudo /usr/local/bin/setup-vm-forwarding.sh add 2201 192.168.122.111 22

# List forwards
sudo /usr/local/bin/setup-vm-forwarding.sh list
```

---

## Part 5: Go Integration (Complete Implementation)

### Create Port Manager

```go
// infrastructure/port_manager.go
package infrastructure

import (
	"fmt"
	"os/exec"
	"sync"
)

type PortManager struct {
	mu            sync.Mutex
	allocatedPorts map[int]string // port -> instanceID
	basePort       int
	publicDomain   string
	scriptPath     string
}

func NewPortManager(publicDomain string) *PortManager {
	return &PortManager{
		allocatedPorts: make(map[int]string),
		basePort:       2200,
		publicDomain:   publicDomain,
		scriptPath:     "/usr/local/bin/setup-vm-forwarding.sh",
	}
}

// AllocatePort finds and reserves an available port
func (pm *PortManager) AllocatePort(instanceID string) (int, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Find first available port
	for port := pm.basePort; port < pm.basePort+100; port++ {
		if _, exists := pm.allocatedPorts[port]; !exists {
			pm.allocatedPorts[port] = instanceID
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available ports (all 100 ports in use)")
}

// SetupPortForward creates iptables rules for VM access
func (pm *PortManager) SetupPortForward(publicPort int, vmIP string, vmPort int) error {
	// Call our shell script
	cmd := exec.Command("sudo", pm.scriptPath, "add", 
		fmt.Sprintf("%d", publicPort), 
		vmIP, 
		fmt.Sprintf("%d", vmPort))
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to setup port forward: %w\nOutput: %s", err, output)
	}

	fmt.Printf("Port forward created: %d → %s:%d\n", publicPort, vmIP, vmPort)
	return nil
}

// RemovePortForward removes iptables rules
func (pm *PortManager) RemovePortForward(publicPort int, vmIP string, vmPort int) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Remove from allocated ports
	delete(pm.allocatedPorts, publicPort)

	// Call shell script to remove
	cmd := exec.Command("sudo", pm.scriptPath, "remove",
		fmt.Sprintf("%d", publicPort),
		vmIP,
		fmt.Sprintf("%d", vmPort))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove port forward: %w\nOutput: %s", err, output)
	}

	fmt.Printf("Port forward removed: %d → %s:%d\n", publicPort, vmIP, vmPort)
	return nil
}

// GetPublicAddress returns the full public address for SSH
func (pm *PortManager) GetPublicAddress(port int) string {
	return fmt.Sprintf("%s:%d", pm.publicDomain, port)
}

// GetSSHCommand returns the full SSH command for users
func (pm *PortManager) GetSSHCommand(port int) string {
	return fmt.Sprintf("ssh -i ~/.ssh/key ubuntu@%s -p %d", pm.publicDomain, port)
}

// ReleasePort marks a port as available again
func (pm *PortManager) ReleasePort(instanceID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for port, id := range pm.allocatedPorts {
		if id == instanceID {
			delete(pm.allocatedPorts, port)
			fmt.Printf("Port %d released\n", port)
			return nil
		}
	}

	return fmt.Errorf("instance %s has no allocated port", instanceID)
}

// LoadExistingPorts loads port allocations from database on startup
func (pm *PortManager) LoadExistingPorts(instances []map[string]interface{}) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, inst := range instances {
		instanceID := inst["id"].(string)
		publicIP := inst["public_ip"].(string)
		
		// Parse port from public_ip (format: "domain:port")
		var port int
		fmt.Sscanf(publicIP, "%*[^:]:%d", &port)
		
		if port >= pm.basePort && port < pm.basePort+100 {
			pm.allocatedPorts[port] = instanceID
		}
	}

	fmt.Printf("Loaded %d existing port allocations\n", len(pm.allocatedPorts))
}
```

### Update Service

```go
// service/instance_service.go
package service

import (
	"ec2-prototype/domain"
	"ec2-prototype/infrastructure"
	"ec2-prototype/repository"
	"fmt"
	"os/exec"
	"time"
	"github.com/google/uuid"
)

type InstanceService struct {
	repo          domain.InstanceRepository
	libvirtClient *infrastructure.LibvirtClient
	portManager   *infrastructure.PortManager
}

func NewInstanceService(repo domain.InstanceRepository, libvirtClient *infrastructure.LibvirtClient, publicDomain string) *InstanceService {
	service := &InstanceService{
		repo:          repo,
		libvirtClient: libvirtClient,
		portManager:   infrastructure.NewPortManager(publicDomain),
	}

	// Load existing port allocations on startup
	instances, _ := repo.FindAll()
	instanceData := make([]map[string]interface{}, len(instances))
	for i, inst := range instances {
		instanceData[i] = map[string]interface{}{
			"id":        inst.ID,
			"public_ip": inst.PublicIP,
		}
	}
	service.portManager.LoadExistingPorts(instanceData)

	return service
}

func (s *InstanceService) createVMAsync(instance *domain.Instance, req *domain.CreateInstanceRequest, baseImagePath, newDiskPath string) {
	// TODO: Fire event: vm_creation_async_started
	
	// Clone disk
	// TODO: Fire event: disk_cloning_started
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2",
		"-F", "qcow2", "-b", baseImagePath, newDiskPath)
	if err := cmd.Run(); err != nil {
		// TODO: Fire event: disk_cloning_failed
		fmt.Printf("Failed to clone disk for %s: %v\n", instance.VMName, err)
		instance.Status = domain.StatusTerminated
		s.repo.Update(instance)
		return
	}
	// TODO: Fire event: disk_cloning_completed

	// Create and start VM
	// TODO: Fire event: vm_libvirt_creation_started
	vmID, ip, err := s.libvirtClient.CreateAndStartVM(instance.VMName, newDiskPath, req.CPU, req.RAM, req.SSHKey)
	if err != nil {
		// TODO: Fire event: vm_libvirt_creation_failed
		fmt.Printf("Failed to create VM %s: %v\n", instance.VMName, err)
		exec.Command("rm", "-f", newDiskPath).Run()
		instance.Status = domain.StatusTerminated
		s.repo.Update(instance)
		// TODO: Fire event: instance_terminated
		return
	}
	// TODO: Fire event: vm_started

	// Allocate public port
	// TODO: Fire event: port_allocation_started
	publicPort, err := s.portManager.AllocatePort(instance.ID)
	if err != nil {
		// TODO: Fire event: port_allocation_failed
		fmt.Printf("Failed to allocate port: %v\n", err)
		s.libvirtClient.DeleteVM(vmID)
		exec.Command("rm", "-f", newDiskPath).Run()
		instance.Status = domain.StatusTerminated
		s.repo.Update(instance)
		return
	}
	// TODO: Fire event: port_allocated

	// Setup port forwarding
	// TODO: Fire event: port_forward_setup_started
	if err := s.portManager.SetupPortForward(publicPort, ip, 22); err != nil {
		// TODO: Fire event: port_forward_setup_failed
		fmt.Printf("Failed to setup port forward: %v\n", err)
		s.portManager.ReleasePort(instance.ID)
		s.libvirtClient.DeleteVM(vmID)
		exec.Command("rm", "-f", newDiskPath).Run()
		instance.Status = domain.StatusTerminated
		s.repo.Update(instance)
		return
	}
	// TODO: Fire event: port_forward_setup_completed

	// Update instance with all details
	instance.Status = domain.StatusRunning
	instance.IP = ip
	instance.PublicIP = s.portManager.GetPublicAddress(publicPort)
	instance.ProxmoxID = vmID

	// TODO: Fire event: instance_status_updating
	if err := s.repo.Update(instance); err != nil {
		// TODO: Fire event: instance_db_update_failed
		fmt.Printf("Failed to update instance %s: %v\n", instance.ID, err)
		s.portManager.RemovePortForward(publicPort, ip, 22)
		s.portManager.ReleasePort(instance.ID)
		s.libvirtClient.DeleteVM(vmID)
		exec.Command("rm", "-f", newDiskPath).Run()
		return
	}

	// TODO: Fire event: instance_running
	// TODO: Fire event: vm_creation_completed
	fmt.Printf("✓ VM %s created successfully\n", instance.VMName)
	fmt.Printf("  Internal IP: %s\n", ip)
	fmt.Printf("  Public Access: %s\n", instance.PublicIP)
	fmt.Printf("  SSH Command: %s\n", s.portManager.GetSSHCommand(publicPort))
}

// DeleteInstance with cleanup
func (s *InstanceService) DeleteInstance(instanceID string) error {
	instance, err := s.repo.FindByID(instanceID)
	if err != nil {
		return err
	}

	// Parse port from public_ip
	var port int
	fmt.Sscanf(instance.PublicIP, "%*[^:]:%d", &port)

	// Remove port forward
	if port > 0 {
		s.portManager.RemovePortForward(port, instance.IP, 22)
		s.portManager.ReleasePort(instanceID)
	}

	// Delete VM
	s.libvirtClient.DeleteVM(instance.ProxmoxID)

	// Delete disk
	diskPath := fmt.Sprintf("/var/lib/libvirt/images/%s.qcow2", instance.VMName)
	exec.Command("rm", "-f", diskPath).Run()

	// Update DB
	return s.repo.Delete(instanceID)
}
```

### Update main.go

```go
// main.go
package main

import (
	"ec2-prototype/api"
	"ec2-prototype/infrastructure"
	"ec2-prototype/repository"
	"ec2-prototype/service"
	"log"
	"os"
	"os/exec"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func main() {
	// Database connection
	db, err := sqlx.Connect("postgres", "host=localhost port=5432 user=ec2user password=ec2password dbname=ec2 sslmode=disable")
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Libvirt client
	libvirtClient, err := infrastructure.NewLibvirtClient()
	if err != nil {
		log.Fatal("Failed to connect to libvirt:", err)
	}
	defer libvirtClient.Close()

	// Restore iptables rules on startup
	if _, err := os.Stat("/etc/iptables/rules.v4"); err == nil {
		log.Println("Restoring iptables rules...")
		exec.Command("sudo", "iptables-restore", "/etc/iptables/rules.v4").Run()
	}

	// Initialize layers
	// IMPORTANT: Replace with your actual DuckDNS domain
	publicDomain := "myec2cloud.duckdns.org"
	
	instanceRepo := repository.NewInstanceRepository(db)
	instanceService := service.NewInstanceService(instanceRepo, libvirtClient, publicDomain)
	instanceHandler := api.NewInstanceHandler(instanceService)

	// Setup router
	r := gin.Default()
	api.SetupRoutes(r, instanceHandler)

	// Start server
	log.Printf("Starting EC2 Prototype API on :8080")
	log.Printf("Public domain: %s", publicDomain)
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
```

### Allow sudo without password

```bash
sudo visudo
```

Add this line (replace `your-username`):
```
your-username ALL=(ALL) NOPASSWD: /usr/local/bin/setup-vm-forwarding.sh, /usr/sbin/iptables, /usr/sbin/iptables-save, /usr/sbin/iptables-restore
```

---

## Part 6: Complete Flow (From Cafe to VM)

### The Journey of an SSH Packet

```
Step 1: You're at a cafe (Public WiFi: 10.50.1.100)
        Type: ssh -i key ubuntu@myec2cloud.duckdns.org -p 2201

Step 2: DNS Resolution
        myec2cloud.duckdns.org → Resolves to your home IP (41.90.123.45)

Step 3: Packet sent
        From: 10.50.1.100:random_port
        To: 41.90.123.45:2201

Step 4: Arrives at your home router
        Router checks port forwarding rules
        Rule found: Port 2201 → Forward to 192.168.1.100:2201

Step 5: Arrives at your PC (192.168.1.100:2201)
        iptables PREROUTING rule activates:
        DNAT: Change destination to 192.168.122.111:22

Step 6: iptables FORWARD rule
        Check: Is forwarding to 192.168.122.111:22 allowed?
        Rule found: ACCEPT

Step 7: Packet reaches VM (192.168.122.111:22)
        SSH server on VM receives packet
        Authenticates with your SSH key

Step 8: Return journey (Response)
        From: 192.168.122.111:22
        To: 10.50.1.100:original_port
        
        Goes through:
        - iptables FORWARD (allow return)
        - iptables POSTROUTING (SNAT if needed)
        - Router NAT
        - Internet
        - Back to your laptop at cafe

Step 9: SSH session established! ✅
```

---

## Part 7: Testing Everything

### Test 1: Check Dynamic DNS

```bash
# From your PC
curl ifconfig.me
# Shows: 41.90.123.45

nslookup myec2cloud.duckdns.org
# Should show: 41.90.123.45

# ✅ If they match, DNS is working!
```

### Test 2: Check Router Port Forwarding

```bash
# From phone (disconnect from WiFi, use 4G)
nc -zv myec2cloud.duckdns.org 2201

# Or use online tool:
# Visit: https://www.yougetsignal.com/tools/open-ports/
# Enter: myec2cloud.duckdns.org
# Port: 2201
# Check: ✅ Open
```

### Test 3: Check iptables Rules

```bash
# On your PC, check NAT rules
sudo iptables -t nat -L PREROUTING -n -v

# Should show something like:
# DNAT tcp -- * * 0.0.0.0/0 0.0.0.0/0 tcp dpt:2201 to:192.168.122.111:22

# Check FORWARD rules
sudo iptables -L FORWARD -n -v

# Should show ACCEPT rules for your VM IP
```

### Test 4: Create VM and Connect

```bash
# Create VM
curl -X POST http://localhost:8080/api/v1/instances \
  -H "Content-Type: application/json" \
  -d '{
    "image": "ubuntu-20.04",
    "cpu": 1,
    "ram": 2048,
    "ssh_key": "'$(cat ~/.ssh/id_rsa.pub)'"
  }'

# Wait 30-40 seconds for VM to boot

# Get VM details
curl http://localhost:8080/api/v1/instances | jq .

# Response shows:
# "public_ip": "myec2cloud.duckdns.org:2201"

# From cafe (or phone 4G), connect:
ssh -i ~/.ssh/id_rsa ubuntu@myec2cloud.duckdns.org -p 2201

# ✅ You're in!
```

---

## Part 8: Troubleshooting

### Problem: Can't resolve domain

```bash
# Check DNS
nslookup myec2cloud.duckdns.org

# If fails, check DuckDNS update script
cat ~/duckdns/duck.log

# Should say "OK"
# If not, check your token and subdomain in duck.sh
```

### Problem: Connection timeout

```bash
# Check if your home IP changed
curl ifconfig.me

# Update DuckDNS manually
~/duckdns/duck.sh

# Check router port forwarding is still configured
# Some routers reset on reboot
```

### Problem: Connection refused

```bash
# Check iptables rules exist
sudo iptables -t nat -L PREROUTING -n

# If empty, restore
sudo /usr/local/bin/setup-vm-forwarding.sh restore

# Or re-add manually
sudo /usr/local/bin/setup-vm-forwarding.sh add 2201 192.168.122.111 22
```

### Problem: Can ping but can't SSH

```bash
# Check VM is actually running
sudo virsh list

# Check VM has IP
sudo virsh domifaddr vm-name

# Try connecting from PC first (bypass router)
ssh ubuntu@192.168.122.111

# If works from PC but not externally, issue is with router/iptables
```

### Problem: SSH works once then stops

```bash
# Your home IP probably changed
# Check current IP
curl ifconfig.me

# Check what DNS resolves to
nslookup myec2cloud.duckdns.org

# If different, DuckDNS client might not be running
ps aux | grep duck

# Restart DuckDNS updates
~/duckdns/duck.sh
```

---

## Part 9: Monitoring and Maintenance

### Create monitoring script

```bash
nano ~/monitor-ec2.sh
```

```bash
#!/bin/bash

echo "=== EC2 System Status ==="
echo ""

echo "1. Public IP:"
PUBLIC_IP=$(curl -s ifconfig.me)
echo "   $PUBLIC_IP"

echo ""
echo "2. DuckDNS Resolution:"
DNS_IP=$(nslookup myec2cloud.duckdns.org | grep -A1 "Name:" | tail -1 | awk '{print $2}')
echo "   $DNS_IP"

if [ "$PUBLIC_IP" == "$DNS_IP" ]; then
    echo "   ✅ DNS is up to date"
else
    echo "   ⚠️  DNS is out of date! Updating..."
    ~/duckdns/duck.sh
fi

echo ""
echo "3. Running VMs:"
sudo virsh list --all

echo ""
echo "4. Port Forwards:"
sudo /usr/local/bin/setup-vm-forwarding.sh list

echo ""
echo "5. Internet Connectivity:"
if ping -c 1 8.8.8.8 > /dev/null 2>&1; then
    echo "   ✅ Internet OK"
else
    echo "   ❌ No internet!"
fi
```

```bash
chmod +x ~/monitor-ec2.sh

# Run it
~/monitor-ec2.sh
```

---

## Summary

**What we achieved:**

1. ✅ Dynamic DNS keeps track of your changing home IP
2. ✅ Router forwards incoming traffic to your PC
3. ✅ iptables forwards PC traffic to VMs
4. ✅ You can SSH to VMs from anywhere
5. ✅ All FREE, no cloud costs!

**Key files to remember:**

- `~/duckdns/duck.sh` - Updates your IP
- `/usr/local/bin/setup-vm-forwarding.sh` - Manages port forwards
- `/etc/iptables/rules.v4` - Saved firewall rules
- Router admin page - Port forwarding config

**Next steps:**

1. Set this up and test from a cafe
2. Add more VMs and watch them auto-configure
3. Later: Add web interface for VM management
4. Later: Add monitoring dashboard

Does this make sense now? Any specific part you want me to explain further?