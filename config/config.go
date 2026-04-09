package config

import (
	"fmt"
	"regexp"
	"time"
)

var (
	ValidName     = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	ValidLabelKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	ValidLabelVal = regexp.MustCompile(`^[a-zA-Z0-9._-]{0,128}$`)
)

const (
	MaxLabels      = 32
	MaxUserLabels  = 8
	LabelCreatedBy = "created-by"

	IdentityCLI           = "cli"
	IdentityK8sController = "k8s"
)

const (
	LabelCloneSourceType = "clone.source.type"
	LabelCloneSourceName = "clone.source.name"
)

// ValidationError is returned by ValidateName and ValidateLabels.
// Consumers can type-assert to distinguish validation errors from other errors.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func ValidateName(name string) error {
	if !ValidName.MatchString(name) {
		return &ValidationError{Message: fmt.Sprintf("invalid name: %q (must be 1-128 chars, only a-z A-Z 0-9 _ -)", name)}
	}
	return nil
}

func ValidateLabels(labels map[string]string) error {
	if len(labels) > MaxLabels {
		return &ValidationError{Message: fmt.Sprintf("too many labels: %d (max %d)", len(labels), MaxLabels)}
	}
	for k, v := range labels {
		if !ValidLabelKey.MatchString(k) {
			return &ValidationError{Message: fmt.Sprintf("invalid label key: %q", k)}
		}
		if !ValidLabelVal.MatchString(v) {
			return &ValidationError{Message: fmt.Sprintf("invalid label value: %q", v)}
		}
	}
	return nil
}

// SoftReservedLabelKeys are managed automatically (identity, clone source tracking).
// Cannot be set via K8s annotations or CLI flags. Agent API consumers should use v1.Client
// which handles these automatically.
var SoftReservedLabelKeys = []string{LabelCreatedBy, LabelCloneSourceType, LabelCloneSourceName}

const (
	DataDir      = "data"
	MetadataFile = "metadata.json"
	SnapshotsDir = "snapshots"
	TasksDir     = "tasks"
)

type AgentConfig struct {
	BasePath               string        `env:"AGENT_BASE_PATH" envDefault:"./storage"`
	ListenAddr             string        `env:"AGENT_LISTEN_ADDR" envDefault:":8080"`
	MetricsAddr            string        `env:"AGENT_METRICS_ADDR" envDefault:"127.0.0.1:9090"`
	Tenants                string        `env:"AGENT_TENANTS,required"`
	TLSCert                string        `env:"AGENT_TLS_CERT"`
	TLSKey                 string        `env:"AGENT_TLS_KEY"`
	QuotaEnabled           bool          `env:"AGENT_FEATURE_QUOTA_ENABLED" envDefault:"true"`
	UsageInterval          time.Duration `env:"AGENT_FEATURE_QUOTA_UPDATE_INTERVAL" envDefault:"1m"`
	NFSExporter            string        `env:"AGENT_NFS_EXPORTER" envDefault:"kernel"`
	ExportfsBin            string        `env:"AGENT_EXPORTFS_BIN" envDefault:"exportfs"`
	KernelExportOptions    string        `env:"AGENT_KERNEL_EXPORT_OPTIONS" envDefault:"rw,nohide,crossmnt,no_root_squash,no_subtree_check"`
	ImmutableLabels        string        `env:"AGENT_IMMUTABLE_LABELS"`
	BtrfsBin               string        `env:"AGENT_BTRFS_BIN" envDefault:"btrfs"`
	NFSReconcileInterval   time.Duration `env:"AGENT_NFS_RECONCILE_INTERVAL" envDefault:"60s"`
	DeviceIOInterval       time.Duration `env:"AGENT_DEVICE_IO_INTERVAL" envDefault:"5s"`
	DeviceStatsInterval    time.Duration `env:"AGENT_DEVICE_STATS_INTERVAL" envDefault:"1m"`
	DefaultDirMode         string        `env:"AGENT_DEFAULT_DIR_MODE" envDefault:"0700"`
	DefaultDataMode        string        `env:"AGENT_DEFAULT_DATA_MODE" envDefault:"2770"`
	TaskCleanupInterval    time.Duration `env:"AGENT_TASK_CLEANUP_INTERVAL" envDefault:"24h"`
	TaskMaxConcurrent      int           `env:"AGENT_TASK_MAX_CONCURRENT" envDefault:"2"`
	TaskDefaultTimeout     time.Duration `env:"AGENT_TASK_DEFAULT_TIMEOUT" envDefault:"6h"`
	TaskScrubTimeout       time.Duration `env:"AGENT_TASK_SCRUB_TIMEOUT" envDefault:"24h"`
	TaskPollInterval       time.Duration `env:"AGENT_TASK_POLL_INTERVAL" envDefault:"5s"`
	DefaultPageLimit       int           `env:"AGENT_DEFAULT_PAGE_LIMIT" envDefault:"0"`
	PaginationSnapshotTTL  time.Duration `env:"AGENT_API_PAGINATION_SNAPSHOT_TTL" envDefault:"30s"`
	PaginationMaxSnapshots int           `env:"AGENT_API_PAGINATION_MAX_SNAPSHOTS" envDefault:"100"`
	SwaggerEnabled         bool          `env:"AGENT_API_SWAGGER_ENABLED"`
}

type ControllerConfig struct {
	Endpoint      string `env:"DRIVER_ENDPOINT" envDefault:"unix:///csi/csi.sock"`
	MetricsAddr   string `env:"DRIVER_METRICS_ADDR" envDefault:":9090"`
	DefaultLabels string `env:"DRIVER_DEFAULT_LABELS"`
}

type NodeConfig struct {
	NodeID           string `env:"DRIVER_NODE_ID,required"`
	NodeIP           string `env:"DRIVER_NODE_IP"`
	StorageInterface string `env:"DRIVER_STORAGE_INTERFACE"`
	StorageCIDR      string `env:"DRIVER_STORAGE_CIDR"`
	Endpoint         string `env:"DRIVER_ENDPOINT" envDefault:"unix:///csi/csi.sock"`
	MetricsAddr      string `env:"DRIVER_METRICS_ADDR" envDefault:":9090"`
}
