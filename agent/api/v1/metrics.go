package v1

import (
	"context"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// httpRequestsTotal counts HTTP requests by method, path, status code,
	// and optional denial reason. `reason` is empty for ordinary outcomes
	// and set to one of the denial* constants (see middleware.go) when the
	// handler rejected the request (invalid token, role gate, ownership, ...).
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "btrfs_nfs_csi",
		Subsystem: "agent",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests by method, path, status code, and denial reason (empty for non-denials).",
	}, []string{"method", "path", "code", "reason"})

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
			dur := time.Since(start)

			method := c.Request().Method
			path := c.RouteInfo().Path
			// Resolve the real status. echo's DefaultHTTPErrorHandler runs
			// after middleware unwinds, so reading c.Response().Status alone
			// would report 200 for handler-returned errors (e.g. 404 from a
			// not-found path, 401 from auth). echo.ResolveResponseStatus walks
			// err and returns the status that will actually be sent.
			_, status := echo.ResolveResponseStatus(c.Response(), err)
			code := strconv.Itoa(status)
			reason, _ := c.Get(ctxKeyDenial).(string)

			httpRequestsTotal.WithLabelValues(method, path, code, reason).Inc()
			httpRequestDuration.WithLabelValues(method, path).Observe(dur.Seconds())

			// Skip the high-frequency CSI healthcheck noise. /metrics lives on
			// a separate listener (AGENT_METRICS_ADDR), so it never reaches us.
			if c.Request().URL.Path == "/healthz" {
				return err
			}

			l := accessLog(c.Request().Context(), status).
				Str("method", method).
				Str("path", c.Request().URL.Path).
				Int("code", status).
				Str("client", c.RealIP()).
				Str("took", dur.String())
			if ua := c.Request().UserAgent(); ua != "" {
				l = l.Str("user_agent", ua)
			}
			if reason != "" {
				l = l.Str("reason", reason)
			}
			l.Msg("request")

			return err
		}
	}
}

// accessLog picks the zerolog level for an access log line by status (5xx
// error, 4xx warn, else info) and returns an event from the per-request
// contextual logger so tenant/identity/token_fingerprint are inherited.
// Operators can silence successful requests with LOG_LEVEL=warn while
// keeping denials and server errors visible.
func accessLog(ctx context.Context, status int) *zerolog.Event {
	l := log.Ctx(ctx)
	switch {
	case status >= 500:
		return l.Error()
	case status >= 400:
		return l.Warn()
	default:
		return l.Info()
	}
}
