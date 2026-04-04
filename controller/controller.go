package controller

import (
	"context"
	"fmt"

	"github.com/erikmagkekse/btrfs-nfs-csi/csiserver"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	snapshotclient "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
)

func Start(ctx context.Context, endpoint, metricsAddr, version, commit, defaultLabels string) error {
	initDefaultLabels(defaultLabels)
	startMetricsServer(metricsAddr)

	k8sCfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("k8s in-cluster config: %w", err)
	}
	kubeClient, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		return fmt.Errorf("k8s client: %w", err)
	}
	snapClient, err := snapshotclient.NewForConfig(k8sCfg)
	if err != nil {
		return fmt.Errorf("k8s snapshot client: %w", err)
	}

	agents := NewAgentTracker(kubeClient, version, commit)
	go agents.Run(ctx)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: csiserver.DriverName + "-controller"})
	defer broadcaster.Shutdown()

	srv, err := csiserver.New(endpoint, version, metricsInterceptor)
	if err != nil {
		return err
	}
	csi.RegisterControllerServer(srv.GRPC(), &Server{agents: agents, kubeClient: kubeClient, snapClient: snapClient, recorder: recorder})
	return srv.Run(ctx, "controller")
}

type Server struct {
	csi.UnimplementedControllerServer
	agents     *AgentTracker
	kubeClient kubernetes.Interface
	snapClient snapshotclient.Interface
	recorder   record.EventRecorder
}

func (s *Server) ValidateVolumeCapabilities(_ context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID required")
	}
	if len(req.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume capabilities required")
	}

	for _, cap := range req.VolumeCapabilities {
		if cap.GetBlock() != nil {
			return &csi.ValidateVolumeCapabilitiesResponse{
				Message: "block access not supported",
			}, nil
		}
		if am := cap.GetAccessMode(); am != nil {
			switch am.Mode {
			case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
				csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
				csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			default:
				return &csi.ValidateVolumeCapabilitiesResponse{
					Message: "only ReadWriteOnce, ReadOnlyOnce, ReadOnlyMany, and ReadWriteMany access modes are supported",
				}, nil
			}
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.VolumeCapabilities,
		},
	}, nil
}

func (s *Server) ControllerGetCapabilities(_ context.Context, _ *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	caps := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
	}

	var csiCaps []*csi.ControllerServiceCapability
	for _, c := range caps {
		csiCaps = append(csiCaps, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: c,
				},
			},
		})
	}

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: csiCaps,
	}, nil
}
