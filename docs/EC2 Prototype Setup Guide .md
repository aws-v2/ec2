# EC2 Prototype Setup Guide - Complete Reproduction Guide

This guide will help you reproduce the exact working EC2 prototype from scratch.

## Prerequisites

### System Requirements
- Ubuntu 20.04+ or similar Linux distribution
- At least 4GB RAM
- 20GB free disk space

### Install Required Packages

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install KVM/QEMU/Libvirt
sudo apt install -y qemu-kvm libvirt-daemon-system libvirt-clients bridge-utils virt-manager

# Install genisoimage for cloud-init ISO creation
sudo apt install -y genisoimage

# Add your user to libvirt group
sudo usermod -aG libvirt $USER
sudo usermod -aG kvm $USER

# Reboot or re-login for group changes to take effect
newgrp libvirt

# Verify installation
sudo virsh list --all
```

### Install PostgreSQL

```bash
# Install PostgreSQL
sudo apt install -y postgresql postgresql-contrib

# Start and enable PostgreSQL
sudo systemctl start postgresql
sudo systemctl enable postgresql

# Create database and user
sudo -u postgres psql << EOF
CREATE DATABASE ec2;
CREATE USER ec2user WITH PASSWORD 'ec2password';
GRANT ALL PRIVILEGES ON DATABASE ec2 TO ec2user;
\c ec2
GRANT ALL ON SCHEMA public TO ec2user;
EOF
```

### Install Go

```bash
# Download and install Go (version 1.21+)
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz

# Add to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify
go version
```

## Setup Libvirt Network

```bash
# Ensure default network is active
sudo virsh net-start default
sudo virsh net-autostart default

# Verify network
sudo virsh net-list --all
sudo virsh net-dumpxml default | grep dhcp
```

## Database Setup

```bash
# Connect to database
sudo -u postgres psql -d ec2

# Create instances table
CREATE TABLE instances (
    id VARCHAR(20) PRIMARY KEY,
    image VARCHAR(100) NOT NULL,
    cpu INTEGER NOT NULL,
    ram INTEGER NOT NULL,
    ssh_key TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,
    ip VARCHAR(45) NOT NULL DEFAULT '',
    public_ip VARCHAR(100) NOT NULL DEFAULT '',
    proxmox_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    vm_name VARCHAR(255) NOT NULL DEFAULT 'unknown'
);

# Verify table
\d instances

# Exit psql
\q
```

## Project Structure

```bash
# Create project directory
mkdir -p ~/ec2-prototype
cd ~/ec2-prototype

# Initialize Go module
go mod init ec2-prototype

# Install dependencies
go get github.com/gin-gonic/gin
go get github.com/jmoiron/sqlx
go get github.com/lib/pq
go get github.com/libvirt/libvirt-go
```

## Code Files

### 1. Domain Models (`domain/instance.go`)

```go
package domain

import "time"

type InstanceStatus string

const (
	StatusRunning    InstanceStatus = "running"
	StatusStopped    InstanceStatus = "stopped"
	StatusTerminated InstanceStatus = "terminated"
)

type Instance struct {
	ID        string         `json:"id" db:"id"`
	VMName    string         `json:"vm_name" db:"vm_name"`
	Image     string         `json:"image" db:"image"`
	CPU       int            `json:"cpu" db:"cpu"`
	RAM       int            `json:"ram" db:"ram"`
	SSHKey    string         `json:"ssh_key" db:"ssh_key"`
	Status    InstanceStatus `json:"status" db:"status"`
	IP        string         `json:"ip" db:"ip"`
	PublicIP  string         `json:"public_ip" db:"public_ip"`
	ProxmoxID int            `json:"proxmox_id" db:"proxmox_id"`
	CreatedAt time.Time      `json:"created_at" db:"created_at"`
}

type CreateInstanceRequest struct {
	Image  string `json:"image" binding:"required"`
	CPU    int    `json:"cpu" binding:"required,min=1,max=32"`
	RAM    int    `json:"ram" binding:"required,min=512,max=65536"`
	SSHKey string `json:"ssh_key" binding:"required"`
}
```

### 2. Libvirt Client (`infrastructure/libvirt_client.go`)

```go
package infrastructure

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/libvirt/libvirt-go"
)

type LibvirtClient struct {
	conn *libvirt.Connect
}

func NewLibvirtClient() (*LibvirtClient, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt: %w", err)
	}
	return &LibvirtClient{conn: conn}, nil
}

func (l *LibvirtClient) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}

func (l *LibvirtClient) EnsureDefaultNetwork() error {
	network, err := l.conn.LookupNetworkByName("default")
	if err != nil {
		return fmt.Errorf("default network not found: %w. Run: virsh net-start default", err)
	}
	defer network.Free()

	active, err := network.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check network status: %w", err)
	}

	if !active {
		if err := network.Create(); err != nil {
			return fmt.Errorf("failed to start default network: %w", err)
		}
	}

	autostart, err := network.GetAutostart()
	if err == nil && !autostart {
		network.SetAutostart(true)
	}

	return nil
}

func (l *LibvirtClient) CreateAndStartVM(vmName, diskPath string, cpu, ram int, sshKey string) (int, string, error) {
	if err := l.EnsureDefaultNetwork(); err != nil {
		return 0, "", fmt.Errorf("network setup failed: %w", err)
	}

	isoPath, cleanupFn, err := l.createCloudInitISO(vmName, sshKey)
	if err != nil {
		return 0, "", fmt.Errorf("failed to create cloud-init ISO: %w", err)
	}
	defer cleanupFn()

	xmlConfig := l.buildVMXML(vmName, diskPath, isoPath, cpu, ram)
	
	domain, err := l.conn.DomainDefineXML(xmlConfig)
	if err != nil {
		return 0, "", fmt.Errorf("failed to define domain: %w", err)
	}

	if err := domain.Create(); err != nil {
		domain.Undefine()
		return 0, "", fmt.Errorf("failed to start VM: %w", err)
	}

	id, err := domain.GetID()
	if err != nil {
		return 0, "", fmt.Errorf("failed to get VM ID: %w", err)
	}

	ip, err := l.waitForVMIP(domain, 60*time.Second)
	if err != nil {
		ip = ""
	}

	return int(id), ip, nil
}

func (l *LibvirtClient) createCloudInitISO(vmName, sshKey string) (string, func(), error) {
	userDataPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-user-data", vmName))
	metaDataPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-meta-data", vmName))
	isoPath := fmt.Sprintf("/var/lib/libvirt/images/%s-cloudinit.iso", vmName)
	
	cleanup := func() {
		os.Remove(userDataPath)
		os.Remove(metaDataPath)
	}

	userData := fmt.Sprintf(`#cloud-config
users:
  - name: ubuntu
    ssh-authorized-keys:
      - %s
    sudo: ['ALL=(ALL) NOPASSWD:ALL']
    shell: /bin/bash
    lock_passwd: true
`, sshKey)
	
	if err := os.WriteFile(userDataPath, []byte(userData), 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write user-data: %w", err)
	}
	
	metaData := fmt.Sprintf(`instance-id: %s
local-hostname: %s
`, vmName, vmName)
	
	if err := os.WriteFile(metaDataPath, []byte(metaData), 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write meta-data: %w", err)
	}
	
	cmd := exec.Command("genisoimage", 
		"-output", isoPath,
		"-volid", "cidata",
		"-joliet", "-rock",
		"-graft-points",
		"user-data="+userDataPath,
		"meta-data="+metaDataPath)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("genisoimage failed: %w\nOutput: %s", err, output)
	}
	
	fmt.Printf("✓ Cloud-init ISO created: %s\n", isoPath)
	
	return isoPath, cleanup, nil
}

func (l *LibvirtClient) buildVMXML(name, diskPath, isoPath string, cpu, ram int) string {
	return fmt.Sprintf(`
<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features>
    <acpi/>
    <apic/>
    <pae/>
  </features>
  <clock offset='utc'>
    <timer name='rtc' tickpolicy='catchup'/>
    <timer name='pit' tickpolicy='delay'/>
    <timer name='hpet' present='no'/>
  </clock>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' cache='writeback'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x04' function='0x0'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='hdc' bus='ide'/>
      <readonly/>
      <address type='drive' controller='0' bus='1' target='0' unit='0'/>
    </disk>
    <controller type='ide' index='0'>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x01' function='0x1'/>
    </controller>
    <interface type='network'>
      <source network='default'/>
      <model type='virtio'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x03' function='0x0'/>
    </interface>
    <serial type='pty'>
      <target type='isa-serial' port='0'>
        <model name='isa-serial'/>
      </target>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
    <graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'>
      <listen type='address' address='127.0.0.1'/>
    </graphics>
    <video>
      <model type='vga' vram='16384' heads='1' primary='yes'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x0'/>
    </video>
    <memballoon model='virtio'>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x05' function='0x0'/>
    </memballoon>
  </devices>
</domain>
`, name, ram, cpu, diskPath, isoPath)
}

func (l *LibvirtClient) waitForVMIP(domain *libvirt.Domain, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	
	for time.Now().Before(deadline) {
		interfaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
		if err == nil {
			for _, iface := range interfaces {
				for _, addr := range iface.Addrs {
					if addr.Type == libvirt.IP_ADDR_TYPE_IPV4 {
						return addr.Addr, nil
					}
				}
			}
		}
		
		time.Sleep(2 * time.Second)
	}
	
	return "", fmt.Errorf("timeout waiting for VM IP address")
}
```

### 3. Repository (`repository/instance_repository.go`)

```go
package repository

import (
	"ec2-prototype/domain"
	"github.com/jmoiron/sqlx"
)

type InstanceRepository interface {
	Create(instance *domain.Instance) error
	FindAll() ([]*domain.Instance, error)
	FindByID(id string) (*domain.Instance, error)
	Update(instance *domain.Instance) error
	Delete(id string) error
}

type instanceRepository struct {
	db *sqlx.DB
}

func NewInstanceRepository(db *sqlx.DB) InstanceRepository {
	return &instanceRepository{db: db}
}

func (r *instanceRepository) Create(instance *domain.Instance) error {
	query := `INSERT INTO instances (id, vm_name, image, cpu, ram, ssh_key, status, ip, public_ip, proxmox_id, created_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := r.db.Exec(query, instance.ID, instance.VMName, instance.Image, instance.CPU, 
		instance.RAM, instance.SSHKey, instance.Status, instance.IP, instance.PublicIP, 
		instance.ProxmoxID, instance.CreatedAt)
	return err
}

func (r *instanceRepository) FindAll() ([]*domain.Instance, error) {
	var instances []*domain.Instance
	query := `SELECT * FROM instances WHERE status != 'terminated' ORDER BY created_at DESC`
	err := r.db.Select(&instances, query)
	return instances, err
}

func (r *instanceRepository) FindByID(id string) (*domain.Instance, error) {
	var instance domain.Instance
	query := `SELECT * FROM instances WHERE id = $1`
	err := r.db.Get(&instance, query, id)
	return &instance, err
}

func (r *instanceRepository) Update(instance *domain.Instance) error {
	query := `UPDATE instances SET status = $1, ip = $2, public_ip = $3 WHERE id = $4`
	_, err := r.db.Exec(query, instance.Status, instance.IP, instance.PublicIP, instance.ID)
	return err
}

func (r *instanceRepository) Delete(id string) error {
	query := `UPDATE instances SET status = 'terminated' WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}
```

### 4. Service (`service/instance_service.go`)

```go
package service

import (
	"crypto/rand"
	"ec2-prototype/domain"
	"ec2-prototype/infrastructure"
	"ec2-prototype/repository"
	"fmt"
	"time"
)

type InstanceService struct {
	repo          repository.InstanceRepository
	libvirtClient *infrastructure.LibvirtClient
}

func NewInstanceService(repo repository.InstanceRepository, libvirtClient *infrastructure.LibvirtClient) *InstanceService {
	return &InstanceService{
		repo:          repo,
		libvirtClient: libvirtClient,
	}
}

func (s *InstanceService) CreateInstance(req *domain.CreateInstanceRequest) (*domain.Instance, error) {
	instanceID := generateInstanceID()
	vmName := fmt.Sprintf("vm-%s", instanceID)
	
	// For now, use a dummy disk path - in production, you'd create/clone a disk
	diskPath := fmt.Sprintf("/var/lib/libvirt/images/%s.qcow2", vmName)
	
	// Create and start VM
	proxmoxID, ip, err := s.libvirtClient.CreateAndStartVM(vmName, diskPath, req.CPU, req.RAM, req.SSHKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}
	
	instance := &domain.Instance{
		ID:        instanceID,
		VMName:    vmName,
		Image:     req.Image,
		CPU:       req.CPU,
		RAM:       req.RAM,
		SSHKey:    req.SSHKey,
		Status:    domain.StatusRunning,
		IP:        ip,
		PublicIP:  fmt.Sprintf("localhost:%d", 2200+proxmoxID),
		ProxmoxID: proxmoxID,
		CreatedAt: time.Now(),
	}
	
	if err := s.repo.Create(instance); err != nil {
		return nil, fmt.Errorf("failed to save instance: %w", err)
	}
	
	return instance, nil
}

func (s *InstanceService) ListInstances() ([]*domain.Instance, error) {
	return s.repo.FindAll()
}

func (s *InstanceService) GetInstance(id string) (*domain.Instance, error) {
	return s.repo.FindByID(id)
}

func generateInstanceID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("i-%x", b)
}
```

### 5. Handler (`api/instance_handler.go`)

```go
package api

import (
	"ec2-prototype/domain"
	"ec2-prototype/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

type InstanceHandler struct {
	service *service.InstanceService
}

func NewInstanceHandler(service *service.InstanceService) *InstanceHandler {
	return &InstanceHandler{service: service}
}

func (h *InstanceHandler) CreateInstance(c *gin.Context) {
	var req domain.CreateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	instance, err := h.service.CreateInstance(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusCreated, instance)
}

func (h *InstanceHandler) ListInstances(c *gin.Context) {
	instances, err := h.service.ListInstances()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, instances)
}

func (h *InstanceHandler) GetInstance(c *gin.Context) {
	id := c.Param("id")
	instance, err := h.service.GetInstance(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Instance not found"})
		return
	}
	
	c.JSON(http.StatusOK, instance)
}
```

### 6. Routes (`api/routes.go`)

```go
package api

import "github.com/gin-gonic/gin"

func SetupRoutes(r *gin.Engine, instanceHandler *InstanceHandler) {
	api := r.Group("/api/v1")
	{
		api.POST("/instances", instanceHandler.CreateInstance)
		api.GET("/instances", instanceHandler.ListInstances)
		api.GET("/instances/:id", instanceHandler.GetInstance)
	}
}
```

### 7. Main (`main.go`)

```go
package main

import (
	"ec2-prototype/api"
	"ec2-prototype/infrastructure"
	"ec2-prototype/repository"
	"ec2-prototype/service"
	"log"

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

	// Initialize layers
	instanceRepo := repository.NewInstanceRepository(db)
	instanceService := service.NewInstanceService(instanceRepo, libvirtClient)
	instanceHandler := api.NewInstanceHandler(instanceService)

	// Setup router
	r := gin.Default()
	api.SetupRoutes(r, instanceHandler)

	// Start server
	log.Println("Starting EC2 Prototype API on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
```

## Generate SSH Key (if you don't have one)

```bash
# Generate SSH key pair
ssh-keygen -t rsa -b 4096 -C "your_email@example.com" -f ~/.ssh/id_rsa -N ""

# View your public key
cat ~/.ssh/id_rsa.pub
```

## Create Base VM Image (Optional - for production)

For now, you can skip this and use a dummy disk path. For production:

```bash
# Download Ubuntu cloud image
cd /var/lib/libvirt/images/
sudo wget https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img

# This will be your base image to clone from
```

## Run the Application

```bash
# From project directory
cd ~/ec2-prototype

# Run the application
go run main.go
```

## Testing

### 1. Create an Instance

```bash
# Get your SSH public key
SSH_KEY=$(cat ~/.ssh/id_rsa.pub)

# Create instance
curl -X POST http://localhost:8080/api/v1/instances \
  -H "Content-Type: application/json" \
  -d "{
    \"image\": \"ubuntu-20.04\",
    \"cpu\": 1,
    \"ram\": 2048,
    \"ssh_key\": \"$SSH_KEY\"
  }"
```

### 2. List Instances

```bash
curl http://localhost:8080/api/v1/instances
```

### 3. Get VM IP and SSH

```bash
# Get VM name from the API response, then:
sudo virsh domifaddr vm-i-XXXXXXXX

# SSH to VM
ssh -i ~/.ssh/id_rsa ubuntu@<IP_ADDRESS>
```

### 4. Verify VM is Working

```bash
# Inside VM
whoami
hostname
ip addr show
```

## Troubleshooting

### VM has no IP
```bash
# Check network
sudo virsh net-list --all
sudo virsh net-start default

# Check VM console
sudo virsh console vm-i-XXXXXXXX
```

### SSH Permission Denied
```bash
# Check authorized_keys in VM
sudo virsh console vm-i-XXXXXXXX
# Login and check: cat /home/ubuntu/.ssh/authorized_keys
```

### Cloud-init not working
```bash
# Verify ISO contents
sudo mount -o loop /var/lib/libvirt/images/vm-i-XXXXXXXX-cloudinit.iso /mnt/test
ls -la /mnt/test/
cat /mnt/test/user-data
sudo umount /mnt/test
```

### Clean up stuck VMs
```bash
# List all VMs
sudo virsh list --all

# Destroy and undefine
sudo virsh destroy vm-i-XXXXXXXX
sudo virsh undefine vm-i-XXXXXXXX
sudo rm /var/lib/libvirt/images/vm-i-XXXXXXXX*
```

## Save This Guide

```bash
# Save to file
cat > ~/ec2-prototype-setup-guide.md << 'EOF'
[Paste entire guide here]
EOF
```

---

**You now have a complete, reproducible EC2 prototype!** 🎉