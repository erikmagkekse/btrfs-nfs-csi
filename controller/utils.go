package controller

import (
	"encoding/base64"
	"encoding/json"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
