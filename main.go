package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/controller"
	"github.com/erikmagkekse/btrfs-nfs-csi/driver"

	"github.com/caarlos0/env/v11"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	level := zerolog.InfoLevel
	if l := os.Getenv("LOG_LEVEL"); l != "" {
		if parsed, err := zerolog.ParseLevel(l); err == nil {
			level = parsed
		}
	}
	zerolog.SetGlobalLevel(level)
	log.Logger = zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}).With().Timestamp().Logger()

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "agent":
		runAgent()
	case "controller":
		runController()
	case "driver":
		runDriver()
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: %s <command>

Commands:
  agent        Start the btrfs-nfs-csi agent
  controller   Start the CSI controller
  driver       Start the CSI node driver
`, os.Args[0])
}

func runAgent() {
	log.Info().Str("version", version).Str("commit", commit).Msg("starting btrfs-nfs-csi agent")

	cfg, err := env.ParseAs[config.AgentConfig]()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse agent config")
	}

	if err := os.MkdirAll(cfg.BasePath, 0755); err != nil {
		log.Fatal().Err(err).Msg("failed to create base path")
	}
	if err := os.MkdirAll(cfg.BasePath+"/snapshots", 0755); err != nil {
		log.Fatal().Err(err).Msg("failed to create snapshots directory")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a := agent.NewAgent(&cfg, version, commit)
	a.Start(ctx)

	<-ctx.Done()
	log.Info().Msg("shutting down")
}

func runController() {
	log.Info().Str("version", version).Str("commit", commit).Msg("starting btrfs-nfs-csi controller")

	cfg, err := env.ParseAs[config.ControllerConfig]()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse controller config")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := controller.Start(ctx, cfg.Endpoint, cfg.MetricsAddr, version, commit); err != nil {
		log.Fatal().Err(err).Msg("controller failed")
	}
}

func runDriver() {
	log.Info().Str("version", version).Str("commit", commit).Msg("starting btrfs-nfs-csi driver")

	cfg, err := env.ParseAs[config.NodeConfig]()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse node config")
	}

	nodeIP, err := driver.ResolveNodeIP(cfg.NodeIP, cfg.StorageInterface, cfg.StorageCIDR)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to resolve node IP")
	}
	log.Info().Str("nodeIP", nodeIP).Msg("resolved storage IP")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := driver.Start(ctx, cfg.Endpoint, cfg.NodeID, nodeIP, cfg.MetricsAddr, version); err != nil {
		log.Fatal().Err(err).Msg("node failed")
	}
}
