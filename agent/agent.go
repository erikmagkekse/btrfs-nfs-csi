package agent

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/model"

	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog/log"
)

type Agent struct {
	cfg     *model.AgentConfig
	version string
	commit  string
	echo    *echo.Echo
	ready   bool
}

func NewAgent(cfg *model.AgentConfig, version, commit string) *Agent {
	return &Agent{cfg: cfg, version: version, commit: commit}
}

func (a *Agent) Start(ctx context.Context) {
	e := echo.New()

	e.Use(v1.MetricsMiddleware())

	// build feature map
	features := map[string]string{
		"nfs_exporter": a.cfg.NFSExporter,
	}
	if a.cfg.QuotaEnabled {
		features["quota"] = "enabled"
	}
	if a.cfg.NFSReconcileInterval > 0 {
		features["nfs_reconcile"] = a.cfg.NFSReconcileInterval.String()
	}

	// unauthenticated endpoints
	e.GET("/healthz", v1.Healthz(a.version, a.commit, features))
	e.GET("/metrics", v1.MetricsHandler())

	// NFS exporter
	var exp nfs.Exporter
	switch a.cfg.NFSExporter {
	case "kernel":
		exp = nfs.NewKernelExporter(a.cfg.ExportfsBin)
	}

	// parse tenants
	tenants := parseTenants(a.cfg.Tenants)
	tenantNames := make([]string, 0, len(tenants))
	for _, name := range tenants {
		tenantNames = append(tenantNames, name)
	}

	// storage layer + handler
	store := storage.New(a.cfg.BasePath, a.cfg.QuotaEnabled, exp, tenantNames, a.cfg.DefaultDirMode, a.cfg.DefaultDataMode)
	h := &v1.Handler{Store: store}

	// v1 API with auth
	api := e.Group("/v1", v1.AuthMiddleware(tenants))

	api.POST("/volumes", h.CreateVolume)
	api.GET("/volumes", h.ListVolumes)
	api.PATCH("/volumes/:name", h.UpdateVolume)
	api.DELETE("/volumes/:name", h.DeleteVolume)

	api.POST("/volumes/:name/export", h.ExportVolume)
	api.DELETE("/volumes/:name/export", h.UnexportVolume)
	api.GET("/exports", h.ListExports)
	api.GET("/dashboard", v1.ServeDashboard(a.cfg.DashboardRefresh))

	api.GET("/stats", h.Stats)
	api.POST("/snapshots", h.CreateSnapshot)
	api.GET("/snapshots", h.ListSnapshots)
	api.DELETE("/snapshots/:name", h.DeleteSnapshot)

	api.POST("/clones", h.CreateClone)

	a.echo = e
	a.ready = true

	store.StartWorkers(ctx, a.cfg.UsageInterval, a.cfg.NFSReconcileInterval)

	go func() {
		var err error
		if a.cfg.TLSCert != "" && a.cfg.TLSKey != "" {
			s := &http.Server{
				Addr:    a.cfg.ListenAddr,
				Handler: e,
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			}
			log.Info().Str("addr", a.cfg.ListenAddr).Msg("starting agent with TLS")
			err = s.ListenAndServeTLS(a.cfg.TLSCert, a.cfg.TLSKey)
		} else {
			log.Warn().Str("addr", a.cfg.ListenAddr).Msg("starting agent without TLS - set AGENT_TLS_CERT and AGENT_TLS_KEY for production")
			err = e.Start(a.cfg.ListenAddr)
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("agent server failed")
		}
	}()
}

func (a *Agent) IsReady() bool {
	return a.ready
}

// parseTenants parses "name:token,name:token" into map[token]name.
// Returns nil if input is empty.
func parseTenants(s string) map[string]string {
	if s == "" {
		return nil
	}
	m := make(map[string]string)
	for _, entry := range strings.Split(s, ",") {
		parts := strings.SplitN(strings.TrimSpace(entry), ":", 2)
		if len(parts) == 2 {
			name := strings.TrimSpace(parts[0])
			token := strings.TrimSpace(parts[1])
			m[token] = name
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
