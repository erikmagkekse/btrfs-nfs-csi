package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// StartNFSReconciler periodically removes NFS exports for volumes that no longer exist.
func (s *Storage) StartNFSReconciler(ctx context.Context, basePath string, interval time.Duration, tenant string) {
	go func() {
		s.reconcileExports(ctx, basePath, tenant)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.reconcileExports(ctx, basePath, tenant)
			}
		}
	}()
}

func (s *Storage) reconcileExports(ctx context.Context, basePath string, tenant string) {
	exports, err := s.exporter.ListExports(ctx)
	if err != nil {
		log.Error().Err(err).Msg("nfs reconciler: failed to list exports")
		return
	}

	// build actual exports: path → set of clients
	actualExports := map[string]map[string]bool{}
	var count int
	for _, e := range exports {
		if !strings.HasPrefix(e.Path, basePath+"/") {
			continue
		}
		count++
		if actualExports[e.Path] == nil {
			actualExports[e.Path] = map[string]bool{}
		}
		actualExports[e.Path][e.Client] = true
	}

	ExportsGauge.WithLabelValues(tenant).Set(float64(count))

	// remove orphaned exports (path no longer exists on disk)
	var removed int
	for path := range actualExports {
		if _, err := os.Stat(path); err == nil {
			continue
		}
		log.Warn().Str("path", path).Msg("nfs reconciler: removing orphaned export")
		if err := s.exporter.Unexport(ctx, path, ""); err != nil {
			log.Error().Err(err).Str("path", path).Msg("nfs reconciler: failed to remove export")
			continue
		}
		removed++
	}

	// re-add missing exports from metadata
	var restored int
	entries, err := os.ReadDir(basePath)
	if err != nil {
		log.Error().Err(err).Msg("nfs reconciler: failed to read base path")
		return
	}

	for _, e := range entries {
		if !e.IsDir() || e.Name() == SnapshotsDir {
			continue
		}
		volDir := filepath.Join(basePath, e.Name())
		metaPath := filepath.Join(volDir, MetadataFile)

		var meta VolumeMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}

		actual := actualExports[volDir]
		for _, client := range meta.Clients {
			if actual != nil && actual[client] {
				continue
			}
			log.Warn().Str("path", volDir).Str("client", client).Msg("nfs reconciler: re-exporting missing export")
			if err := s.exporter.Export(ctx, volDir, client); err != nil {
				log.Error().Err(err).Str("path", volDir).Str("client", client).Msg("nfs reconciler: failed to re-export")
				continue
			}
			restored++
		}
	}

	if removed > 0 || restored > 0 {
		log.Info().Str("tenant", tenant).Int("removed", removed).Int("restored", restored).Msg("nfs reconciler: reconciliation complete")
	}
}
