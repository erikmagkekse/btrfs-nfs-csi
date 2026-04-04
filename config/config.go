package config

import (
	"regexp"
	"strings"
	"time"
)

var (
	ValidName     = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	ValidLabelKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	ValidLabelVal = regexp.MustCompile(`^[a-zA-Z0-9._-]{0,128}$`)
)

const DriverName = "btrfs-nfs-csi"

// K8s settings
const (
	AnnoPrefix = DriverName + "/"

	PvcNameKey      = "csi.storage.k8s.io/pvc/name"
	PvcNamespaceKey = "csi.storage.k8s.io/pvc/namespace"

	SecretNameKey      = "csi.storage.k8s.io/provisioner-secret-name"
	SecretNamespaceKey = "csi.storage.k8s.io/provisioner-secret-namespace"

	ParamNoCOW       = "nocow"
	ParamCompression = "compression"
	ParamUID         = "uid"
	ParamGID         = "gid"
	ParamMode        = "mode"
	ParamLabels      = "labels"
	MaxUserLabels    = 4

	// MaxLabels caps total labels per resource, trying to keep metadata JSON below 4KB (one filesystem block).
	// Worst case: 12 * (63 + 128 + 6) = ~2.4KB labels + ~500B other fields = ~2.9KB.
	MaxLabels = 12

	ParamNFSServer       = "nfsServer"
	ParamNFSMountOptions = "nfsMountOptions"
	ParamNFSSharePath    = "nfsSharePath"

	VolumeIDSep = "|"
	NodeIDSep   = "|"

	LabelCreatedBy              = "created-by"
	LabelKubernetesNodeName     = "kubernetes.node.name"
	LabelKubernetesVolumeID     = "kubernetes.pvc.id"
	LabelKubernetesStorageClass = "kubernetes.pvc.storageclass"
)

// DefaultImmutableLabelKeys are always immutable. Additional keys can be added via AGENT_IMMUTABLE_LABELS.
var DefaultImmutableLabelKeys = []string{LabelCreatedBy}

// ImmutableLabelKeys returns the merged list of default + configured immutable label keys.
func ImmutableLabelKeys(extra string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, k := range DefaultImmutableLabelKeys {
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	for _, k := range strings.Split(extra, ",") {
		k = strings.TrimSpace(k)
		if k != "" && !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	return keys
}

// Storage engine settings
const (
	DataDir      = "data"
	MetadataFile = "metadata.json"
	SnapshotsDir = "snapshots"
	TasksDir     = "tasks"
)

type AgentConfig struct {
	BasePath             string        `env:"AGENT_BASE_PATH" envDefault:"./storage"`
	ListenAddr           string        `env:"AGENT_LISTEN_ADDR" envDefault:":8080"`
	MetricsAddr          string        `env:"AGENT_METRICS_ADDR" envDefault:"127.0.0.1:9090"`
	Tenants              string        `env:"AGENT_TENANTS,required"`
	TLSCert              string        `env:"AGENT_TLS_CERT"`
	TLSKey               string        `env:"AGENT_TLS_KEY"`
	QuotaEnabled         bool          `env:"AGENT_FEATURE_QUOTA_ENABLED" envDefault:"true"`
	UsageInterval        time.Duration `env:"AGENT_FEATURE_QUOTA_UPDATE_INTERVAL" envDefault:"1m"`
	NFSExporter          string        `env:"AGENT_NFS_EXPORTER" envDefault:"kernel"`
	ExportfsBin          string        `env:"AGENT_EXPORTFS_BIN" envDefault:"exportfs"`
	KernelExportOptions  string        `env:"AGENT_KERNEL_EXPORT_OPTIONS" envDefault:"rw,nohide,crossmnt,no_root_squash,no_subtree_check"`
	ImmutableLabels      string        `env:"AGENT_IMMUTABLE_LABELS"`
	BtrfsBin             string        `env:"AGENT_BTRFS_BIN" envDefault:"btrfs"`
	NFSReconcileInterval time.Duration `env:"AGENT_NFS_RECONCILE_INTERVAL" envDefault:"60s"`
	DeviceIOInterval     time.Duration `env:"AGENT_DEVICE_IO_INTERVAL" envDefault:"5s"`
	DeviceStatsInterval  time.Duration `env:"AGENT_DEVICE_STATS_INTERVAL" envDefault:"1m"`
	DashboardRefresh     int           `env:"AGENT_DASHBOARD_REFRESH_SECONDS" envDefault:"5"`
	DefaultDirMode       string        `env:"AGENT_DEFAULT_DIR_MODE" envDefault:"0700"`
	DefaultDataMode      string        `env:"AGENT_DEFAULT_DATA_MODE" envDefault:"2770"`
	TaskCleanupInterval  time.Duration `env:"AGENT_TASK_CLEANUP_INTERVAL" envDefault:"24h"`
	TaskMaxConcurrent    int           `env:"AGENT_TASK_MAX_CONCURRENT" envDefault:"2"`
	TaskDefaultTimeout   time.Duration `env:"AGENT_TASK_DEFAULT_TIMEOUT" envDefault:"6h"`
	TaskScrubTimeout     time.Duration `env:"AGENT_TASK_SCRUB_TIMEOUT" envDefault:"24h"`
	TaskPollInterval     time.Duration `env:"AGENT_TASK_POLL_INTERVAL" envDefault:"5s"`
}

type ControllerConfig struct {
	Endpoint      string `env:"DRIVER_ENDPOINT" envDefault:"unix:///csi/csi.sock"`
	MetricsAddr   string `env:"DRIVER_METRICS_ADDR" envDefault:":9090"`
	DefaultLabels string `env:"DRIVER_DEFAULT_LABELS"`
}

type NodeConfig struct {
	NodeID              string        `env:"DRIVER_NODE_ID,required"`
	NodeIP              string        `env:"DRIVER_NODE_IP"`
	StorageInterface    string        `env:"DRIVER_STORAGE_INTERFACE"`
	StorageCIDR         string        `env:"DRIVER_STORAGE_CIDR"`
	Endpoint            string        `env:"DRIVER_ENDPOINT" envDefault:"unix:///csi/csi.sock"`
	MetricsAddr         string        `env:"DRIVER_METRICS_ADDR" envDefault:":9090"`
	HealthCheckInterval time.Duration `env:"DRIVER_HEALTH_CHECK_INTERVAL" envDefault:"30s"`
}
