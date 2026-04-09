// @title           btrfs-nfs-csi Agent API
// @version         1.0
// @description     REST API for managing btrfs volumes, snapshots, clones, NFS exports, and background tasks.
// @host
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     Tenant token as "Bearer <token>"

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent"
	"github.com/erikmagkekse/btrfs-nfs-csi/integrations/kubernetes/controller"
	"github.com/erikmagkekse/btrfs-nfs-csi/integrations/kubernetes/driver"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
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

	app := &cli.Command{
		Name:  "btrfs-nfs-csi",
		Usage: "btrfs-nfs-csi storage driver",
		Description: `Turns any btrfs filesystem into a full-featured NFS storage backend with
instant snapshots, clones, and quotas. Includes a standalone REST agent,
a CLI, and several CSI integrations.

Docs:   https://github.com/erikmagkekse/btrfs-nfs-csi#readme
Issues: https://github.com/erikmagkekse/btrfs-nfs-csi/issues`,
		OnUsageError: func(_ context.Context, _ *cli.Command, err error, _ bool) error {
			return err
		},
		Flags: []cli.Flag{},
		Commands: append(
			withCLIHooks(
				volumeCmd(),
				snapshotCmd(),
				exportCmd(),
				taskCmd(),
				versionCmd(),
				statsCmd(),
				healthCmd(),
			),
			agentCmd(),
			integrationCmd(),
			controllerBackwardsCompatibilityWillBeRemovedInTheFuture(),
			driverBackwardsCompatibilityWillBeRemovedInTheFuture(),
		),
	}

	if err := app.Run(context.Background(), append([]string{"btrfs-nfs-csi"}, injectWatchDefault(os.Args[1:])...)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if timingLine != "" {
		_, _ = fmt.Fprintf(os.Stderr, "\n%s\n", timingLine)
	}
}

func agentCmd() *cli.Command {
	return &cli.Command{
		Name:     "agent",
		Usage:    "Start the agent",
		Category: "Server",
		Action: func(_ context.Context, _ *cli.Command) error {
			log.Info().Str("version", version).Str("commit", commit).Msg("starting btrfs-nfs-csi agent by Erik Groh <me@eriks.life> (https://github.com/erikmagkekse)")
			if err := agent.Run(version, commit); err != nil {
				log.Fatal().Err(err).Msg("agent failed")
			}
			return nil
		},
	}
}

func controllerBackwardsCompatibilityWillBeRemovedInTheFuture() *cli.Command {
	return &cli.Command{
		Name:   "controller",
		Hidden: true,
		Action: func(_ context.Context, _ *cli.Command) error {
			log.Warn().Msg("'controller' is deprecated and will be removed in a future release, use 'integration kubernetes controller' instead")
			log.Info().Str("version", version).Str("commit", commit).Str("integration", "kubernetes").Msg("starting CSI controller by Erik Groh <me@eriks.life> (https://github.com/erikmagkekse)")
			if err := controller.Start(version, commit); err != nil {
				log.Fatal().Err(err).Msg("controller failed")
			}
			return nil
		},
	}
}

func driverBackwardsCompatibilityWillBeRemovedInTheFuture() *cli.Command {
	return &cli.Command{
		Name:   "driver",
		Hidden: true,
		Action: func(_ context.Context, _ *cli.Command) error {
			log.Warn().Msg("'driver' is deprecated and will be removed in a future release, use 'integration kubernetes driver' instead")
			log.Info().Str("version", version).Str("commit", commit).Str("integration", "kubernetes").Msg("starting CSI node driver by Erik Groh <me@eriks.life> (https://github.com/erikmagkekse)")
			if err := driver.Start(version, commit); err != nil {
				log.Fatal().Err(err).Msg("driver failed")
			}
			return nil
		},
	}
}

func integrationCmd() *cli.Command {
	return &cli.Command{
		Name:        "integration",
		Aliases:     []string{"integrations", "int"},
		Usage:       "Platform integrations (kubernetes, ...)",
		Category:    "Server",
		Description: "Start platform-specific server components for integrations.",
		Commands: []*cli.Command{
			kubernetesCmd(),
		},
	}
}

func kubernetesCmd() *cli.Command {
	return &cli.Command{
		Name:    "kubernetes",
		Aliases: []string{"k8s"},
		Usage:   "Kubernetes integration",
		Description: `Dynamic provisioning of btrfs volumes and snapshots via PVCs.
Supports volume expansion, cloning, snapshots, NFS exports,
and multi-node access.

https://github.com/erikmagkekse/btrfs-nfs-csi
Author: Erik Groh <me@eriks.life>`,
		Commands: []*cli.Command{
			{
				Name:  "controller",
				Usage: "Start the CSI controller (ControllerServer)",
				Action: func(_ context.Context, _ *cli.Command) error {
					log.Info().Str("version", version).Str("commit", commit).Str("integration", "kubernetes").Msg("starting CSI controller by Erik Groh <me@eriks.life> (https://github.com/erikmagkekse)")
					if err := controller.Start(version, commit); err != nil {
						log.Fatal().Err(err).Msg("controller failed")
					}
					return nil
				},
			},
			{
				Name:  "driver",
				Usage: "Start the CSI node driver (NodeServer)",
				Action: func(_ context.Context, _ *cli.Command) error {
					log.Info().Str("version", version).Str("commit", commit).Str("integration", "kubernetes").Msg("starting CSI node driver by Erik Groh <me@eriks.life> (https://github.com/erikmagkekse)")
					if err := driver.Start(version, commit); err != nil {
						log.Fatal().Err(err).Msg("driver failed")
					}
					return nil
				},
			},
		},
	}
}
