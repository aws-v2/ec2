package vpcpkg

// Provisioner currently serves as a placeholder.
// The actual physical libvirt NAT network orchestration has been moved out of the 
// control plane and into the host agent (`/reconcile` endpoint).
type Provisioner struct {
	// Previously held a libvirt connection
}

func NewVPCProvisioner() *Provisioner {
	return &Provisioner{}
}