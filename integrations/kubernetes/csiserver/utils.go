package csiserver

import (
	"fmt"
	"strings"
)

func MakeVolumeID(storageClass, name string) string {
	return storageClass + VolumeIDSep + name
}

func ParseVolumeID(id string) (storageClass, name string, err error) {
	parts := strings.SplitN(id, VolumeIDSep, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid volume ID: %s", id)
	}
	return parts[0], parts[1], nil
}

func MakeNodeID(hostname, ip string) string {
	return hostname + NodeIDSep + ip
}

func ParseNodeID(nodeID string) (hostname, ip string, err error) {
	parts := strings.SplitN(nodeID, NodeIDSep, 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", fmt.Errorf("invalid node ID %q (expected hostname%sip)", nodeID, NodeIDSep)
	}
	return parts[0], parts[1], nil
}
