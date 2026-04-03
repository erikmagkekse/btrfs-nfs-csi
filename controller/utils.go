package controller

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// pageToken encodes/decodes a cursor for multi-agent pagination.
// Format: base64(json({"sc":"storageclass","after":"last_name"}))
type pageToken struct {
	SC    string `json:"sc"`
	After string `json:"after"`
}

func encodePageToken(sc, after string) string {
	data, _ := json.Marshal(pageToken{SC: sc, After: after})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodePageToken(token string) (pageToken, error) {
	var pt pageToken
	if token == "" {
		return pt, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return pt, status.Errorf(codes.Aborted, "invalid starting_token: %v", err)
	}
	if err := json.Unmarshal(data, &pt); err != nil {
		return pt, status.Errorf(codes.Aborted, "invalid starting_token: %v", err)
	}
	return pt, nil
}

const (
	paramAgentURL    = "agentURL"
	secretAgentToken = "agentToken"
)

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
