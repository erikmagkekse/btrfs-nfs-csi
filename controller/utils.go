package controller

import (
	"fmt"
	"strconv"
	"strings"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// paginate applies index-based pagination to a slice. Order is non-deterministic
// (map iteration) which may cause duplicates or skips across paginated requests.
// Acceptable for now since a single agent is not expected to host more than ~5k
// volumes and the project is targeting homelab or small prod environments.
func paginate[T any](entries []T, startingToken string, maxEntries int32) ([]T, string, error) {
	startIndex := 0
	if startingToken != "" {
		idx, err := strconv.Atoi(startingToken)
		if err != nil {
			return nil, "", status.Errorf(codes.Aborted, "invalid starting_token: %v", err)
		}
		startIndex = idx
	}
	if startIndex > len(entries) {
		startIndex = len(entries)
	}
	entries = entries[startIndex:]
	var nextToken string
	if maxEntries > 0 && int(maxEntries) < len(entries) {
		nextToken = strconv.Itoa(startIndex + int(maxEntries))
		entries = entries[:maxEntries]
	}
	return entries, nextToken, nil
}

const (
	paramAgentURL    = "agentURL"
	secretAgentToken = "agentToken"
)

func makeVolumeID(storageClass, name string) string {
	return storageClass + config.VolumeIDSep + name
}

func parseVolumeID(id string) (storageClass, name string, err error) {
	parts := strings.SplitN(id, config.VolumeIDSep, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid volume ID: %s", id)
	}
	return parts[0], parts[1], nil
}

func parseNodeIP(nodeID string) (string, error) {
	parts := strings.SplitN(nodeID, config.NodeIDSep, 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", fmt.Errorf("node ID %q missing IP (expected hostname%sip)", nodeID, config.NodeIDSep)
	}
	return parts[1], nil
}

func agentClientFromSecrets(agentURL string, secrets map[string]string) (*agentAPI.Client, error) {
	token := secrets[secretAgentToken]
	if token == "" {
		return nil, status.Error(codes.InvalidArgument, "missing agentToken secret")
	}
	return agentAPI.NewClient(agentURL, token), nil
}

func agentClientFromStorageClass(tracker *AgentTracker, scName string, secrets map[string]string) (*agentAPI.Client, error) {
	agentURL, err := tracker.AgentURL(scName)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "resolve agent for storage class %q: %v", scName, err)
	}
	return agentClientFromSecrets(agentURL, secrets)
}
