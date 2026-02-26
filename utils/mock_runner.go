package utils

import "context"

// MockRunner records calls and returns preconfigured responses.
// Use this in tests to avoid real shell execution.
// Set RunFn for dynamic per-call responses, otherwise Out/Err are returned.
type MockRunner struct {
	Calls [][]string
	Out   string
	Err   error
	RunFn func(args []string) (string, error)
}

func (m *MockRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	m.Calls = append(m.Calls, args)
	if m.RunFn != nil {
		return m.RunFn(args)
	}
	return m.Out, m.Err
}
