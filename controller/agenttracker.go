package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/csiserver"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/rs/zerolog/log"
)

// Just a prive thinggy here
type agentInfo struct {
	scName   string
	agentURL string
	token    string
}

type AgentTracker struct {
	kubeClient kubernetes.Interface
	version    string
	commit     string
	mu         sync.RWMutex
	agents     map[string]*agentAPI.Client
	scToURL    map[string]string // SC name -> agentURL
}

func NewAgentTracker(kubeClient kubernetes.Interface, version, commit string) *AgentTracker {
	return &AgentTracker{
		kubeClient: kubeClient,
		version:    version,
		commit:     commit,
		agents:     make(map[string]*agentAPI.Client),
		scToURL:    make(map[string]string),
	}
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
func (t *AgentTracker) discoverAgents(ctx context.Context) ([]agentInfo, error) {
	scList, err := t.kubeClient.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		ctrlK8sOpsTotal.WithLabelValues("error").Inc()
		return nil, err
	}
	ctrlK8sOpsTotal.WithLabelValues("success").Inc()

	var result []agentInfo
	for _, sc := range scList.Items {
		if sc.Provisioner != csiserver.DriverName {
			continue
		}
		url := sc.Parameters[paramAgentURL]
		if url == "" {
			continue
		}
		result = append(result, agentInfo{
			scName:   sc.Name,
			agentURL: url,
			token:    t.resolveAgentToken(ctx, sc.Parameters),
		})
	}
	return result, nil
}

// resolveAgentToken reads the agentToken from the K8s Secret referenced by SC parameters.
func (t *AgentTracker) resolveAgentToken(ctx context.Context, params map[string]string) string {
	name := params[csiserver.SecretNameKey]
	ns := params[csiserver.SecretNamespaceKey]
	if name == "" || ns == "" {
		return ""
	}

	secret, err := t.kubeClient.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.Warn().Err(err).Str("secret", ns+"/"+name).Msg("failed to read agent secret")
		return ""
	}
	return string(secret.Data[secretAgentToken])
}

func (t *AgentTracker) discoverFromStorageClasses(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	scList, err := t.discoverAgents(checkCtx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to list StorageClasses")
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	scToURL := make(map[string]string, len(scList))
	known := make(map[string]bool, len(scList))
	for _, a := range scList {
		scToURL[a.scName] = a.agentURL
		known[a.agentURL] = true

		if _, exists := t.agents[a.agentURL]; !exists {
			t.agents[a.agentURL] = agentAPI.NewClient(a.agentURL, a.token)
			log.Info().Str("agent", a.agentURL).Str("sc", a.scName).Msg("discovered agent from StorageClass")
		}
	}
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
	snapshot := make(map[string]*agentAPI.Client, len(t.scToURL))
	for sc, url := range t.scToURL {
		if c, ok := t.agents[url]; ok {
			snapshot[sc] = c
		}
	}
	t.mu.RUnlock()

	for sc, c := range snapshot {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		start := time.Now()
		health, err := c.Healthz(checkCtx)
		agentDuration.WithLabelValues("health_check", sc).Observe(time.Since(start).Seconds())
		cancel()

		if err != nil {
			agentOpsTotal.WithLabelValues("health_check", "error", sc).Inc()
			log.Error().Err(err).Str("sc", sc).Msg("agent health check failed")
			continue
		}

		switch {
		case health.Status == agentAPI.HealthStatusDegraded:
			agentOpsTotal.WithLabelValues("health_check", "degraded", sc).Inc()
			log.Warn().Str("sc", sc).Msg("agent reports degraded status (missing device or btrfs errors)")
		case health.Version != t.version:
			agentOpsTotal.WithLabelValues("health_check", "version_mismatch", sc).Inc()
			log.Warn().Str("sc", sc).Str("agentVersion", health.Version).Str("driverVersion", t.version).Msg("agent/driver version mismatch")
		case health.Commit != t.commit:
			agentOpsTotal.WithLabelValues("health_check", "healthy", sc).Inc()
			log.Info().Str("sc", sc).Str("agentCommit", health.Commit).Str("driverCommit", t.commit).Msg("agent healthy - commit mismatch, but same version (could be a security update)")
		default:
			agentOpsTotal.WithLabelValues("health_check", "healthy", sc).Inc()
			log.Info().Str("sc", sc).Str("version", health.Version).Str("commit", health.Commit).Msg("agent healthy - vibes immaculate, bits aligned, absolutely bussin")
		}
	}
}
