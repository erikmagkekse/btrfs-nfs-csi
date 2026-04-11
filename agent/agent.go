package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/swagger"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	env "github.com/caarlos0/env/v11"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/rs/zerolog/log"
)

type Agent struct {
	cfg     *config.AgentConfig
	version string
	commit  string
	echo    *echo.Echo
	store   *storage.Storage
}

func NewAgent(cfg *config.AgentConfig, version, commit string) (*Agent, error) {
	// validate TLS config
	if (cfg.TLSCert != "") != (cfg.TLSKey != "") {
		return nil, fmt.Errorf("both AGENT_TLS_CERT and AGENT_TLS_KEY must be set, or neither")
	}

	// parse tenants
	tenants := parseTenants(cfg.Tenants)
	if tenants == nil {
		return nil, fmt.Errorf("AGENT_TENANTS must contain at least one valid name:token pair")
	}
	tenantNames := make([]string, 0, len(tenants))
	for _, name := range tenants {
		tenantNames = append(tenantNames, name)
	}

	// NFS exporter
	var exp nfs.Exporter
	switch cfg.NFSExporter {
	case "kernel":
		exp = nfs.NewKernelExporter(cfg.ExportfsBin, cfg.KernelExportOptions)
	}

	// storage layer
	store := storage.New(
		cfg.BasePath, cfg.QuotaEnabled, exp, tenantNames,
		cfg.DefaultDirMode, cfg.DefaultDataMode, cfg.BtrfsBin, cfg.ImmutableLabels,
		cfg.TaskMaxConcurrent, cfg.TaskDefaultTimeout, cfg.TaskScrubTimeout, cfg.TaskPollInterval,
	)

	// echo + routes
	e := echo.New()
	e.Use(middleware.BodyLimit(1024 * 1024)) // 1MB
	e.Use(v1.MetricsMiddleware())

	h := &v1.Handler{Store: store, DefaultPageLimit: cfg.DefaultPageLimit, PaginationSnapshotTTL: cfg.PaginationSnapshotTTL, PaginationMaxSnapshots: cfg.PaginationMaxSnapshots}

	// unauthenticated
	e.GET("/healthz", v1.Healthz(version, commit, store))
	e.GET("/", v1.ServeLanding(version, commit, cfg.SwaggerEnabled))
	if cfg.SwaggerEnabled {
		e.GET("/swagger.json", swagger.ServeSwaggerJSON())
	}

	// v1 API with auth
	api := e.Group("/v1", v1.AuthMiddleware(tenants))

	api.POST("/volumes", h.CreateVolume)
	api.GET("/volumes", h.ListVolumes)
	api.GET("/volumes/:name", h.GetVolume)
	api.PATCH("/volumes/:name", h.UpdateVolume)
	api.DELETE("/volumes/:name", h.DeleteVolume)

	api.GET("/volumes/:name/snapshots", h.ListVolumeSnapshots)
	api.POST("/volumes/:name/export", h.CreateVolumeExport)
	api.DELETE("/volumes/:name/export", h.DeleteVolumeExport)
	api.GET("/exports", h.ListVolumeExports)

	api.GET("/stats", h.Stats)
	api.POST("/snapshots", h.CreateSnapshot)
	api.GET("/snapshots", h.ListSnapshots)
	api.GET("/snapshots/:name", h.GetSnapshot)
	api.DELETE("/snapshots/:name", h.DeleteSnapshot)

	api.POST("/clones", h.CreateClone)
	api.POST("/volumes/clone", h.CloneVolume)

	api.GET("/tasks", h.ListTasks)
	api.POST("/tasks/:type", h.CreateTask)
	api.GET("/tasks/:id", h.GetTask)
	api.DELETE("/tasks/:id", h.CancelTask)

	return &Agent{
		cfg:     cfg,
		version: version,
		commit:  commit,
		echo:    e,
		store:   store,
	}, nil
}

func Run(version, commit string) error {
	cfg, err := env.ParseAs[config.AgentConfig]()
	if err != nil {
		return fmt.Errorf("parse agent config: %w", err)
	}

	a, err := NewAgent(&cfg, version, commit)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a.Start(ctx)

	<-ctx.Done()
	log.Info().Msg("shutting down")
	return nil
}

func (a *Agent) Start(ctx context.Context) {
	startMetricsServer(a.cfg.MetricsAddr)

	a.store.StartWorkers(ctx, a.cfg.UsageInterval, a.cfg.NFSReconcileInterval, a.cfg.DeviceIOInterval, a.cfg.DeviceStatsInterval, a.cfg.TaskCleanupInterval)

	ln, err := net.Listen("tcp", a.cfg.ListenAddr)
	if err != nil {
		log.Fatal().Err(err).Str("addr", a.cfg.ListenAddr).Msg("failed to bind listen address")
	}

	go func() {
		var srvErr error
		if a.cfg.TLSCert != "" && a.cfg.TLSKey != "" {
			s := &http.Server{
				Handler: a.echo,
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			}
			log.Info().Str("addr", a.cfg.ListenAddr).Msg("starting agent with TLS")
			srvErr = s.ServeTLS(ln, a.cfg.TLSCert, a.cfg.TLSKey)
		} else {
			log.Warn().Str("addr", a.cfg.ListenAddr).Msg("starting agent without TLS - set AGENT_TLS_CERT and AGENT_TLS_KEY for production")
			s := &http.Server{Handler: a.echo}
			srvErr = s.Serve(ln)
		}
		if srvErr != nil && srvErr != http.ErrServerClosed {
			log.Fatal().Err(srvErr).Msg("agent server failed")
		}
	}()
}

// parseTenants parses "name:token,name:token" into map[token]name.
// Returns nil if input is empty.
func parseTenants(s string) map[string]string {
	if s == "" {
		return nil
	}
	m := make(map[string]string)
	for entry := range strings.SplitSeq(s, ",") {
		name, tok, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if ok {
			name = strings.TrimSpace(name)
			tok = strings.TrimSpace(tok)
			m[tok] = name
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
