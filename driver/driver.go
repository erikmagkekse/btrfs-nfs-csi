package driver

import (
	"context"
	"sync"

	"github.com/erikmagkekse/btrfs-nfs-csi/csiserver"
	"github.com/erikmagkekse/btrfs-nfs-csi/model"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
)

func Start(ctx context.Context, endpoint, nodeID, nodeIP, metricsAddr, version string) error {
	startMetricsServer(metricsAddr)

	srv, err := csiserver.New(endpoint, version, metricsInterceptor)
	if err != nil {
		return err
	}
	csi.RegisterNodeServer(srv.GRPC(), &NodeServer{nodeID: nodeID, nodeIP: nodeIP})
	return srv.Run(ctx, "driver")
}

type NodeServer struct {
	csi.UnimplementedNodeServer
	nodeID string
	nodeIP string
	locks  sync.Map
}

func (s *NodeServer) volumeLock(id string) func() {
	val, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *NodeServer) NodeGetCapabilities(_ context.Context, _ *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
					},
				},
			},
		},
	}, nil
}

func (s *NodeServer) NodeGetInfo(_ context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: s.nodeID + model.NodeIDSep + s.nodeIP,
	}, nil
}
