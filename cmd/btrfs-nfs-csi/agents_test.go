package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTempConfigDir points os.UserHomeDir at a temp dir for the test and
// clears any explicit path override so the default location is exercised.
func withTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("BTRFS_NFS_CSI_CONFIG_FILE", "")
	return dir
}

func TestLoadConfig_MissingFileReturnsEmptyConfig(t *testing.T) {
	withTempConfigDir(t)
	cfg, err := loadConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.CurrentAgent)
	assert.Empty(t, cfg.Agents)
	_, ok := cfg.Active()
	assert.False(t, ok, "no current set, Active should report false")
}

func TestConfig_SaveLoadRoundTrip(t *testing.T) {
	withTempConfigDir(t)

	want := &Config{
		CurrentAgent: "prod",
		Agents: map[string]Agent{
			"prod": {
				URL: "https://agent:8080", Token: "s3cret", Identity: "cli",
				Tenant: "team-a", Role: models.RoleAdmin, Fingerprint: "abc123def456",
			},
			"dev": {URL: "http://dev:8080", Token: "devtok"},
		},
	}
	require.NoError(t, want.save())

	got, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, want.CurrentAgent, got.CurrentAgent)
	assert.Equal(t, want.Agents, got.Agents)

	active, ok := got.Active()
	require.True(t, ok)
	assert.Equal(t, "https://agent:8080", active.URL)
}

func TestConfig_SaveSetsRestrictivePermissions(t *testing.T) {
	configHome := withTempConfigDir(t)
	cfg := &Config{Agents: map[string]Agent{"a": {URL: "u", Token: "t"}}}
	require.NoError(t, cfg.save())

	path := filepath.Join(configHome, ".btrfs-nfs-csi", "config.json")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "config.json must be 0600 (holds bearer tokens)")

	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "config dir must be 0700")
}

func TestConfigPath_OverrideEnvWins(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-config.json")
	t.Setenv("BTRFS_NFS_CSI_CONFIG_FILE", custom)
	got, err := configPath()
	require.NoError(t, err)
	assert.Equal(t, custom, got, "BTRFS_NFS_CSI_CONFIG_FILE must take precedence over $HOME default")
}

func TestConfigPath_DefaultsToHomeDotdir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BTRFS_NFS_CSI_CONFIG_FILE", "")
	got, err := configPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".btrfs-nfs-csi", "config.json"), got)
}

func TestAgent_TLSSkipVerifyRoundTrips(t *testing.T) {
	withTempConfigDir(t)
	want := &Config{
		CurrentAgent: "prod",
		Agents: map[string]Agent{
			"prod": {URL: "https://agent:8443", Token: "t", TLSSkipVerify: true},
			"dev":  {URL: "https://agent:8443", Token: "t"},
		},
	}
	require.NoError(t, want.save())
	got, err := loadConfig()
	require.NoError(t, err)
	assert.True(t, got.Agents["prod"].TLSSkipVerify, "skip flag must persist for prod")
	assert.False(t, got.Agents["dev"].TLSSkipVerify, "dev kept default verify")
}

func TestTLSLabel(t *testing.T) {
	assert.Equal(t, "skip", tlsLabel(true))
	assert.Equal(t, "verify", tlsLabel(false))
}

func TestConfig_ActiveReturnsFalseWhenCurrentAgentMissing(t *testing.T) {
	cfg := &Config{
		CurrentAgent: "ghost",
		Agents:  map[string]Agent{"prod": {URL: "u", Token: "t"}},
	}
	_, ok := cfg.Active()
	assert.False(t, ok, "Current names an agent that does not exist")
}

func TestDiffAgent(t *testing.T) {
	cached := Agent{Tenant: "team-a", Role: models.RoleAdmin, Fingerprint: "abc123def456"}

	t.Run("identical returns empty", func(t *testing.T) {
		live := &models.WhoamiResponse{Tenant: "team-a", Role: models.RoleAdmin, Fingerprint: "abc123def456"}
		assert.Empty(t, diffAgent(cached, live))
	})

	t.Run("role drift reported", func(t *testing.T) {
		live := &models.WhoamiResponse{Tenant: "team-a", Role: models.RoleUser, Fingerprint: "abc123def456"}
		got := diffAgent(cached, live)
		assert.Contains(t, got, "role: cached=admin, live=user")
		assert.NotContains(t, got, "tenant")
		assert.NotContains(t, got, "fingerprint")
	})

	t.Run("multiple drifts joined", func(t *testing.T) {
		live := &models.WhoamiResponse{Tenant: "team-b", Role: models.RoleUser, Fingerprint: "xx99yy88zz77"}
		got := diffAgent(cached, live)
		assert.Contains(t, got, "tenant:")
		assert.Contains(t, got, "role:")
		assert.Contains(t, got, "fingerprint:")
		assert.Contains(t, got, "; ", "diffs should be semicolon-joined")
	})
}
