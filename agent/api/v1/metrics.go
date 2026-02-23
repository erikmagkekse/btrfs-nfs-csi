package v1

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests by method, path, and status code.",
	}, []string{"method", "path", "code"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request duration in seconds.",
		Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"method", "path"})
)

func init() {
	prometheus.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
	)
}

func MetricsHandler() echo.HandlerFunc {
	h := promhttp.Handler()
	return func(c *echo.Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

func MetricsMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			err := next(c)
			duration := time.Since(start).Seconds()

			method := c.Request().Method
			path := c.RouteInfo().Path
			resp := c.Response().(*echo.Response)
			code := strconv.Itoa(resp.Status)

			httpRequestsTotal.WithLabelValues(method, path, code).Inc()
			httpRequestDuration.WithLabelValues(method, path).Observe(duration)

			log.Debug().
				Str("method", method).
				Str("path", path).
				Str("code", code).
				Str("client", c.RealIP()).
				Dur("duration", time.Since(start)).
				Msg("request")

			return err
		}
	}
}
