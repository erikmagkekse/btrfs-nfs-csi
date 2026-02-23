package storage

import "github.com/prometheus/client_golang/prometheus"

var (
	VolumesGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "volumes",
		Help:      "Current number of volumes.",
	}, []string{"tenant"})

	ExportsGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "exports",
		Help:      "Current number of NFS exports.",
	}, []string{"tenant"})

	VolumeSizeBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "volume_size_bytes",
		Help:      "Volume quota size in bytes.",
	}, []string{"tenant", "volume"})

	VolumeUsedBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "volume_used_bytes",
		Help:      "Volume used space in bytes.",
	}, []string{"tenant", "volume"})
)

func init() {
	prometheus.MustRegister(
		VolumesGauge,
		ExportsGauge,
		VolumeSizeBytes,
		VolumeUsedBytes,
	)
}
