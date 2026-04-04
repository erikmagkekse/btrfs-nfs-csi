package controller

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	NoCOW        string
	Compression  string
	UID          string
	GID          string
	Mode         string
	Labels       map[string]string
}

func (s *Server) resolveVolumeParams(ctx context.Context, params map[string]string) volumeParams {
	vp := volumeParams{
		NoCOW:       params[config.ParamNoCOW],
		Compression: params[config.ParamCompression],
		UID:         params[config.ParamUID],
		GID:         params[config.ParamGID],
		Mode:        params[config.ParamMode],
	}

	pvcName := params[config.PvcNameKey]
	pvcNamespace := params[config.PvcNamespaceKey]
	if pvcName == "" || pvcNamespace == "" {
		return vp
	}

	pvc, err := s.kubeClient.CoreV1().PersistentVolumeClaims(pvcNamespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		ctrlK8sOpsTotal.WithLabelValues("error").Inc()
		log.Warn().Err(err).Str("pvc", pvcNamespace+"/"+pvcName).Msg("failed to fetch PVC, using SC defaults")
		return vp
	}
	ctrlK8sOpsTotal.WithLabelValues("success").Inc()

	scName := ptr.Deref(pvc.Spec.StorageClassName, "")
	vp.StorageClass = scName
	vp.Labels = map[string]string{
		"kubernetes.pvc.name":         pvcName,
		"kubernetes.pvc.namespace":    pvcNamespace,
		"kubernetes.pvc.storageclass": scName,
		"created-by":                  "csi",
	}
	for k, v := range envDefaultLabels {
		if _, exists := vp.Labels[k]; !exists {
			vp.Labels[k] = v
		}
	}

	annos := pvc.Annotations
	if v, ok := annos[config.AnnoPrefix+config.ParamNoCOW]; ok {
		vp.NoCOW = v
	}
	if v, ok := annos[config.AnnoPrefix+config.ParamCompression]; ok {
		vp.Compression = v
	}
	if v, ok := annos[config.AnnoPrefix+config.ParamUID]; ok {
		vp.UID = v
	}
	if v, ok := annos[config.AnnoPrefix+config.ParamGID]; ok {
		vp.GID = v
	}
	if v, ok := annos[config.AnnoPrefix+config.ParamMode]; ok {
		vp.Mode = v
	}
	if v, ok := annos[config.AnnoPrefix+config.ParamLabels]; ok && v != "" {
		s.mergeUserLabels(pvc, vp.Labels, parseLabels(v), config.MaxUserLabels)
	}

	return vp
}

var reservedLabelKeys = map[string]bool{
	"kubernetes.pvc.name":         true,
	"kubernetes.pvc.namespace":    true,
	"kubernetes.pvc.storageclass": true,
	"created-by":                  true,
}

func (s *Server) mergeUserLabels(pvc runtime.Object, dst, user map[string]string, maxUser int) {
	keys := make([]string, 0, len(user))
	for k := range user {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	merged := 0
	for _, k := range keys {
		if reservedLabelKeys[k] {
			log.Warn().Str("key", k).Msg("skipping reserved label key from PVC annotation")
			s.recorder.Eventf(pvc, corev1.EventTypeWarning, "LabelSkipped", "skipping reserved label key %q from PVC annotation", k)
			continue
		}
		if !config.ValidLabelKey.MatchString(k) || !config.ValidLabelVal.MatchString(user[k]) {
			log.Warn().Str("key", k).Str("value", user[k]).Msg("skipping invalid label from PVC annotation")
			s.recorder.Eventf(pvc, corev1.EventTypeWarning, "LabelInvalid", "skipping invalid label %q=%q from PVC annotation", k, user[k])
			continue
		}
		if merged >= maxUser {
			log.Warn().Int("max", maxUser).Msg("too many user labels in PVC annotation, truncating")
			s.recorder.Eventf(pvc, corev1.EventTypeWarning, "LabelsTruncated", "too many user labels (max %d), remaining labels ignored", maxUser)
			break
		}
		dst[k] = user[k]
		merged++
	}
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

func (vp *volumeParams) validate() error {
	if vp.NoCOW != "" && vp.NoCOW != "true" && vp.NoCOW != "false" {
		return fmt.Errorf("invalid nocow %q: must be \"true\" or \"false\"", vp.NoCOW)
	}
	if vp.Compression != "" && !utils.IsValidCompression(vp.Compression) {
		return fmt.Errorf("invalid compression %q", vp.Compression)
	}
	if vp.UID != "" {
		if _, err := strconv.Atoi(vp.UID); err != nil {
			return fmt.Errorf("invalid uid %q: %v", vp.UID, err)
		}
	}
	if vp.GID != "" {
		if _, err := strconv.Atoi(vp.GID); err != nil {
			return fmt.Errorf("invalid gid %q: %v", vp.GID, err)
		}
	}
	if vp.Mode != "" {
		if _, err := strconv.ParseUint(vp.Mode, 8, 32); err != nil {
			return fmt.Errorf("invalid mode %q: %v", vp.Mode, err)
		}
	}
	return nil
}

func (vp *volumeParams) toUpdateRequest() (agentAPI.VolumeUpdateRequest, bool) {
	var update agentAPI.VolumeUpdateRequest
	var changed bool
	if vp.UID != "" {
		uid, _ := strconv.Atoi(vp.UID)
		update.UID = &uid
		changed = true
	}
	if vp.GID != "" {
		gid, _ := strconv.Atoi(vp.GID)
		update.GID = &gid
		changed = true
	}
	if vp.Mode != "" {
		update.Mode = &vp.Mode
		changed = true
	}
	if vp.NoCOW != "" {
		nocow := vp.NoCOW == "true"
		update.NoCOW = &nocow
		changed = true
	}
	if vp.Compression != "" {
		update.Compression = &vp.Compression
		changed = true
	}
	if vp.Labels != nil {
		update.Labels = &vp.Labels
		changed = true
	}
	return update, changed
}
