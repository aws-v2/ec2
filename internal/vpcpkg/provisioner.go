package vpcpkg

import (
	"fmt"
	"log"

	libvirtgo "libvirt.org/go/libvirt"
)

// Provisioner manages the physical libvirt NAT network on the host.
// It is intentionally separate from service.go so it can be swapped
// for a multi-host agent implementation later without touching business logic.
type Provisioner struct {
	conn *libvirtgo.Connect
}

func NewVPCProvisioner(conn *libvirtgo.Connect) *Provisioner {
	return &Provisioner{conn: conn}
}

// EnsureNetwork creates the libvirt NAT network for a VPC if it does not
// already exist on this host. It is safe to call multiple times (idempotent).
func (p *Provisioner) EnsureNetwork(vpc *VPC) error {
	// Already exists — nothing to do
	net, err := p.conn.LookupNetworkByName(vpc.ID)
	if err == nil {
		_ = net.Free()
		log.Printf("[PROVISIONER] Network %s already exists on host, skipping", vpc.ID)
		return nil
	}

	xml := p.buildNetworkXML(vpc)
	log.Printf("[PROVISIONER] Creating libvirt NAT network for VPC %s (bridge: %s, cidr: %s)",
		vpc.ID, vpc.BridgeName, vpc.CIDRBlock)

	network, err := p.conn.NetworkDefineXML(xml)
	if err != nil {
		return fmt.Errorf("failed to define libvirt network for VPC %s: %w", vpc.ID, err)
	}
	defer network.Free()

	if err := network.SetAutostart(true); err != nil {
		return fmt.Errorf("failed to set autostart for network %s: %w", vpc.ID, err)
	}

	active, err := network.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check network active state: %w", err)
	}
	if !active {
		if err := network.Create(); err != nil {
			return fmt.Errorf("failed to start libvirt network %s: %w", vpc.ID, err)
		}
	}

	log.Printf("[PROVISIONER] Network %s created and active", vpc.ID)
	return nil
}

// DestroyNetwork stops and undefines the libvirt network for a VPC.
// Called when the VPC is deleted.
func (p *Provisioner) DestroyNetwork(vpcID string) error {
	network, err := p.conn.LookupNetworkByName(vpcID)
	if err != nil {
		// Already gone — treat as success
		log.Printf("[PROVISIONER] Network %s not found on host during destroy, skipping", vpcID)
		return nil
	}
	defer network.Free()

	active, err := network.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check network state for %s: %w", vpcID, err)
	}

	if active {
		if err := network.Destroy(); err != nil {
			return fmt.Errorf("failed to stop libvirt network %s: %w", vpcID, err)
		}
	}

	if err := network.Undefine(); err != nil {
		return fmt.Errorf("failed to undefine libvirt network %s: %w", vpcID, err)
	}

	log.Printf("[PROVISIONER] Network %s destroyed", vpcID)
	return nil
}

// buildNetworkXML produces the libvirt XML definition for a NAT network.
// The DHCP range covers .10 → .254, matching NextAvailableIP in cidr.go.
func (p *Provisioner) buildNetworkXML(vpc *VPC) string {
	// Derive DHCP range start/end from gateway IP
	// gateway is x.x.x.1, so range start = x.x.x.10, end = x.x.x.254
	base := gatewayToBase(vpc.GatewayIP)
	dhcpStart := base + ".10"
	dhcpEnd := base + ".254"

	return fmt.Sprintf(`
<network>
  <name>%s</name>
  <forward mode='nat'>
    <nat>
      <port start='1024' end='65535'/>
    </nat>
  </forward>
  <bridge name='%s' stp='on' delay='0'/>
  <ip address='%s' netmask='255.255.255.0'>
    <dhcp>
      <range start='%s' end='%s'/>
    </dhcp>
  </ip>
</network>`,
		vpc.ID,
		vpc.BridgeName,
		vpc.GatewayIP,
		dhcpStart,
		dhcpEnd,
	)
}

// gatewayToBase strips the last octet from an IP string.
// "10.0.1.1" → "10.0.1"
func gatewayToBase(gatewayIP string) string {
	for i := len(gatewayIP) - 1; i >= 0; i-- {
		if gatewayIP[i] == '.' {
			return gatewayIP[:i]
		}
	}
	return gatewayIP
}