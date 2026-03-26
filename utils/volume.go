package utils

import (
	"fmt"
	"strings"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
)

func MakeVolumeID(storageClass, name string) string {
	return storageClass + config.VolumeIDSep + name
}

func ParseVolumeID(id string) (storageClass, name string, err error) {
	parts := strings.SplitN(id, config.VolumeIDSep, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid volume ID: %s", id)
	}
	return parts[0], parts[1], nil
}
