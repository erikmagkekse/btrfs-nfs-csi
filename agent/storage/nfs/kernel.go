package nfs

import (
	"context"
	"fmt"
	"hash/crc32"
	"os/exec"
	"strings"

	"github.com/rs/zerolog/log"
)

type kernelExporter struct {
	bin string
}

func NewKernelExporter(bin string) Exporter {
	return &kernelExporter{bin: bin}
}

func (e *kernelExporter) Export(ctx context.Context, path string, client string) error {
	fsid := crc32.ChecksumIEEE([]byte(path)) & 0x7FFFFFFF
	if fsid == 0 {
		fsid = 1
	}
	opts := fmt.Sprintf("rw,nohide,crossmnt,no_root_squash,no_subtree_check,fsid=%d", fsid)
	return run(ctx, e.bin, "-o", opts, fmt.Sprintf("%s:%s", client, path))
}

func (e *kernelExporter) Unexport(ctx context.Context, path string, client string) error {
	if client != "" {
		return runIgnoreNotFound(ctx, e.bin, "-u", fmt.Sprintf("%s:%s", client, path))
	}

	// remove all clients for this path
	clients, err := e.exportedClients(ctx, path)
	if err != nil || len(clients) == 0 {
		return err
	}

	var lastErr error
	for _, c := range clients {
		if err := runIgnoreNotFound(ctx, e.bin, "-u", fmt.Sprintf("%s:%s", c, path)); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// ListExports returns all path+client pairs currently exported.
// exportfs -v wraps long paths onto two lines:
//
//	/short/path  client(opts)
//	/very/long/path
//	        client(opts)
func (e *kernelExporter) ListExports(ctx context.Context) ([]ExportInfo, error) {
	out, err := output(ctx, e.bin, "-v")
	if err != nil {
		return nil, err
	}

	var exports []ExportInfo
	var currentPath string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) >= 2 && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			// path and client on same line
			client := strings.SplitN(fields[1], "(", 2)[0]
			exports = append(exports, ExportInfo{Path: fields[0], Client: client})
			currentPath = ""
		} else if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			// path only, client on next line
			currentPath = fields[0]
		} else if currentPath != "" {
			// indented client line
			client := strings.SplitN(fields[0], "(", 2)[0]
			exports = append(exports, ExportInfo{Path: currentPath, Client: client})
			currentPath = ""
		}
	}
	return exports, nil
}

// exportedClients returns all clients that have the given path exported.
func (e *kernelExporter) exportedClients(ctx context.Context, path string) ([]string, error) {
	exports, err := e.ListExports(ctx)
	if err != nil {
		return nil, err
	}

	var clients []string
	for _, ex := range exports {
		if ex.Path == path {
			clients = append(clients, ex.Client)
		}
	}
	return clients, nil
}

func runIgnoreNotFound(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Could not find") {
			log.Debug().Str("args", strings.Join(args, " ")).Msg("export not found, skipping unexport")
			return nil
		}
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
