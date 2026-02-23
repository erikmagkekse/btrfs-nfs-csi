package controller

import "github.com/prometheus/client_golang/prometheus"

var (
	agentOpsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "controller",
		Name:      "agent_ops_total",
		Help:      "Total agent API operations by type, status, and storage class.",
	}, []string{"operation", "status", "storage_class"})

	agentDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "controller",
		Name:      "agent_duration_seconds",
		Help:      "Agent API operation duration in seconds.",
		Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"operation", "storage_class"})

	agentHealthTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "controller",
		Name:      "agent_health_total",
		Help:      "Total agent health check results.",
	}, []string{"result", "storage_class"})

	ctrlK8sOpsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "controller",
		Name:      "k8s_ops_total",
		Help:      "Total K8s API operations from controller by status.",
	}, []string{"status"})
)

func init() {
	prometheus.MustRegister(agentOpsTotal, agentDuration, agentHealthTotal, ctrlK8sOpsTotal)
}
