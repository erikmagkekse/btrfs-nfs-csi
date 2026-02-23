package driver

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
		Subsystem: "node",
		Name:      "grpc_requests_total",
		Help:      "Total gRPC requests by method and status code.",
	}, []string{"method", "code"})

	grpcRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "node",
		Name:      "grpc_request_duration_seconds",
		Help:      "gRPC request duration in seconds.",
		Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"method"})

	mountOpsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "node",
		Name:      "mount_ops_total",
		Help:      "Total mount/unmount operations by type and status.",
	}, []string{"operation", "status"})

	mountDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "node",
		Name:      "mount_duration_seconds",
		Help:      "Mount/unmount operation duration in seconds.",
		Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120},
	}, []string{"operation"})
)

func init() {
	prometheus.MustRegister(grpcRequestsTotal, grpcRequestDuration,
		mountOpsTotal, mountDuration)
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
