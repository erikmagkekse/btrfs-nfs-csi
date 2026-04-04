package controller

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/csiserver"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/rs/zerolog/log"
)

var envDefaultLabels map[string]string

func initDefaultLabels(raw string) {
	if raw == "" {
		return
	}
	parsed := parseLabels(raw)
	envDefaultLabels = make(map[string]string, len(parsed))
	for k, v := range parsed {
		if reservedLabelKeys[k] {
			log.Warn().Str("key", k).Msg("ignoring reserved key in DRIVER_DEFAULT_LABELS")
			continue
		}
		if !config.ValidLabelKey.MatchString(k) || !config.ValidLabelVal.MatchString(v) {
			log.Warn().Str("key", k).Str("value", v).Msg("ignoring invalid label in DRIVER_DEFAULT_LABELS")
			continue
		}
		envDefaultLabels[k] = v
	}
	if len(envDefaultLabels) > 0 {
		log.Info().Int("count", len(envDefaultLabels)).Msg("loaded default labels from env")
	}
}

type volumeParams struct {
	StorageClass string
	Labels       map[string]string
	agentAPI.VolumeUpdateRequest
}

// applyStringParams parses string params (from SC or PVC annotations) into typed fields.
func (vp *volumeParams) applyStringParams(params map[string]string) error {
	if v, ok := params[paramNoCOW]; ok && v != "" {
		if v != "true" && v != "false" {
			return fmt.Errorf("invalid nocow %q: must be \"true\" or \"false\"", v)
		}
		nocow := v == "true"
		vp.NoCOW = &nocow
	}
	if v, ok := params[paramCompression]; ok && v != "" {
		if !utils.IsValidCompression(v) {
			return fmt.Errorf("invalid compression %q", v)
		}
		vp.Compression = &v
	}
	if v, ok := params[paramUID]; ok && v != "" {
		uid, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid uid %q: %v", v, err)
		}
		vp.UID = &uid
	}
	if v, ok := params[paramGID]; ok && v != "" {
		gid, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid gid %q: %v", v, err)
		}
		vp.GID = &gid
	}
	if v, ok := params[paramMode]; ok && v != "" {
		if _, err := strconv.ParseUint(v, 8, 32); err != nil {
			return fmt.Errorf("invalid mode %q: %v", v, err)
		}
		vp.Mode = &v
	}
	return nil
}

func (s *Server) resolveVolumeParams(ctx context.Context, params map[string]string) (volumeParams, error) {
	var vp volumeParams

	// SC params first
	if err := vp.applyStringParams(params); err != nil {
		return vp, err
	}

	pvcName := params[csiserver.PvcNameKey]
	pvcNamespace := params[csiserver.PvcNamespaceKey]
	if pvcName == "" || pvcNamespace == "" {
		return vp, nil
	}

	pvc, err := s.kubeClient.CoreV1().PersistentVolumeClaims(pvcNamespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		ctrlK8sOpsTotal.WithLabelValues("error").Inc()
		log.Warn().Err(err).Str("pvc", pvcNamespace+"/"+pvcName).Msg("failed to fetch PVC, using SC defaults")
		return vp, nil
	}
	ctrlK8sOpsTotal.WithLabelValues("success").Inc()

	scName := ptr.Deref(pvc.Spec.StorageClassName, "")
	vp.StorageClass = scName
	vp.Labels = map[string]string{
		labelPVCName:          pvcName,
		labelPVCNamespace:     pvcNamespace,
		labelPVCStorageClass:  scName,
		config.LabelCreatedBy: controllerIdentity,
	}
	for k, v := range envDefaultLabels {
		if _, exists := vp.Labels[k]; !exists {
			vp.Labels[k] = v
		}
	}

	// PVC annotations override SC params (validate each individually so one bad annotation doesn't block others)
	for _, key := range []string{paramNoCOW, paramCompression, paramUID, paramGID, paramMode} {
		v, ok := pvc.Annotations[annoPrefix+key]
		if !ok {
			continue
		}
		if err := vp.applyStringParams(map[string]string{key: v}); err != nil {
			log.Warn().Err(err).Str("pvc", pvcNamespace+"/"+pvcName).Str("annotation", annoPrefix+key).Msg("invalid PVC annotation, skipping")
			s.recorder.Eventf(pvc, corev1.EventTypeWarning, "AnnotationInvalid", "invalid annotation %s=%q: %v", annoPrefix+key, v, err)
		}
	}

	if v, ok := pvc.Annotations[annoPrefix+paramLabels]; ok && v != "" {
		if skipped := mergeUserLabels(vp.Labels, parseLabels(v), config.MaxUserLabels); len(skipped) > 0 {
			s.recorder.Eventf(pvc, corev1.EventTypeWarning, "LabelsSkipped", "skipped label(s) from PVC annotation: %v", skipped)
		}
	}

	return vp, nil
}

var reservedLabelKeys = map[string]bool{
	labelPVCName:               true,
	labelPVCNamespace:          true,
	labelPVCStorageClass:       true,
	config.LabelCreatedBy:      true,
	labelPVName:                true,
	labelPVStorageClass:        true,
	labelNodeName:              true,
	labelVolumeAttachmentName:  true,
	labelSourcePVCName:         true,
	labelSourcePVCNamespace:    true,
	labelSourcePVCStorageClass: true,
	labelSnapshotName:          true,
	labelSnapshotNamespace:     true,
}

func init() {
	for _, k := range config.SoftReservedLabelKeys {
		reservedLabelKeys[k] = true
	}
}

// mergeUserLabels merges user labels into dst, skipping reserved/invalid keys.
// Returns the skipped keys for the caller to handle (e.g. record events).
func mergeUserLabels(dst, user map[string]string, maxUser int) (skippedKeys []string) {
	keys := make([]string, 0, len(user))
	for k := range user {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	merged := 0
	truncated := false
	for _, k := range keys {
		if reservedLabelKeys[k] {
			log.Warn().Str("key", k).Msg("skipping reserved label key")
			skippedKeys = append(skippedKeys, k)
			continue
		}
		if !config.ValidLabelKey.MatchString(k) || !config.ValidLabelVal.MatchString(user[k]) {
			log.Warn().Str("key", k).Str("value", user[k]).Msg("skipping invalid label")
			skippedKeys = append(skippedKeys, k)
			continue
		}
		if merged >= maxUser {
			if !truncated {
				log.Warn().Int("max", maxUser).Msg("too many user labels, truncating")
				truncated = true
			}
			skippedKeys = append(skippedKeys, k)
			continue
		}
		dst[k] = user[k]
		merged++
	}
	return skippedKeys
}

func parseLabels(raw string) map[string]string {
	labels := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		labels[k] = v
	}
	return labels
}

func (s *Server) pvcFromVolumeHandle(ctx context.Context, volumeID string) (*corev1.PersistentVolumeClaim, error) {
	pvList, err := s.kubeClient.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list PVs: %w", err)
	}
	for i := range pvList.Items {
		pv := &pvList.Items[i]
		if pv.Spec.CSI == nil || pv.Spec.CSI.Driver != csiserver.DriverName {
			continue
		}
		if pv.Spec.CSI.VolumeHandle != volumeID {
			continue
		}
		ref := pv.Spec.ClaimRef
		if ref == nil {
			return nil, fmt.Errorf("PV %s has no claimRef", pv.Name)
		}
		pvc, err := s.kubeClient.CoreV1().PersistentVolumeClaims(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get PVC %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		return pvc, nil
	}
	return nil, fmt.Errorf("no PV found for volume handle %s", volumeID)
}

func (s *Server) resolveSnapshotLabels(ctx context.Context, params map[string]string, sourceVolumeID, sc, pvName string) map[string]string {
	labels := map[string]string{
		config.LabelCreatedBy: controllerIdentity,
		labelPVName:           pvName,
		labelPVStorageClass:   sc,
	}
	for k, v := range envDefaultLabels {
		if _, exists := labels[k]; !exists {
			labels[k] = v
		}
	}

	// snapshot identity from snapshotter --extra-create-metadata
	snapName := params[snapshotNameKey]
	snapNS := params[snapshotNamespaceKey]
	if snapName != "" {
		labels[labelSnapshotName] = snapName
	}
	if snapNS != "" {
		labels[labelSnapshotNamespace] = snapNS
	}

	// source PVC metadata
	pvc, err := s.pvcFromVolumeHandle(ctx, sourceVolumeID)
	if err != nil {
		log.Warn().Err(err).Str("volume", pvName).Str("sc", sc).Msg("failed to resolve source PVC for snapshot labels")
	} else {
		labels[labelSourcePVCName] = pvc.Name
		labels[labelSourcePVCNamespace] = pvc.Namespace
		labels[labelSourcePVCStorageClass] = ptr.Deref(pvc.Spec.StorageClassName, "")
	}

	// user labels from VolumeSnapshot annotation only (no PVC fallback)
	var userLabelsRaw string
	if snapName != "" && snapNS != "" {
		vs, err := s.snapClient.SnapshotV1().VolumeSnapshots(snapNS).Get(ctx, snapName, metav1.GetOptions{})
		if err != nil {
			log.Warn().Err(err).Str("snapshot", snapNS+"/"+snapName).Msg("failed to fetch VolumeSnapshot for user labels")
		} else if v, ok := vs.Annotations[annoPrefix+paramLabels]; ok && v != "" {
			userLabelsRaw = v
		}
	}
	if userLabelsRaw != "" {
		mergeUserLabels(labels, parseLabels(userLabelsRaw), config.MaxUserLabels)
	}

	return labels
}

func (vp *volumeParams) hasUpdates() bool {
	return vp.NoCOW != nil || vp.Compression != nil || vp.UID != nil || vp.GID != nil || vp.Mode != nil || vp.Labels != nil
}

func (vp *volumeParams) updateRequest() agentAPI.VolumeUpdateRequest {
	u := vp.VolumeUpdateRequest
	if vp.Labels != nil {
		u.Labels = &vp.Labels
	}
	return u
}
