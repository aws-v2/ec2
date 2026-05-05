package vpcpkg

import (
	"fmt"
	"net"
)

const (
	// masterCIDR is the pool from which all VPC subnets are carved.
	// Each VPC gets one /24 block: 10.0.1.0/24, 10.0.2.0/24 ... 10.0.255.0/24
	// then 10.1.0.0/24, 10.1.1.0/24, etc.
	masterBase = "10.0.0.0"
	subnetBits = 24
)

// AllocateNextCIDR returns the next available /24 block that is not in usedCIDRs.
// The caller is responsible for running this inside a serializable transaction.
func AllocateNextCIDR(usedCIDRs []string) (string, error) {
	used := make(map[string]bool, len(usedCIDRs))
	for _, c := range usedCIDRs {
		used[c] = true
	}

	base := net.ParseIP(masterBase).To4()

	// Iterate through 10.0.1.0/24 → 10.255.254.0/24
	// Skip 10.x.x.0 (network addr) by starting third octet at 1
	for second := 0; second <= 255; second++ {
		start := 1
		if second == 0 {
			start = 1 // skip 10.0.0.0/24
		}
		for third := start; third <= 254; third++ {
			candidate := fmt.Sprintf("%d.%d.%d.0/%d", base[0], second, third, subnetBits)
			if !used[candidate] {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("exhausted all available /24 blocks in %s/8", masterBase)
}

// GatewayFromCIDR returns the gateway IP for a subnet — always the .1 address.
// e.g. "10.0.1.0/24" → "10.0.1.1"
func GatewayFromCIDR(cidr string) (string, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	_ = ip
	base := network.IP.To4()
	if base == nil {
		return "", fmt.Errorf("only IPv4 CIDRs are supported")
	}
	gateway := net.IP{base[0], base[1], base[2], 1}
	return gateway.String(), nil
}

// BridgeNameFromVPCID derives a deterministic, valid Linux interface name.
// Linux bridge names must be ≤ 15 characters.
// Format: "vbr-" + first 11 chars of vpc ID → exactly 15 chars max.
func BridgeNameFromVPCID(vpcID string) string {
	suffix := vpcID
	if len(suffix) > 11 {
		suffix = suffix[:11]
	}
	return "vbr-" + suffix
}

// NextAvailableIP returns the next unallocated host IP within the subnet.
// Skips .0 (network), .1 (gateway), .255 (broadcast).
// Assignable range: .10 → .254 (leaves .2–.9 for future reserved use).
func NextAvailableIP(cidr string, usedIPs []string) (string, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}

	used := make(map[string]bool, len(usedIPs))
	for _, ip := range usedIPs {
		used[ip] = true
	}

	base := network.IP.To4()
	if base == nil {
		return "", fmt.Errorf("only IPv4 CIDRs are supported")
	}

	for i := 10; i <= 254; i++ {
		candidate := net.IP{base[0], base[1], base[2], byte(i)}.String()
		if !used[candidate] {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("subnet %s is full — no available IPs", cidr)
}