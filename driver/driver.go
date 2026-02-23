package driver

import (
	"context"
	"net"
	"net/url"
	"os"

	"github.com/erikmagkekse/btrfs-nfs-csi/controller"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

func listen(endpoint string) (net.Listener, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	if u.Scheme == "unix" {
		addr := u.Path
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return net.Listen("unix", addr)
	}
	return net.Listen("tcp", u.Host)
}

func serve(ctx context.Context, srv *grpc.Server, listener net.Listener, component string) error {
	log.Info().Str("component", component).Msg("CSI listening")

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		log.Info().Str("component", component).Msg("shutting down")
		srv.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

func StartController(ctx context.Context, endpoint, metricsAddr, version, commit string) error {
	listener, err := listen(endpoint)
	if err != nil {
		return err
	}

	startMetricsServer(metricsAddr)

	agents := controller.NewAgentTracker(version, commit)
	go agents.Run(ctx)

	srv := grpc.NewServer(grpc.UnaryInterceptor(metricsInterceptor))
	csi.RegisterIdentityServer(srv, &IdentityServer{version: version})
	csi.RegisterControllerServer(srv, controller.NewServer(agents))

	return serve(ctx, srv, listener, "controller")
}

func StartNode(ctx context.Context, endpoint, nodeID, nodeIP, metricsAddr, version string) error {
	listener, err := listen(endpoint)
	if err != nil {
		return err
	}

	startMetricsServer(metricsAddr)

	srv := grpc.NewServer(grpc.UnaryInterceptor(metricsInterceptor))
	csi.RegisterIdentityServer(srv, &IdentityServer{version: version})
	csi.RegisterNodeServer(srv, &NodeServer{nodeID: nodeID, nodeIP: nodeIP})

	return serve(ctx, srv, listener, "node")
}
