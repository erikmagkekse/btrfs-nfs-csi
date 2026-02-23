package controller

import (
	"fmt"
	"strings"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const volumeIDSep = "|"

const (
	paramAgentURL    = "agentURL"
	secretAgentToken = "agentToken"
)

func makeVolumeID(storageClass, name string) string {
	return storageClass + volumeIDSep + name
}

func parseVolumeID(id string) (storageClass, name string, err error) {
	parts := strings.SplitN(id, volumeIDSep, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid volume ID: %s", id)
	}
	return parts[0], parts[1], nil
}

func parseNodeIP(nodeID string) (string, error) {
	parts := strings.SplitN(nodeID, "|", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", fmt.Errorf("node ID %q missing IP (expected hostname|ip)", nodeID)
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
