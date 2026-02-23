package controller

import (
	"context"
	"fmt"
	"strconv"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/k8s"
	"github.com/erikmagkekse/btrfs-nfs-csi/model"

	"github.com/rs/zerolog/log"
)

const (
	paramNoCOW       = "nocow"
	paramCompression = "compression"
	paramUID         = "uid"
	paramGID         = "gid"
	paramMode        = "mode"
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
		NoCOW:       params[paramNoCOW],
		Compression: params[paramCompression],
		UID:         params[paramUID],
		GID:         params[paramGID],
		Mode:        params[paramMode],
	}

	pvcName := params[model.PvcNameKey]
	pvcNamespace := params[model.PvcNamespaceKey]
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
	if v, ok := annos[model.AnnoNoCOW]; ok {
		vp.NoCOW = v
	}
	if v, ok := annos[model.AnnoCompression]; ok {
		vp.Compression = v
	}
	if v, ok := annos[model.AnnoUID]; ok {
		vp.UID = v
	}
	if v, ok := annos[model.AnnoGID]; ok {
		vp.GID = v
	}
	if v, ok := annos[model.AnnoMode]; ok {
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
