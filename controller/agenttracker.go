package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/k8s"
	"github.com/erikmagkekse/btrfs-nfs-csi/model"

	"github.com/rs/zerolog/log"
)

// Just a prive thinggy here
type agentInfo struct {
	scName   string
	agentURL string
	token    string
}

type AgentTracker struct {
	version string
	commit  string
	mu      sync.RWMutex
	agents  map[string]*agentAPI.Client
	scNames map[string]string // agentURL -> SC name
	scToURL map[string]string // SC name -> agentURL
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

func (t *AgentTracker) Agents() map[string]*agentAPI.Client {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]*agentAPI.Client, len(t.scToURL))
	for sc, url := range t.scToURL {
		if c, ok := t.agents[url]; ok {
			result[sc] = c
		}
	}
	return result
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

// discoverAgents returns agent info for all StorageClasses owned by our driver.
func discoverAgents(ctx context.Context) ([]agentInfo, error) {
	scList, err := k8s.ListStorageClasses(ctx, model.DriverName)
	if err != nil {
		ctrlK8sOpsTotal.WithLabelValues("error").Inc()
		return nil, err
	}
	ctrlK8sOpsTotal.WithLabelValues("success").Inc()

	var result []agentInfo
	for _, sc := range scList {
		url := sc.Parameters[paramAgentURL]
		if url == "" {
			continue
		}

		token := resolveAgentToken(ctx, sc.Parameters)

		result = append(result, agentInfo{
			scName:   sc.Metadata.Name,
			agentURL: url,
			token:    token,
		})
	}
	return result, nil
}

// resolveAgentToken reads the agentToken from the K8s Secret referenced by SC parameters.
func resolveAgentToken(ctx context.Context, params map[string]string) string {
	name := params["csi.storage.k8s.io/provisioner-secret-name"]
	ns := params["csi.storage.k8s.io/provisioner-secret-namespace"]
	if name == "" || ns == "" {
		return ""
	}

	token, err := k8s.GetSecretValue(ctx, ns, name, secretAgentToken)
	if err != nil {
		log.Warn().Err(err).Str("secret", ns+"/"+name).Msg("failed to read agent secret")
		return ""
	}
	return token
}

func (t *AgentTracker) discoverFromStorageClasses(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	scList, err := discoverAgents(checkCtx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to list StorageClasses")
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	scNames := make(map[string]string, len(scList))
	scToURL := make(map[string]string, len(scList))
	known := make(map[string]bool, len(scList))
	for _, a := range scList {
		scNames[a.agentURL] = a.scName
		scToURL[a.scName] = a.agentURL
		known[a.agentURL] = true

		if _, exists := t.agents[a.agentURL]; !exists {
			t.agents[a.agentURL] = agentAPI.NewClient(a.agentURL, a.token)
			log.Info().Str("agent", a.agentURL).Str("sc", a.scName).Msg("discovered agent from StorageClass")
		}
	}
	t.scNames = scNames
	t.scToURL = scToURL

	for url := range t.agents {
		if !known[url] {
			delete(t.agents, url)
			log.Info().Str("agent", url).Msg("agent removed - StorageClass deleted")
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
