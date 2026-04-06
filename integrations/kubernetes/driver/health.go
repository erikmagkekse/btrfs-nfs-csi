package driver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/integrations/kubernetes/csiserver"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/mount-utils"
)

func eventType(reason string) string {
	if reason == eventMountHealthy || reason == eventMountRemounted {
		return corev1.EventTypeNormal
	}
	return corev1.EventTypeWarning
}

type volumeHealth struct {
	abnormal bool
	message  string
}

func (s *NodeServer) startHealthChecker(ctx context.Context, interval time.Duration) {
	log.Info().Str("interval", interval.String()).Msg("starting NFS mount health checker")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkMountHealth(ctx)
		}
	}
}

// volumeInfo holds resolved k8s metadata for an NFS source.
type volumeInfo struct {
	volumeID     string
	pvcName      string
	pvcNamespace string
}

func (s *NodeServer) checkMountHealth(ctx context.Context) {
	start := time.Now()

	mounts, err := s.mounter.List()
	if err != nil {
		log.Error().Err(err).Msg("health check: failed to list mounts")
		healthChecksTotal.WithLabelValues("error").Inc()
		return
	}

	// Cache volume attachments once per cycle to avoid duplicate API calls.
	var volumeMap map[string]volumeInfo
	resolveVolume := func(nfsSource string) (volumeInfo, bool) {
		if volumeMap == nil {
			volumeMap = s.buildVolumeMap(ctx)
		}
		vi, ok := volumeMap[nfsSource]
		return vi, ok
	}

	hasUnhealthy := false
	s.healthState.Range(func(_, v any) bool {
		if h, _ := v.(*volumeHealth); h != nil && h.abnormal {
			hasUnhealthy = true
			return false
		}
		return true
	})

	for _, mp := range mounts {
		if (mp.Type != "nfs" && mp.Type != "nfs4") || !strings.Contains(mp.Path, "globalmount") {
			continue
		}

		dataPath := mp.Path + "/" + config.DataDir

		// Skip if a stat is already in-flight for this path to prevent
		// goroutine accumulation when NFS hangs cause stat calls to block.
		if _, loaded := s.statInFlight.LoadOrStore(dataPath, struct{}{}); loaded {
			log.Debug().Str("path", dataPath).Msg("health check: stat already in-flight, skipping")
			healthChecksTotal.WithLabelValues("skipped").Inc()
			continue
		}
		err := statWithTimeout(dataPath, staleCheckTimeout)
		s.statInFlight.Delete(dataPath)

		if err == nil {
			healthChecksTotal.WithLabelValues("healthy").Inc()
			if hasUnhealthy {
				if vi, ok := resolveVolume(mp.Device); ok {
					if prev, loaded := s.healthState.LoadAndDelete(vi.volumeID); loaded {
						if h, _ := prev.(*volumeHealth); h != nil && h.abnormal {
							s.reportVolumeEvent(ctx, &vi, eventMountHealthy, "NFS mount is healthy again")
						}
					}
				}
			}
			continue
		}

		if !mount.IsCorruptedMnt(err) && !errors.Is(err, errStatTimeout) {
			continue
		}

		log.Warn().Err(err).Str("mountpoint", mp.Path).Str("source", mp.Device).Msg("health check: stale NFS mount detected")
		healthChecksTotal.WithLabelValues("stale").Inc()

		// Resolve volume before healing so we can acquire the correct lock.
		vi, resolved := resolveVolume(mp.Device)
		if resolved {
			func() {
				unlock := s.volumeLock(vi.volumeID)
				defer unlock()
				s.healMount(ctx, mp, &vi)
			}()
		} else {
			s.healMount(ctx, mp, nil)
		}
	}

	healthCheckDuration.Observe(time.Since(start).Seconds())
}

func (s *NodeServer) healMount(ctx context.Context, mp mount.MountPoint, vi *volumeInfo) {
	// Re-check after acquiring the volume lock: if Unstage ran while we were
	// waiting for the lock (or during the stat timeout), the mountpoint is gone
	// and we must not re-create it.
	if isMountPoint, err := s.mounter.IsMountPoint(mp.Path); err != nil || !isMountPoint {
		log.Debug().Str("mountpoint", mp.Path).Msg("health check: mountpoint gone after lock, skipping heal")
		return
	}

	// Mount over the stale NFS mount. The kernel stacks the fresh mount on top;
	// the stale one becomes hidden but stays as a fallback if remount fails on
	// the next health check cycle. We intentionally do NOT lazy-unmount the
	// globalmount first -- if the fresh mount fails, the stale entry is still
	// present so the health checker can retry.
	log.Info().Str("source", mp.Device).Str("mountpoint", mp.Path).Str("fstype", mp.Type).Strs("opts", mp.Opts).Msg("health check: remounting")
	start := time.Now()
	if err := s.mounter.Mount(mp.Device, mp.Path, mp.Type, mp.Opts); err != nil {
		mountOpsTotal.WithLabelValues("health_remount", "error").Inc()
		mountDuration.WithLabelValues("health_remount").Observe(time.Since(start).Seconds())
		log.Error().Err(err).Str("mountpoint", mp.Path).Msg("health check: remount failed")
		healthChecksTotal.WithLabelValues("remount_failed").Inc()
		s.reportVolumeEvent(ctx, vi, eventMountRemountFailed, fmt.Sprintf("NFS mount stale, remount failed: %v", err))
		return
	}

	mountOpsTotal.WithLabelValues("health_remount", "success").Inc()
	mountDuration.WithLabelValues("health_remount").Observe(time.Since(start).Seconds())
	healthChecksTotal.WithLabelValues("remounted").Inc()

	// Pod bind mounts still reference the old (stale) globalmount. Lazy-unmount
	// each and re-bind from the fresh globalmount so pods get working I/O.
	healed := s.healBindMounts(mp.Device, mp.Path)
	log.Info().Str("mountpoint", mp.Path).Int("bind_mounts_healed", healed).Msg("health check: remount succeeded")
	s.reportVolumeEvent(ctx, vi, eventMountRemounted, "NFS mount was stale, auto-healed by driver")
}

// healBindMounts lazy-unmounts and re-binds pod bind mounts that reference the
// given NFS globalmount device. Returns the number of healed bind mounts.
func (s *NodeServer) healBindMounts(globalDevice, globalMountPath string) int {
	dataDir := globalMountPath + "/" + config.DataDir
	mounts, err := s.mounter.List()
	if err != nil {
		log.Warn().Err(err).Msg("health check: failed to list mounts for bind healing")
		return 0
	}
	healed := 0
	for _, m := range mounts {
		// In /proc/mounts, bind mounts from an NFS subpath show the parent NFS
		// device (e.g. "10.0.0.5:/exports/vol"), not the subpath. Match on the
		// device and filter to pod volume paths.
		if m.Device != globalDevice || !strings.Contains(m.Path, "/pods/") {
			continue
		}
		if err := unix.Unmount(m.Path, unix.MNT_DETACH); err != nil {
			log.Warn().Err(err).Str("path", m.Path).Msg("health check: lazy unmount bind failed")
			continue
		}
		if err := s.mounter.Mount(dataDir, m.Path, "", []string{"bind"}); err != nil {
			log.Error().Err(err).Str("path", m.Path).Msg("health check: re-bind failed")
			continue
		}
		log.Info().Str("path", m.Path).Msg("health check: bind mount healed")
		healed++
	}
	return healed
}

func (s *NodeServer) reportVolumeEvent(ctx context.Context, vi *volumeInfo, reason, message string) {
	if vi == nil {
		return
	}

	if s.kubeClient != nil && vi.pvcName != "" && vi.pvcNamespace != "" {
		event := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: csiserver.DriverName + "-",
				Namespace:    vi.pvcNamespace,
			},
			InvolvedObject: corev1.ObjectReference{
				APIVersion: "v1",
				Kind:       "PersistentVolumeClaim",
				Name:       vi.pvcName,
				Namespace:  vi.pvcNamespace,
			},
			Reason:  reason,
			Message: message,
			Type:    eventType(reason),
			Source:  corev1.EventSource{Component: csiserver.DriverName + "-node"},
		}
		if _, err := s.kubeClient.CoreV1().Events(vi.pvcNamespace).Create(ctx, event, metav1.CreateOptions{}); err != nil {
			log.Warn().Err(err).Str("pvc", vi.pvcNamespace+"/"+vi.pvcName).Msg("health check: failed to create PVC event")
		}
	}

	if reason == eventMountRemounted || reason == eventMountHealthy {
		s.healthState.Delete(vi.volumeID)
	} else {
		s.healthState.Store(vi.volumeID, &volumeHealth{
			abnormal: true,
			message:  message,
		})
	}
}

// buildVolumeMap fetches VolumeAttachments + PVs and builds a map of NFS source -> volumeInfo.
func (s *NodeServer) buildVolumeMap(ctx context.Context) map[string]volumeInfo {
	result := make(map[string]volumeInfo)
	if s.kubeClient == nil {
		return result
	}

	apiCtx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()

	vaList, err := s.kubeClient.StorageV1().VolumeAttachments().List(apiCtx, metav1.ListOptions{})
	if err != nil {
		log.Warn().Err(err).Msg("health check: failed to list volume attachments")
		return result
	}

	for _, va := range vaList.Items {
		if va.Spec.Attacher != csiserver.DriverName || va.Spec.NodeName != s.nodeID || !va.Status.Attached {
			continue
		}
		pvName := va.Spec.Source.PersistentVolumeName
		if pvName == nil {
			continue
		}
		pv, err := s.kubeClient.CoreV1().PersistentVolumes().Get(apiCtx, *pvName, metav1.GetOptions{})
		if err != nil {
			log.Warn().Err(err).Str("pv", *pvName).Msg("health check: failed to get PV")
			continue
		}
		if pv.Spec.CSI == nil || pv.Spec.CSI.Driver != csiserver.DriverName {
			continue
		}

		server := pv.Spec.CSI.VolumeAttributes[csiserver.ParamNFSServer]
		sharePath := pv.Spec.CSI.VolumeAttributes[csiserver.ParamNFSSharePath]
		nfsSource := server + ":" + sharePath

		vi := volumeInfo{volumeID: pv.Spec.CSI.VolumeHandle}
		if pv.Spec.ClaimRef != nil {
			vi.pvcName = pv.Spec.ClaimRef.Name
			vi.pvcNamespace = pv.Spec.ClaimRef.Namespace
		}
		result[nfsSource] = vi
	}
	return result
}
