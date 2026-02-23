package model

import "time"

const DriverName = "btrfs-nfs-csi"

const (
	AnnoPrefix      = DriverName + "/"
	AnnoNoCOW       = AnnoPrefix + "nocow"
	AnnoCompression = AnnoPrefix + "compression"
	AnnoUID         = AnnoPrefix + "uid"
	AnnoGID         = AnnoPrefix + "gid"
	AnnoMode        = AnnoPrefix + "mode"

	PvcNameKey      = "csi.storage.k8s.io/pvc/name"
	PvcNamespaceKey = "csi.storage.k8s.io/pvc/namespace"

	ParamNFSServer       = "nfsServer"
	ParamNFSMountOptions = "nfsMountOptions"
	ParamNFSSharePath    = "nfsSharePath"
)

type AgentConfig struct {
	BasePath             string        `env:"AGENT_BASE_PATH" envDefault:"./storage"`
	ListenAddr           string        `env:"AGENT_LISTEN_ADDR" envDefault:":8080"`
	Tenants              string        `env:"AGENT_TENANTS,required"`
	TLSCert              string        `env:"AGENT_TLS_CERT"`
	TLSKey               string        `env:"AGENT_TLS_KEY"`
	QuotaEnabled         bool          `env:"AGENT_FEATURE_QUOTA_ENABLED" envDefault:"true"`
	UsageInterval        time.Duration `env:"AGENT_FEATURE_QUOTA_UPDATE_INTERVAL" envDefault:"1m"`
	NFSExporter          string        `env:"AGENT_NFS_EXPORTER" envDefault:"kernel"`
	ExportfsBin          string        `env:"AGENT_EXPORTFS_BIN" envDefault:"exportfs"`
	NFSReconcileInterval time.Duration `env:"AGENT_NFS_RECONCILE_INTERVAL" envDefault:"10m"`
	DashboardRefresh     int           `env:"AGENT_DASHBOARD_REFRESH_SECONDS" envDefault:"5"`
}

type ControllerConfig struct {
	Endpoint    string `env:"DRIVER_ENDPOINT" envDefault:"unix:///csi/csi.sock"`
	MetricsAddr string `env:"DRIVER_METRICS_ADDR" envDefault:":9090"`
}

type NodeConfig struct {
	NodeID           string `env:"DRIVER_NODE_ID,required"`
	NodeIP           string `env:"DRIVER_NODE_IP"`
	StorageInterface string `env:"DRIVER_STORAGE_INTERFACE"`
	StorageCIDR      string `env:"DRIVER_STORAGE_CIDR"`
	Endpoint         string `env:"DRIVER_ENDPOINT" envDefault:"unix:///csi/csi.sock"`
	MetricsAddr      string `env:"DRIVER_METRICS_ADDR" envDefault:":9090"`
}
