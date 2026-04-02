package task

import "github.com/prometheus/client_golang/prometheus"

var (
	tasksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "tasks_total",
	}, []string{"type", "status"})

	taskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "task_duration_seconds",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 15),
	}, []string{"type"})
)

func init() {
	prometheus.MustRegister(tasksTotal, taskDuration)
}
