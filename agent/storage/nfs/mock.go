package nfs

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type MockExporter struct {
	mock.Mock
}

func (m *MockExporter) Export(ctx context.Context, path string, client string) error {
	args := m.Called(ctx, path, client)
	return args.Error(0)
}

func (m *MockExporter) Unexport(ctx context.Context, path string, client string) error {
	args := m.Called(ctx, path, client)
	return args.Error(0)
}

func (m *MockExporter) ListExports(ctx context.Context) ([]ExportInfo, error) {
	args := m.Called(ctx)
	return args.Get(0).([]ExportInfo), args.Error(1)
}
