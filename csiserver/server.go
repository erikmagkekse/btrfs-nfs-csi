package csiserver

import (
	"context"
	"net"
	"net/url"
	"os"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

type Server struct {
	grpc     *grpc.Server
	listener net.Listener
}

func New(endpoint, version string, interceptor grpc.UnaryServerInterceptor) (*Server, error) {
	listener, err := listen(endpoint)
	if err != nil {
		return nil, err
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(interceptor))
	csi.RegisterIdentityServer(srv, &IdentityServer{Version: version})
	return &Server{grpc: srv, listener: listener}, nil
}

func (s *Server) GRPC() *grpc.Server { return s.grpc }

func (s *Server) Run(ctx context.Context, component string) error {
	return serve(ctx, s.grpc, s.listener, component)
}

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
