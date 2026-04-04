package controller

import "github.com/erikmagkekse/btrfs-nfs-csi/csiserver"

const (
	controllerIdentity = "k8s"
	paramAgentURL      = "agentURL"
	secretAgentToken   = "agentToken"

	// Volume labels (set on agent volumes from PVC metadata)
	labelPVCName         = "kubernetes.pvc.name"
	labelPVCNamespace    = "kubernetes.pvc.namespace"
	labelPVCStorageClass = "kubernetes.pvc.storageclassname"

	// Export labels (set on NFS exports)
	labelPVName               = "kubernetes.pv.name"
	labelPVStorageClass       = "kubernetes.pv.storageclassname"
	labelNodeName             = "kubernetes.node.name"
	labelVolumeAttachmentName = "kubernetes.volumeattachment.name"

	// Snapshot labels (source PVC identity)
	labelSourcePVCName         = "kubernetes.source.pvc.name"
	labelSourcePVCNamespace    = "kubernetes.source.pvc.namespace"
	labelSourcePVCStorageClass = "kubernetes.source.pvc.storageclassname"

	// Snapshot labels (VolumeSnapshot identity)
	labelSnapshotName      = "kubernetes.snapshot.name"
	labelSnapshotNamespace = "kubernetes.snapshot.namespace"

	// CSI snapshotter extra-create-metadata keys
	snapshotNameKey      = "csi.storage.k8s.io/volumesnapshot/name"
	snapshotNamespaceKey = "csi.storage.k8s.io/volumesnapshot/namespace"

	// StorageClass / PVC annotation parameter keys
	annoPrefix       = csiserver.DriverName + "/"
	paramNoCOW       = "nocow"
	paramCompression = "compression"
	paramUID         = "uid"
	paramGID         = "gid"
	paramMode        = "mode"
	paramLabels      = "labels"
)
