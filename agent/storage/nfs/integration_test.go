//go:build integration

package nfs

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/suite"
)

type KernelExporterSuite struct {
	suite.Suite
	exp Exporter
	ctx context.Context
	dir string
}

func TestKernelExporterIntegration(t *testing.T) {
	suite.Run(t, new(KernelExporterSuite))
}

func (s *KernelExporterSuite) SetupSuite() {
	bin, err := exec.LookPath("exportfs")
	s.Require().NoError(err, "exportfs not found")
	s.exp = NewKernelExporter(bin, "rw,nohide,crossmnt,no_root_squash,no_subtree_check")
	s.ctx = context.Background()
}

func (s *KernelExporterSuite) SetupTest() {
	s.dir = s.T().TempDir()
}

func (s *KernelExporterSuite) TearDownTest() {
	_ = s.exp.Unexport(s.ctx, s.dir, "")
}

func (s *KernelExporterSuite) TestExportAndList() {
	err := s.exp.Export(s.ctx, s.dir, "127.0.0.1")
	s.Require().NoError(err, "Export")

	exports, err := s.exp.ListExports(s.ctx)
	s.Require().NoError(err, "ListExports")

	s.Assert().True(containsExport(exports, s.dir, "127.0.0.1"),
		"expected %s for 127.0.0.1 in exports: %v", s.dir, exports)
}

func (s *KernelExporterSuite) TestExportMultipleClients() {
	err := s.exp.Export(s.ctx, s.dir, "127.0.0.1")
	s.Require().NoError(err, "Export client 1")

	err = s.exp.Export(s.ctx, s.dir, "127.0.0.2")
	s.Require().NoError(err, "Export client 2")

	exports, err := s.exp.ListExports(s.ctx)
	s.Require().NoError(err, "ListExports")

	s.Assert().True(containsExport(exports, s.dir, "127.0.0.1"),
		"expected 127.0.0.1 in exports: %v", exports)
	s.Assert().True(containsExport(exports, s.dir, "127.0.0.2"),
		"expected 127.0.0.2 in exports: %v", exports)
}

func (s *KernelExporterSuite) TestUnexportSingleClient() {
	err := s.exp.Export(s.ctx, s.dir, "127.0.0.1")
	s.Require().NoError(err, "Export client 1")

	err = s.exp.Export(s.ctx, s.dir, "127.0.0.2")
	s.Require().NoError(err, "Export client 2")

	err = s.exp.Unexport(s.ctx, s.dir, "127.0.0.1")
	s.Require().NoError(err, "Unexport client 1")

	exports, err := s.exp.ListExports(s.ctx)
	s.Require().NoError(err, "ListExports")

	s.Assert().False(containsExport(exports, s.dir, "127.0.0.1"),
		"127.0.0.1 should be removed: %v", exports)
	s.Assert().True(containsExport(exports, s.dir, "127.0.0.2"),
		"127.0.0.2 should still be exported: %v", exports)
}

func (s *KernelExporterSuite) TestUnexportAllClients() {
	err := s.exp.Export(s.ctx, s.dir, "127.0.0.1")
	s.Require().NoError(err, "Export client 1")

	err = s.exp.Export(s.ctx, s.dir, "127.0.0.2")
	s.Require().NoError(err, "Export client 2")

	err = s.exp.Unexport(s.ctx, s.dir, "")
	s.Require().NoError(err, "Unexport all")

	exports, err := s.exp.ListExports(s.ctx)
	s.Require().NoError(err, "ListExports")

	s.Assert().False(containsExport(exports, s.dir, "127.0.0.1"),
		"127.0.0.1 should be removed: %v", exports)
	s.Assert().False(containsExport(exports, s.dir, "127.0.0.2"),
		"127.0.0.2 should be removed: %v", exports)
}

func (s *KernelExporterSuite) TestUnexportNotFound() {
	err := s.exp.Unexport(s.ctx, s.dir, "127.0.0.1")
	s.Assert().NoError(err, "Unexport on non-exported path should not error")
}

func (s *KernelExporterSuite) TestExportIdempotent() {
	err := s.exp.Export(s.ctx, s.dir, "127.0.0.1")
	s.Require().NoError(err, "Export first call")

	err = s.exp.Export(s.ctx, s.dir, "127.0.0.1")
	s.Require().NoError(err, "Export second call (idempotent)")

	exports, err := s.exp.ListExports(s.ctx)
	s.Require().NoError(err, "ListExports")

	count := 0
	for _, e := range exports {
		if e.Path == s.dir && e.Client == "127.0.0.1" {
			count++
		}
	}
	s.Assert().Equal(1, count,
		"expected exactly 1 export entry, got %d in: %v", count, exports)
}

func (s *KernelExporterSuite) TestExportLongPath() {
	// Create a deeply nested path that exceeds 64 characters, which causes
	// exportfs -v to wrap the output across two lines.
	nested := s.dir
	for len(nested) <= 128 {
		nested = nested + "/deeply"
	}
	err := os.MkdirAll(nested, 0o755)
	s.Require().NoError(err, "MkdirAll")

	err = s.exp.Export(s.ctx, nested, "127.0.0.1")
	s.Require().NoError(err, "Export long path")

	// TearDownTest only cleans s.dir; also clean the nested export.
	s.T().Cleanup(func() { _ = s.exp.Unexport(s.ctx, nested, "") })

	exports, err := s.exp.ListExports(s.ctx)
	s.Require().NoError(err, "ListExports")

	s.Assert().True(containsExport(exports, nested, "127.0.0.1"),
		"expected long path %s in exports: %v", nested, exports)
}

func containsExport(exports []ExportInfo, path, client string) bool {
	for _, e := range exports {
		if e.Path == path && e.Client == client {
			return true
		}
	}
	return false
}
