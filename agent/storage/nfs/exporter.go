package nfs

import "context"

type ExportInfo struct {
	Path   string
	Client string
}

type Exporter interface {
	Export(ctx context.Context, path, client, fsid string) error    // fsid: 32-hex UUID or numeric
	Unexport(ctx context.Context, path string, client string) error // client="" removes all
	ListExports(ctx context.Context) ([]ExportInfo, error)
}
