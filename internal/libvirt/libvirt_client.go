package libvirt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"libvirt.org/go/libvirt"
)

type LibvirtClient struct {
	conn      *libvirt.Connect
	imagesDir string
}

func NewLibvirtClient(uri string, imagesDir string) (*LibvirtClient, error) {
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt: %w", err)
	}
	return &LibvirtClient{conn: conn, imagesDir: imagesDir}, nil
}

func (l *LibvirtClient) Close() error {
	_, err := l.conn.Close()
	return err
}

func (l *LibvirtClient) GetImagesDir() string {
	return l.imagesDir
}

// CreateAndStartVM creates and starts a VM with a statically configured IP.
// privateIP and gateway come from the network service — they are pre-allocated
// before this function is called. The VM boots with this IP already configured
// via cloud-init network-config, so waitForVMIP is no longer needed.
func (l *LibvirtClient) CreateAndStartVM(vmName, diskPath string, cpu, ram int, sshKey, bridgeName, privateIP, gateway string) (int, error) {
	if bridgeName == "" {
		if err := l.EnsureDefaultNetwork(); err != nil {
			return 0, fmt.Errorf("network setup failed: %w", err)
		}
	} else {
		fmt.Printf("[Libvirt] Attaching VM %s to bridge %s\n", vmName, bridgeName)
	}

	// Create cloud-init ISO with static network config baked in
	isoPath, cleanupFn, err := l.createCloudInitISO(vmName, sshKey, privateIP, gateway)
	if err != nil {
		return 0, fmt.Errorf("failed to create cloud-init ISO: %w", err)
	}
	defer cleanupFn()

	xmlConfig := l.buildVMXML(vmName, diskPath, isoPath, cpu, ram, bridgeName)

	domain, err := l.conn.DomainDefineXML(xmlConfig)
	if err != nil {
		return 0, fmt.Errorf("failed to define domain: %w", err)
	}

	if err := domain.Create(); err != nil {
		domain.Undefine()
		return 0, fmt.Errorf("failed to start VM: %w", err)
	}

	id, err := domain.GetID()
	if err != nil {
		return 0, fmt.Errorf("failed to get VM ID: %w", err)
	}

	// We no longer wait for DHCP — the IP was pre-allocated and is baked into
	// cloud-init. The VM will boot with it immediately.
	fmt.Printf("[Libvirt] VM %s started with static IP %s (bridge: %s)\n", vmName, privateIP, bridgeName)

	return int(id), nil
}

// createCloudInitISO builds a cloud-init ISO with three files:
//   - user-data  : SSH keys, user setup
//   - meta-data  : instance ID and hostname
//   - network-config : static IP configuration (v2 format)
//
// The network-config file is what tells cloud-init to configure the NIC with
// the pre-allocated IP from the network service instead of using DHCP.

func (l *LibvirtClient) createCloudInitISO(vmName, sshKey, privateIP, gateway string) (string, func(), error) {
	userDataPath   := filepath.Join(os.TempDir(), fmt.Sprintf("%s-user-data", vmName))
	metaDataPath   := filepath.Join(os.TempDir(), fmt.Sprintf("%s-meta-data", vmName))
	networkCfgPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-network-config", vmName))
	absDir, _      := filepath.Abs(l.imagesDir)
	isoPath        := filepath.Join(absDir, fmt.Sprintf("%s-cloudinit.iso", vmName))

	cleanup := func() {
		os.Remove(userDataPath)
		os.Remove(metaDataPath)
		os.Remove(networkCfgPath)
	}

	// ── user-data ─────────────────────────────────────────────────────────────
	keysYaml := ""
	for _, key := range strings.Split(sshKey, "\n") {
		if strings.TrimSpace(key) != "" {
			keysYaml += fmt.Sprintf("      - %s\n", strings.TrimSpace(key))
		}
	}

	if keysYaml == "" {
		fmt.Printf("[Libvirt] [WARN] No SSH keys provided for VM %s\n", vmName)
	}

	userData := fmt.Sprintf(`#cloud-config
ssh_pwauth: true
users:
  - name: ubuntu
    plain_text_passwd: "ubuntu"
    sudo: ['ALL=(ALL) NOPASSWD:ALL']
    shell: /bin/bash
    lock_passwd: false
    ssh-authorized-keys:
%s
`, keysYaml)

	if err := os.WriteFile(userDataPath, []byte(userData), 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write user-data: %w", err)
	}

	// ── meta-data ─────────────────────────────────────────────────────────────
	metaData := fmt.Sprintf(`instance-id: %s
local-hostname: %s
`, vmName, vmName)

	if err := os.WriteFile(metaDataPath, []byte(metaData), 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write meta-data: %w", err)
	}

	// ── network-config ────────────────────────────────────────────────────────
	// Interface name is ens3 — this is the KVM/QEMU default interface name.
	// Previously this was enp1s0 which does not exist in this VM configuration,
	// causing cloud-init to skip network setup entirely and leaving the VM with no IP.
	var networkConfig string
	if privateIP != "" && gateway != "" {
		networkConfig = fmt.Sprintf(`version: 2
ethernets:
  ens3:
    dhcp4: false
    addresses:
      - %s/24
    gateway4: %s
    nameservers:
      addresses:
        - 8.8.8.8
        - 8.8.4.4
`, privateIP, gateway)
	} else {
		networkConfig = `version: 2
ethernets:
  ens3:
    dhcp4: true
`
	}

	if err := os.WriteFile(networkCfgPath, []byte(networkConfig), 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write network-config: %w", err)
	}

	cmd := exec.Command("genisoimage",
		"-output", isoPath,
		"-volid", "cidata",
		"-joliet", "-rock",
		"-graft-points",
		"user-data="+userDataPath,
		"meta-data="+metaDataPath,
		"network-config="+networkCfgPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("genisoimage failed: %w\nOutput: %s", err, output)
	}

	fmt.Printf("✓ Cloud-init ISO created: %s (static IP: %s)\n", isoPath, privateIP)

	return isoPath, cleanup, nil
}
func (l *LibvirtClient) RestartVM(vmName string) error {
	// Lookup the domain by name
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to find VM %s: %w", vmName, err)
	}
	defer domain.Free()

	// Check if the VM is active (running)
	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check VM state: %w", err)
	}

	if !active {
		return fmt.Errorf("VM is not running")
	}

	// Shutdown the VM gracefully
	if err := domain.Shutdown(); err != nil {
		return fmt.Errorf("failed to shut down VM %s: %w", vmName, err)
	}

	// Wait for the VM to shut down (polling for the active status to become false)
	for {
		active, err := domain.IsActive()
		if err != nil {
			return fmt.Errorf("failed to check VM state: %w", err)
		}

		if !active {
			// VM has shut down
			break
		}

		// Sleep for a short period before checking again
		time.Sleep(1 * time.Second)
	}

	// Start the VM again
	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to restart VM %s: %w", vmName, err)
	}

	return nil
}

func (l *LibvirtClient) StopVM(vmName string) error {
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return err
	}
	defer domain.Free()
	return domain.Shutdown()
}

func (l *LibvirtClient) StartVM(vmName string) error {
	// Use name instead of ID
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to find VM %s: %w", vmName, err)
	}
	defer domain.Free()

	// Check if already running
	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check VM state: %w", err)
	}

	if active {
		return fmt.Errorf("VM is already running")
	}

	// Start the VM
	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	return nil
}

func (l *LibvirtClient) DeleteVM(vmName string) error {
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return err
	}

	// Force stop if running
	domain.Destroy()

	return domain.Undefine()
}

func (l *LibvirtClient) GetPublicIP(vmID int) string {
	// Return NAT port mapping for SSH access
	return fmt.Sprintf("localhost:%d", 2200+vmID)
}
func (l *LibvirtClient) buildVMXML(name, diskPath, isoPath string, cpu, ram int, bridgeName string) string {
	networkXML := ""
	if bridgeName != "" {
		networkXML = fmt.Sprintf(`
    <interface type='bridge'>
      <source bridge='%s'/>
      <model type='virtio'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x03' function='0x0'/>
    </interface>`, bridgeName)
	} else {
		networkXML = `
    <interface type='network'>
      <source network='default'/>
      <model type='virtio'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x03' function='0x0'/>
    </interface>`
	}

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
    <!-- Main disk -->
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' cache='writeback'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x04' function='0x0'/>
    </disk>
    <!-- Cloud-init ISO -->
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
    <!-- Network with DHCP -->
    %s
    <!-- Serial console -->
    <serial type='pty'>
      <target type='isa-serial' port='0'>
        <model name='isa-serial'/>
      </target>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
    <!-- Graphics -->
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
`, name, ram, cpu, diskPath, isoPath, networkXML)
}

// EnsureDefaultNetwork ensures the default network is active and has DHCP
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
			// Check for specific bridge conflict error
			if strings.Contains(err.Error(), "Network is already in use by interface") {
				return fmt.Errorf("failed to start default network: %w. This is likely a bridge conflict. Try running: 'sudo ip link set virbr0 down && sudo ip link delete virbr0' then restart the service", err)
			}
			return fmt.Errorf("failed to start default network: %w", err)
		}
	}

	// Check autostart
	autostart, err := network.GetAutostart()
	if err == nil && !autostart {
		network.SetAutostart(true)
	}

	return nil
}

// waitForVMIP waits for the VM to get an IP address
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

func (l *LibvirtClient) getVMIP(domain *libvirt.Domain) (string, error) {
	ifaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		for _, addr := range iface.Addrs {
			if addr.Type == libvirt.IP_ADDR_TYPE_IPV4 && !strings.HasPrefix(addr.Addr, "127.") {
				return strings.Split(addr.Addr, "/")[0], nil
			}
		}
	}
	return "", fmt.Errorf("no IP found")
}

func (l *LibvirtClient) CreateSnapshot(vmName, snapshotName, description string) error {
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	xml := fmt.Sprintf(`
<domainsnapshot>
  <name>%s</name>
  <description>%s</description>
</domainsnapshot>`, snapshotName, description)

	_, err = domain.CreateSnapshotXML(xml, 0)
	if err != nil {
		return fmt.Errorf("failed to create snapshot for %s: %w", vmName, err)
	}

	return nil
}

func (l *LibvirtClient) DeleteSnapshot(vmName, snapshotName string) error {
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	snapshot, err := domain.SnapshotLookupByName(snapshotName, 0)
	if err != nil {
		return fmt.Errorf("failed to lookup snapshot %s for domain %s: %w", snapshotName, vmName, err)
	}
	defer snapshot.Free()

	if err := snapshot.Delete(0); err != nil {
		return fmt.Errorf("failed to delete snapshot %s for domain %s: %w", snapshotName, vmName, err)
	}

	return nil
}

func (l *LibvirtClient) RestoreSnapshot(vmName, snapshotName string) error {
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	snapshot, err := domain.SnapshotLookupByName(snapshotName, 0)
	if err != nil {
		return fmt.Errorf("failed to lookup snapshot %s for domain %s: %w", snapshotName, vmName, err)
	}
	defer snapshot.Free()

	if err := snapshot.RevertToSnapshot(0); err != nil {
		return fmt.Errorf("failed to revert domain %s to snapshot %s: %w", vmName, snapshotName, err)
	}

	return nil
}
