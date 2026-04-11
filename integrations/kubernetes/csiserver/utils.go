package csiserver

import (
	"fmt"
	"strings"
)

func MakeVolumeID(storageClass, name string) string {
	return storageClass + VolumeIDSep + name
}

func ParseVolumeID(id string) (storageClass, name string, err error) {
	sc, n, ok := strings.Cut(id, VolumeIDSep)
	if !ok || sc == "" || n == "" {
		return "", "", fmt.Errorf("invalid volume ID: %s", id)
	}
	return sc, n, nil
}

func MakeNodeID(hostname, ip string) string {
	return hostname + NodeIDSep + ip
}

func ParseNodeID(nodeID string) (hostname, ip string, err error) {
	h, i, ok := strings.Cut(nodeID, NodeIDSep)
	if !ok || i == "" {
		return "", "", fmt.Errorf("invalid node ID %q (expected hostname%sip)", nodeID, NodeIDSep)
	}
	return h, i, nil
}
