package models

import (
	"fmt"
	"net/http"
)

// AgentError represents an HTTP error response from the agent API.
type AgentError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *AgentError) Error() string {
	return fmt.Sprintf("agent error %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

// IsConflict reports whether err is a 409 Conflict response.
func IsConflict(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusConflict
	}
	return false
}

// IsNotFound reports whether err is a 404 Not Found response.
func IsNotFound(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusNotFound
	}
	return false
}

// IsLocked reports whether err is a 423 Locked response (e.g. volume has active exports).
func IsLocked(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusLocked
	}
	return false
}
