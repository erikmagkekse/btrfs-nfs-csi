package nfs

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/rs/zerolog/log"
)

// validFSID is what nfsd accepts after fsid=: a number or a 32-digit hex UUID.
// Checked here because fsid is the only part of the option string that comes
// from stored data, and a comma in it would append further export options.
var validFSID = regexp.MustCompile(`^([0-9]{1,10}|[0-9a-f]{32})$`)

type kernelExporter struct {
	bin  string
	cmd  utils.Runner
	opts string
}

func NewKernelExporter(bin, exportOpts string) Exporter {
	return &kernelExporter{bin: bin, cmd: &utils.ShellRunner{}, opts: exportOpts}
}

// exportfsClient wraps IPv6 addresses in brackets for exportfs compatibility.
// exportfs requires [addr]:/path for IPv6 but addr:/path for IPv4.
func exportfsClient(client string) string {
	if strings.Contains(client, ":") {
		return "[" + client + "]"
	}
	return client
}

// unwrapBrackets removes bracket wrapping from an IPv6 address returned by exportfs -v.
func unwrapBrackets(client string) string {
	if addr, _, ok := strings.Cut(client, "]"); ok && strings.HasPrefix(addr, "[") {
		return strings.TrimPrefix(addr, "[")
	}
	return client
}

func (e *kernelExporter) Export(ctx context.Context, path, client, fsid string) error {
	if !validFSID.MatchString(fsid) {
		return fmt.Errorf("invalid fsid %q", fsid)
	}
	opts := fmt.Sprintf("%s,fsid=%s", e.opts, fsid)
	return e.run(ctx, "-o", opts, fmt.Sprintf("%s:%s", exportfsClient(client), path))
}

func (e *kernelExporter) Unexport(ctx context.Context, path string, client string) error {
	if client != "" {
		return e.tryUnexport(ctx, "-u", fmt.Sprintf("%s:%s", exportfsClient(client), path))
	}

	// remove all clients for this path
	clients, err := e.exportedClients(ctx, path)
	if err != nil || len(clients) == 0 {
		return err
	}

	var lastErr error
	for _, c := range clients {
		if err := e.tryUnexport(ctx, "-u", fmt.Sprintf("%s:%s", exportfsClient(c), path)); err != nil {
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
	out, err := e.exec(ctx, "-v")
	if err != nil {
		return nil, err
	}
	return parseExports(out), nil
}

// parseExports parses the output of exportfs -v into export entries.
func parseExports(output string) []ExportInfo {
	var exports []ExportInfo
	var currentPath string
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		indented := strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ")
		switch {
		case !indented && len(fields) >= 2:
			// path and client on same line
			raw, _, _ := strings.Cut(fields[1], "(")
			client := unwrapBrackets(raw)
			exports = append(exports, ExportInfo{Path: fields[0], Client: client})
			currentPath = ""
		case !indented:
			// path only, client on next line
			currentPath = fields[0]
		case currentPath != "":
			// indented client line
			raw, _, _ := strings.Cut(fields[0], "(")
			client := unwrapBrackets(raw)
			exports = append(exports, ExportInfo{Path: currentPath, Client: client})
			currentPath = ""
		}
	}
	return exports
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

func (e *kernelExporter) exec(ctx context.Context, args ...string) (string, error) {
	return e.cmd.Run(ctx, e.bin, args...)
}

func (e *kernelExporter) run(ctx context.Context, args ...string) error {
	_, err := e.cmd.Run(ctx, e.bin, args...)
	return err
}

// tryUnexport removes an export, silently ignoring already removed entries.
func (e *kernelExporter) tryUnexport(ctx context.Context, args ...string) error {
	out, err := e.exec(ctx, args...)
	if err != nil && strings.Contains(out, "Could not find") {
		log.Debug().Str("args", strings.Join(args, " ")).Msg("export not found, skipping unexport")
		return nil
	}
	return err
}
