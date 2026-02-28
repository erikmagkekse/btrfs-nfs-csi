package driver

import (
	"fmt"
	"net"
)

// ResolveNodeIP determines the node's storage IP using the following priority:
//
//  1. DRIVER_STORAGE_INTERFACE - use the first non-loopback IPv4 address on the
//     named interface (e.g. "eth1", "ens192"). Best for dedicated storage NICs.
//
//  2. DRIVER_STORAGE_CIDR - use the first address on any interface that falls
//     within the given CIDR (e.g. "10.10.0.0/24"). Useful when interface names
//     vary across nodes but the storage subnet is consistent.
//
//  3. DRIVER_NODE_IP - static fallback, typically set via the Kubernetes
//     Downward API (status.hostIP). Works for single-network setups.
//
// At least one of these must be configured. If DRIVER_STORAGE_INTERFACE or
// DRIVER_STORAGE_CIDR is set, the resolved IP takes precedence over DRIVER_NODE_IP.
func ResolveNodeIP(nodeIP, storageIface, storageCIDR string) (string, error) {
	if storageIface != "" {
		ip, err := ipFromInterface(storageIface)
		if err != nil {
			return "", fmt.Errorf("DRIVER_STORAGE_INTERFACE=%s: %w", storageIface, err)
		}
		return ip, nil
	}

	if storageCIDR != "" {
		ip, err := ipFromCIDR(storageCIDR)
		if err != nil {
			return "", fmt.Errorf("DRIVER_STORAGE_CIDR=%s: %w", storageCIDR, err)
		}
		return ip, nil
	}

	if nodeIP != "" {
		return nodeIP, nil
	}

	return "", fmt.Errorf("one of DRIVER_NODE_IP, DRIVER_STORAGE_INTERFACE, or DRIVER_STORAGE_CIDR is required")
}

func ipFromInterface(name string) (string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", fmt.Errorf("interface not found: %w", err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("reading addresses: %w", err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ip4 := ipNet.IP.To4(); ip4 != nil {
			return ip4.String(), nil
		}
	}

	return "", fmt.Errorf("no IPv4 address on interface %s", name)
}

func ipFromCIDR(cidr string) (string, error) {
	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR: %w", err)
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("listing interfaces: %w", err)
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipNet.IP.To4(); ip4 != nil && subnet.Contains(ip4) {
				return ip4.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no address found matching %s", cidr)
}
