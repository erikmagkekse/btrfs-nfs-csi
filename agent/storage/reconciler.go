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
func (s *Storage) startNFSReconciler(ctx context.Context, interval time.Duration, tenant string) {
	go func() {
		s.reconcileExports(ctx, tenant)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.reconcileExports(ctx, tenant)
			}
		}
	}()
}

func (s *Storage) reconcileExports(ctx context.Context, tenant string) {
	tenantDir := filepath.Join(s.basePath, tenant)

	exports, err := s.exporter.ListExports(ctx)
	if err != nil {
		log.Error().Err(err).Msg("nfs reconciler: failed to list exports")
		return
	}

	// build actual exports: path -> set of clients
	actualExports := map[string]map[string]bool{}
	var count int
	for _, e := range exports {
		if !strings.HasPrefix(e.Path, tenantDir+"/") {
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
	s.volumes.Range(func(t, name string, meta *VolumeMetadata) bool {
		if t != tenant {
			return true
		}
		volDir := s.volumes.Dir(tenant, name)
		actual := actualExports[volDir]
		for _, ip := range uniqueExportIPs(meta.Exports) {
			if actual != nil && actual[ip] {
				continue
			}
			log.Warn().Str("path", volDir).Str("client", ip).Msg("nfs reconciler: re-exporting missing export")
			if err := s.exporter.Export(ctx, volDir, ip); err != nil {
				log.Error().Err(err).Str("path", volDir).Str("client", ip).Msg("nfs reconciler: failed to re-export")
				continue
			}
			restored++
		}
		return true
	})

	if removed > 0 || restored > 0 {
		log.Info().Str("tenant", tenant).Int("removed", removed).Int("restored", restored).Msg("nfs reconciler: reconciliation complete")
	} else {
		log.Debug().Str("tenant", tenant).Msg("nfs reconciler: in sync")
	}
}
