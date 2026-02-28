package controller

import (
	"context"
	"fmt"
	"strconv"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/k8s"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"github.com/rs/zerolog/log"
)

type volumeParams struct {
	NoCOW       string
	Compression string
	UID         string
	GID         string
	Mode        string
}

func resolveVolumeParams(ctx context.Context, params map[string]string) volumeParams {
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

	var obj struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	path := "/api/v1/namespaces/" + pvcNamespace + "/persistentvolumeclaims/" + pvcName
	if err := k8s.Get(ctx, path, &obj); err != nil {
		ctrlK8sOpsTotal.WithLabelValues("error").Inc()
		log.Warn().Err(err).Str("pvc", pvcNamespace+"/"+pvcName).Msg("failed to fetch PVC annotations, using SC defaults")
		return vp
	}
	ctrlK8sOpsTotal.WithLabelValues("success").Inc()

	annos := obj.Metadata.Annotations
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

	return vp
}

func (vp *volumeParams) validate() error {
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
	return update, changed
}
