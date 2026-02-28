package controller

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	grpcRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "controller",
		Name:      "grpc_requests_total",
		Help:      "Total gRPC requests by method and status code.",
	}, []string{"method", "code"})

	grpcRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "controller",
		Name:      "grpc_request_duration_seconds",
		Help:      "gRPC request duration in seconds.",
		Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"method"})

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
	prometheus.MustRegister(grpcRequestsTotal, grpcRequestDuration,
		agentOpsTotal, agentDuration, agentHealthTotal, ctrlK8sOpsTotal)
}

func metricsInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if strings.HasSuffix(info.FullMethod, "/Probe") {
		return handler(ctx, req)
	}

	start := time.Now()
	log.Debug().Str("method", info.FullMethod).Msg("gRPC call")

	resp, err := handler(ctx, req)

	duration := time.Since(start).Seconds()
	code := status.Code(err).String()
	grpcRequestsTotal.WithLabelValues(info.FullMethod, code).Inc()
	grpcRequestDuration.WithLabelValues(info.FullMethod).Observe(duration)

	if err != nil {
		log.Error().Err(err).Str("method", info.FullMethod).Msg("gRPC error")
	}

	return resp, err
}

func startMetricsServer(addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		log.Info().Str("addr", addr).Msg("metrics server listening")
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Error().Err(err).Msg("metrics server failed")
		}
	}()
}
