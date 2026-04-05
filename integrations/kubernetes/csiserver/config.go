package csiserver

const DriverName = "btrfs-nfs-csi"

const (
	// Volume / Node ID separators
	VolumeIDSep = "|"
	NodeIDSep   = "|"

	// CSI provisioner extra-create-metadata keys
	PvcNameKey      = "csi.storage.k8s.io/pvc/name"
	PvcNamespaceKey = "csi.storage.k8s.io/pvc/namespace"

	// CSI secret parameter keys
	SecretNameKey      = "csi.storage.k8s.io/provisioner-secret-name"
	SecretNamespaceKey = "csi.storage.k8s.io/provisioner-secret-namespace"

	// StorageClass / VolumeContext parameter keys
	ParamNFSServer       = "nfsServer"
	ParamNFSMountOptions = "nfsMountOptions"
	ParamNFSSharePath    = "nfsSharePath"
)
