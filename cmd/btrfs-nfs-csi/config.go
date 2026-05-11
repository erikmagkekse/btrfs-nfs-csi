package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the on-disk CLI config. Flat top-level keys leave room for
// future settings (default_output, aliases, ...) without restructuring.
type Config struct {
	CurrentAgent string           `json:"current_agent,omitempty"`
	Agents       map[string]Agent `json:"agents,omitempty"`
}

func (c *Config) Active() (Agent, bool) {
	if c == nil || c.CurrentAgent == "" {
		return Agent{}, false
	}
	a, ok := c.Agents[c.CurrentAgent]
	return a, ok
}

// configPath honours BTRFS_NFS_CSI_CONFIG_FILE so tests and per-project
// profiles can redirect away from the default $HOME location.
func configPath() (string, error) {
	if override := os.Getenv("BTRFS_NFS_CSI_CONFIG_FILE"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".btrfs-nfs-csi", "config.json"), nil
}

// loadConfig treats a missing file as an empty config so first-run is
// indistinguishable from "no agents configured".
func loadConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{Agents: map[string]Agent{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Agents == nil {
		c.Agents = map[string]Agent{}
	}
	return &c, nil
}

// save writes atomically with 0600 + 0700 because the file holds bearer
// tokens in plaintext.
func (c *Config) save() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	// rename is atomic, but without fsync the new bytes can be lost on
	// power loss while the directory entry already points at them.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	cleanup = false
	return nil
}
