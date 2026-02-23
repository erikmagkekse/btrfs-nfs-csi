package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"

	"github.com/rs/zerolog/log"
)

type AgentTracker struct {
	version string
	commit  string
	mu      sync.RWMutex
	agents  map[string]*agentAPI.Client
	scNames map[string]string // agentURL → SC name
	scToURL map[string]string // SC name → agentURL
}

func NewAgentTracker(version, commit string) *AgentTracker {
	return &AgentTracker{
		version: version,
		commit:  commit,
		agents:  make(map[string]*agentAPI.Client),
		scNames: make(map[string]string),
		scToURL: make(map[string]string),
	}
}

func (t *AgentTracker) StorageClass(agentURL string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if name, ok := t.scNames[agentURL]; ok {
		return name
	}
	return "unknown"
}

func (t *AgentTracker) AgentURL(scName string) (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if url, ok := t.scToURL[scName]; ok {
		return url, nil
	}
	return "", fmt.Errorf("no agent URL cached for storage class %q", scName)
}

func (t *AgentTracker) Track(url string, client *agentAPI.Client) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.agents[url] = client
}

func (t *AgentTracker) Run(ctx context.Context) {
	t.discoverFromStorageClasses(ctx)
	t.checkAll(ctx)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.discoverFromStorageClasses(ctx)
			t.checkAll(ctx)
		}
	}
}

func (t *AgentTracker) discoverFromStorageClasses(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	urlToSC, err := k8sDiscoverAgentURLs(checkCtx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to list StorageClasses")
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.scNames = urlToSC
	scToURL := make(map[string]string, len(urlToSC))
	for url, sc := range urlToSC {
		scToURL[sc] = url
	}
	t.scToURL = scToURL

	for url := range t.agents {
		if _, ok := urlToSC[url]; !ok {
			delete(t.agents, url)
			log.Info().Str("agent", url).Msg("agent removed - StorageClass deleted")
		}
	}
	for url := range urlToSC {
		if _, exists := t.agents[url]; !exists {
			t.agents[url] = agentAPI.NewClient(url, "")
			log.Info().Str("agent", url).Msg("discovered agent from StorageClass")
		}
	}
}

func (t *AgentTracker) checkAll(ctx context.Context) {
	t.mu.RLock()
	snapshot := make(map[string]*agentAPI.Client, len(t.agents))
	for url, c := range t.agents {
		snapshot[url] = c
	}
	t.mu.RUnlock()

	for url, c := range snapshot {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		health, err := c.Healthz(checkCtx)
		cancel()

		sc := t.StorageClass(url)

		if err != nil {
			agentHealthTotal.WithLabelValues("error", sc).Inc()
			log.Error().Err(err).Str("agent", url).Msg("agent health check failed")
			continue
		}

		if health.Version != t.version {
			agentHealthTotal.WithLabelValues("version_mismatch", sc).Inc()
			log.Warn().Str("agent", url).Str("agentVersion", health.Version).Str("driverVersion", t.version).Msg("agent/driver version mismatch")
		} else if health.Commit != t.commit {
			agentHealthTotal.WithLabelValues("healthy", sc).Inc()
			log.Info().Str("agent", url).Str("agentCommit", health.Commit).Str("driverCommit", t.commit).Msg("agent/driver version match, commit mismatch (could be security update)")
		} else {
			agentHealthTotal.WithLabelValues("healthy", sc).Inc()
			log.Info().Str("agent", url).Str("version", health.Version).Str("commit", health.Commit).Msg("agent healthy - vibes immaculate, bits aligned, absolutely bussin")
		}
	}
}
