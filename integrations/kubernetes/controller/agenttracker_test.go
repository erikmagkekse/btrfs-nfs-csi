package controller

import (
	"sync"
	"testing"

	agentclient "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/client"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTracker() *AgentTracker {
	return &AgentTracker{
		agents:  make(map[string]*agentclient.Client),
		scToURL: make(map[string]string),
	}
}

func TestAgentTrackerClient(t *testing.T) {
	t.Run("returns_cached_client", func(t *testing.T) {
		tr := newTestTracker()
		c, err := agentclient.NewClient("http://agent:8080", "tok", config.IdentityK8sController)
		require.NoError(t, err)
		tr.agents["http://agent:8080"] = c
		tr.scToURL["my-sc"] = "http://agent:8080"

		got := tr.Client("my-sc")
		assert.Same(t, c, got)
	})

	t.Run("returns_nil_unknown_sc", func(t *testing.T) {
		tr := newTestTracker()
		assert.Nil(t, tr.Client("unknown"))
	})

	t.Run("returns_nil_sc_without_agent", func(t *testing.T) {
		tr := newTestTracker()
		tr.scToURL["orphan-sc"] = "http://gone:8080"

		assert.Nil(t, tr.Client("orphan-sc"))
	})
}

func TestAgentTrackerAgentURL(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		tr := newTestTracker()
		tr.scToURL["my-sc"] = "http://agent:8080"

		url, err := tr.AgentURL("my-sc")
		require.NoError(t, err)
		assert.Equal(t, "http://agent:8080", url)
	})

	t.Run("unknown", func(t *testing.T) {
		tr := newTestTracker()
		_, err := tr.AgentURL("missing")
		require.Error(t, err)
	})
}

func TestAgentTrackerTrack(t *testing.T) {
	tr := newTestTracker()
	c, err := agentclient.NewClient("http://agent:8080", "tok", config.IdentityK8sController)
	require.NoError(t, err)
	tr.Track("http://agent:8080", c)

	tr.mu.RLock()
	got := tr.agents["http://agent:8080"]
	tr.mu.RUnlock()
	assert.Same(t, c, got)
}

func TestAgentTrackerAgents(t *testing.T) {
	tr := newTestTracker()
	c1, err := agentclient.NewClient("http://a1:8080", "tok", config.IdentityK8sController)
	require.NoError(t, err)
	c2, err := agentclient.NewClient("http://a2:8080", "tok", config.IdentityK8sController)
	require.NoError(t, err)
	tr.agents["http://a1:8080"] = c1
	tr.agents["http://a2:8080"] = c2
	tr.scToURL["sc-1"] = "http://a1:8080"
	tr.scToURL["sc-2"] = "http://a2:8080"

	agents := tr.Agents()
	assert.Len(t, agents, 2)
	assert.Same(t, c1, agents["sc-1"])
	assert.Same(t, c2, agents["sc-2"])
}

func TestAgentTrackerConcurrent(t *testing.T) {
	tr := newTestTracker()
	c, err := agentclient.NewClient("http://agent:8080", "tok", config.IdentityK8sController)
	require.NoError(t, err)
	tr.agents["http://agent:8080"] = c
	tr.scToURL["my-sc"] = "http://agent:8080"

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(4)
		go func() {
			defer wg.Done()
			tr.Client("my-sc")
		}()
		go func() {
			defer wg.Done()
			_, _ = tr.AgentURL("my-sc")
		}()
		go func() {
			defer wg.Done()
			tr.Agents()
		}()
		go func() {
			defer wg.Done()
			tr.Track("http://agent:8080", c)
		}()
	}
	wg.Wait()
}

func TestAgentClientFromStorageClass(t *testing.T) {
	t.Run("uses_cached_client", func(t *testing.T) {
		tr := newTestTracker()
		c, err := agentclient.NewClient("http://agent:8080", "tok", config.IdentityK8sController)
		require.NoError(t, err)
		tr.agents["http://agent:8080"] = c
		tr.scToURL["my-sc"] = "http://agent:8080"

		got, err := agentClientFromStorageClass(tr, "my-sc", map[string]string{"agentToken": "tok"})
		require.NoError(t, err)
		assert.Same(t, c, got)
	})

	t.Run("falls_back_to_secrets", func(t *testing.T) {
		tr := newTestTracker()
		tr.scToURL["my-sc"] = "http://agent:8080"
		// no cached client in tr.agents

		got, err := agentClientFromStorageClass(tr, "my-sc", map[string]string{"agentToken": "tok"})
		require.NoError(t, err)
		assert.NotNil(t, got)
	})

	t.Run("unknown_sc", func(t *testing.T) {
		tr := newTestTracker()
		_, err := agentClientFromStorageClass(tr, "missing", map[string]string{"agentToken": "tok"})
		require.Error(t, err)
	})

	t.Run("missing_token", func(t *testing.T) {
		tr := newTestTracker()
		tr.scToURL["my-sc"] = "http://agent:8080"

		_, err := agentClientFromStorageClass(tr, "my-sc", map[string]string{})
		require.Error(t, err)
	})
}
