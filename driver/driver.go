package driver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/csiserver"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/mount-utils"
)

// parseVolumeLog parses a composite volume ID (sc|name) into separate fields.
// If parsing fails, emits a warning and returns the raw ID as volume name.
func parseVolumeLog(volumeID string) (sc, name string) {
	sc, name, err := utils.ParseVolumeID(volumeID)
	if err != nil {
		log.Warn().Str("volumeId", volumeID).Msg("unparseable volume ID")
		return "", volumeID
	}
	return sc, name
}

func Start(ctx context.Context, endpoint, nodeID, nodeIP, metricsAddr, version string, healthCheckInterval time.Duration) error {
	startMetricsServer(metricsAddr)

	srv, err := csiserver.New(endpoint, version, metricsInterceptor)
	if err != nil {
		return fmt.Errorf("create CSI server on %s: %w", endpoint, err)
	}
	ns := &NodeServer{nodeID: nodeID, nodeIP: nodeIP, mounter: mount.New("")}
	if healthCheckInterval > 0 {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			log.Warn().Err(err).Msg("k8s in-cluster config unavailable, health checker events disabled")
		} else {
			ns.kubeClient = kubernetes.NewForConfigOrDie(cfg)
			log.Debug().Str("host", cfg.Host).Msg("health checker: k8s API endpoint")
		}
		go ns.startHealthChecker(ctx, healthCheckInterval)
	}
	csi.RegisterNodeServer(srv.GRPC(), ns)
	return srv.Run(ctx, "driver")
}

type NodeServer struct {
	csi.UnimplementedNodeServer
	nodeID      string
	nodeIP      string
	mounter     mount.Interface
	kubeClient  kubernetes.Interface
	locks       sync.Map
	healthState sync.Map
}

func (s *NodeServer) volumeLock(id string) func() {
	val, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *NodeServer) NodeGetCapabilities(_ context.Context, _ *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	log.Trace().Msg("NodeGetCapabilities")
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
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_VOLUME_CONDITION,
					},
				},
			},
		},
	}, nil
}

func (s *NodeServer) NodeGetInfo(_ context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	log.Trace().Str("node", s.nodeID).Msg("NodeGetInfo")
	return &csi.NodeGetInfoResponse{
		NodeId: s.nodeID + config.NodeIDSep + s.nodeIP,
	}, nil
}
