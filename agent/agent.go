package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/swagger"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/secret"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	env "github.com/caarlos0/env/v11"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	// Make log.Ctx(ctx) fall back to the global logger when no per-request
	// or per-worker logger has been put into the context. Storage code can
	// then use log.Ctx(ctx) unconditionally without checking origin.
	zerolog.DefaultContextLogger = &log.Logger
}

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
	creds, err := parseTenants(cfg.Tenants)
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("AGENT_TENANTS must contain at least one valid name:token pair")
	}
	nameSet := make(map[string]struct{}, len(creds))
	for _, c := range creds {
		nameSet[c.Info.Name] = struct{}{}
	}
	tenantNames := slices.Sorted(maps.Keys(nameSet))

	// NFS exporter
	var exp nfs.Exporter
	switch cfg.NFSExporter {
	case "kernel":
		exp = nfs.NewKernelExporter(cfg.ExportfsBin, cfg.KernelExportOptions)
	}

	// storage layer
	store, err := storage.New(storage.Config{
		BasePath:           cfg.BasePath,
		QuotaEnabled:       cfg.QuotaEnabled,
		Exporter:           exp,
		Tenants:            tenantNames,
		DefaultDirMode:     cfg.DefaultDirMode,
		DefaultDataMode:    cfg.DefaultDataMode,
		BtrfsBin:           cfg.BtrfsBin,
		ImmutableLabels:    cfg.ImmutableLabels,
		TaskMaxConcurrent:  cfg.TaskMaxConcurrent,
		TaskDefaultTimeout: cfg.TaskDefaultTimeout,
		TaskScrubTimeout:   cfg.TaskScrubTimeout,
		TaskBalanceTimeout: cfg.TaskBalanceTimeout,
		TaskPollInterval:   cfg.TaskPollInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}

	// echo + routes. Use a router that rejects auto-OPTIONS: Echo's default
	// OptionsMethodHandler returns 204 + Allow without running middleware,
	// letting unauthenticated callers enumerate the route table. We don't
	// serve browser clients, so dropping CORS preflight is safe.
	e := echo.NewWithConfig(echo.Config{
		Router: echo.NewRouter(echo.RouterConfig{
			OptionsMethodHandler: func(c *echo.Context) error {
				return c.NoContent(http.StatusMethodNotAllowed)
			},
		}),
	})
	e.Use(v1.LoggerMiddleware())             // place per-request logger in ctx; must run before Metrics + Auth
	e.Use(middleware.BodyLimit(1024 * 1024)) // 1MB
	e.Use(v1.MetricsMiddleware())

	metadataDir := filepath.Join(cfg.BasePath, config.MetadataDir)
	if err := os.MkdirAll(metadataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create metadata dir: %w", err)
	}
	if err := os.Chmod(metadataDir, 0o700); err != nil {
		return nil, fmt.Errorf("chmod metadata dir: %w", err)
	}
	secrets, err := secret.NewManager(metadataDir, "root_secret")
	if err != nil {
		return nil, fmt.Errorf("init agent secrets: %w", err)
	}
	tokens, err := v1.NewTokenSet(creds, secrets.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("init token set: %w", err)
	}
	h := &v1.Handler{Store: store, Tokens: tokens, DefaultPageLimit: cfg.DefaultPageLimit, PaginationSnapshotTTL: cfg.PaginationSnapshotTTL, PaginationMaxSnapshots: cfg.PaginationMaxSnapshots}

	// unauthenticated
	e.GET("/healthz", v1.Healthz(version, commit, store))
	e.GET("/", v1.ServeLanding(version, commit, cfg.SwaggerEnabled))
	if cfg.SwaggerEnabled {
		e.GET("/swagger.json", swagger.ServeSwaggerJSON())
	}

	// v1 API with auth
	api := e.Group("/v1", v1.AuthMiddleware(tokens))

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

	api.GET("/whoami", h.Whoami)
	api.GET("/tokens", h.ListTokens)

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

	srvErr, err := a.Start(ctx)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		log.Info().Msg("shutting down")
		return nil
	case err := <-srvErr:
		return fmt.Errorf("agent server failed: %w", err)
	}
}

// Start launches the metrics server, background workers, and the HTTP/HTTPS
// agent server. The listener is bound synchronously, so a bind failure is
// returned as an error. The returned channel emits a single error if the
// server exits unexpectedly (anything other than http.ErrServerClosed) and
// is closed once the server goroutine returns.
func (a *Agent) Start(ctx context.Context) (<-chan error, error) {
	startMetricsServer(a.cfg.MetricsAddr)

	a.store.StartWorkers(ctx, a.cfg.UsageInterval, a.cfg.NFSReconcileInterval, a.cfg.DeviceIOInterval, a.cfg.DeviceStatsInterval, a.cfg.TaskCleanupInterval)

	ln, err := net.Listen("tcp", a.cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("bind listen address %q: %w", a.cfg.ListenAddr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		var srvErr error
		// MaxHeaderBytes shrinks per-connection header buffer from Go's 1 MiB
		// default. ReadHeaderTimeout caps slowloris-style open connections
		// that drip-feed headers; without it, each such connection pins a
		// full MaxHeaderBytes buffer until the client decides to finish.
		const maxHeaderBytes = 64 * 1024
		const readHeaderTimeout = 10 * time.Second
		if a.cfg.TLSCert != "" && a.cfg.TLSKey != "" {
			s := &http.Server{
				Handler: a.echo,
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
				MaxHeaderBytes:    maxHeaderBytes,
				ReadHeaderTimeout: readHeaderTimeout,
			}
			log.Info().Str("addr", a.cfg.ListenAddr).Msg("starting agent with TLS")
			srvErr = s.ServeTLS(ln, a.cfg.TLSCert, a.cfg.TLSKey)
		} else {
			log.Warn().Str("addr", a.cfg.ListenAddr).Msg("starting agent without TLS - set AGENT_TLS_CERT and AGENT_TLS_KEY for production")
			s := &http.Server{
				Handler:           a.echo,
				MaxHeaderBytes:    maxHeaderBytes,
				ReadHeaderTimeout: readHeaderTimeout,
			}
			srvErr = s.Serve(ln)
		}
		if srvErr != nil && srvErr != http.ErrServerClosed {
			errCh <- srvErr
		}
	}()
	return errCh, nil
}

// parseTenants parses comma-separated `name:token[:role[:identity]]`
// entries. The token field may be a plaintext value or a bcrypt hash
// (`$2a$/$2b$/$2y$`); both are validated downstream by the auth package.
// Semantics are documented in docs/rbac.md.
func parseTenants(s string) ([]v1.TokenCredential, error) {
	if s == "" {
		return nil, nil
	}
	out := make([]v1.TokenCredential, 0)
	seen := make(map[string]string) // stored credential -> tenant name (for early dup error)
	for raw := range strings.SplitSeq(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ":")
		if len(parts) < 2 || len(parts) > 4 {
			return nil, fmt.Errorf("AGENT_TENANTS: invalid entry %q (expected name:token[:role[:identity]])", raw)
		}
		name := strings.TrimSpace(parts[0])
		tok := strings.TrimSpace(parts[1])
		if err := config.ValidateTenantName(name); err != nil {
			return nil, fmt.Errorf("AGENT_TENANTS: %w", err)
		}
		if tok == "" {
			return nil, fmt.Errorf("AGENT_TENANTS: empty token for tenant %q", name)
		}
		role := models.RoleAdmin
		if len(parts) >= 3 {
			switch r := models.TenantRole(strings.TrimSpace(parts[2])); r {
			case models.RoleReadonly, models.RoleMounter, models.RoleUser, models.RoleAdmin:
				role = r
			default:
				return nil, fmt.Errorf("AGENT_TENANTS: unknown role %q for tenant %q (expected %q, %q, %q, or %q)", parts[2], name, models.RoleReadonly, models.RoleMounter, models.RoleUser, models.RoleAdmin)
			}
		}
		var identity string
		if len(parts) == 4 {
			identity = strings.TrimSpace(parts[3])
			if role == models.RoleReadonly {
				return nil, fmt.Errorf("AGENT_TENANTS: identity does not apply to role %q (tenant %q)", models.RoleReadonly, name)
			}
			if err := config.ValidateIdentity(identity); err != nil {
				return nil, fmt.Errorf("AGENT_TENANTS: %w for tenant %q", err, name)
			}
		}
		if existing, dup := seen[tok]; dup {
			return nil, fmt.Errorf("AGENT_TENANTS: duplicate token shared by tenants %q and %q", existing, name)
		}
		seen[tok] = name
		out = append(out, v1.TokenCredential{
			Stored: tok,
			Info:   models.TenantInfo{Name: name, Role: role, Identity: identity},
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
