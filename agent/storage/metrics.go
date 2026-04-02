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

	// Device IO metrics
	DeviceReadBytesTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_read_bytes_total",
		Help:      "Total bytes read from the block device.",
	}, []string{"device"})

	DeviceReadIOsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_read_ios_total",
		Help:      "Total read IO operations on the block device.",
	}, []string{"device"})

	DeviceReadTimeSecondsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_read_time_seconds_total",
		Help:      "Total time spent reading from the block device in seconds.",
	}, []string{"device"})

	DeviceWriteBytesTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_write_bytes_total",
		Help:      "Total bytes written to the block device.",
	}, []string{"device"})

	DeviceWriteIOsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_write_ios_total",
		Help:      "Total write IO operations on the block device.",
	}, []string{"device"})

	DeviceWriteTimeSecondsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_write_time_seconds_total",
		Help:      "Total time spent writing to the block device in seconds.",
	}, []string{"device"})

	DeviceIOsInProgress = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_ios_in_progress",
		Help:      "Number of IO operations currently in progress.",
	}, []string{"device"})

	DeviceIOTimeSecondsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_io_time_seconds_total",
		Help:      "Total time spent on IO operations in seconds.",
	}, []string{"device"})

	DeviceIOWeightedTimeSecondsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_io_weighted_time_seconds_total",
		Help:      "Weighted total time spent on IO operations in seconds.",
	}, []string{"device"})

	DeviceSizeBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_size_bytes",
		Help:      "Total size of the block device in bytes.",
	}, []string{"device"})

	DeviceAllocatedBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_allocated_bytes",
		Help:      "Bytes allocated by btrfs on the block device.",
	}, []string{"device"})

	DevicePresentGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_present",
		Help:      "Whether the block device is present (1) or missing (0).",
	}, []string{"device"})

	// Device error metrics
	DeviceReadErrsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_read_errs_total",
		Help:      "Total btrfs read errors on the device.",
	}, []string{"device"})

	DeviceWriteErrsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_write_errs_total",
		Help:      "Total btrfs write errors on the device.",
	}, []string{"device"})

	DeviceFlushErrsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_flush_errs_total",
		Help:      "Total btrfs flush errors on the device.",
	}, []string{"device"})

	DeviceCorruptionErrsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_corruption_errs_total",
		Help:      "Total btrfs corruption errors on the device.",
	}, []string{"device"})

	DeviceGenerationErrsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "device_generation_errs_total",
		Help:      "Total btrfs generation errors on the device.",
	}, []string{"device"})

	// Filesystem allocation metrics (labeled by mount path, not device,
	// because filesystem usage spans all devices in a multi-device setup)
	FilesystemSizeBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "filesystem_size_bytes",
		Help:      "Total filesystem size in bytes.",
	}, []string{"path"})

	FilesystemUsedBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "filesystem_used_bytes",
		Help:      "Used filesystem space in bytes.",
	}, []string{"path"})

	FilesystemUnallocatedBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "filesystem_unallocated_bytes",
		Help:      "Unallocated filesystem space in bytes.",
	}, []string{"path"})

	FilesystemMetadataUsedBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "filesystem_metadata_used_bytes",
		Help:      "Used metadata space in bytes.",
	}, []string{"path"})

	FilesystemMetadataTotalBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "filesystem_metadata_total_bytes",
		Help:      "Total metadata allocation in bytes.",
	}, []string{"path"})

	FilesystemDataRatio = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "filesystem_data_ratio",
		Help:      "Data RAID profile ratio (1.0 for single, 2.0 for RAID1/DUP).",
	}, []string{"path"})

	// Task metrics
	TasksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "tasks_total",
		Help:      "Total number of completed tasks by type and status.",
	}, []string{"type", "status"})

	TaskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "task_duration_seconds",
		Help:      "Duration of tasks in seconds.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 15),
	}, []string{"type"})
)

func init() {
	prometheus.MustRegister(
		VolumesGauge,
		ExportsGauge,
		VolumeSizeBytes,
		VolumeUsedBytes,
		// Device IO
		DeviceReadBytesTotal,
		DeviceReadIOsTotal,
		DeviceReadTimeSecondsTotal,
		DeviceWriteBytesTotal,
		DeviceWriteIOsTotal,
		DeviceWriteTimeSecondsTotal,
		DeviceIOsInProgress,
		DeviceIOTimeSecondsTotal,
		DeviceIOWeightedTimeSecondsTotal,
		DeviceSizeBytes,
		DeviceAllocatedBytes,
		DevicePresentGauge,
		// Device errors
		DeviceReadErrsTotal,
		DeviceWriteErrsTotal,
		DeviceFlushErrsTotal,
		DeviceCorruptionErrsTotal,
		DeviceGenerationErrsTotal,
		// Filesystem allocation
		FilesystemSizeBytes,
		FilesystemUsedBytes,
		FilesystemUnallocatedBytes,
		FilesystemMetadataUsedBytes,
		FilesystemMetadataTotalBytes,
		FilesystemDataRatio,
		// Tasks
		TasksTotal,
		TaskDuration,
	)
}
