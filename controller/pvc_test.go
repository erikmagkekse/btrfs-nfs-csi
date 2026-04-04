package controller

import (
	"context"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
)

// --- TestVolumeParamsValidate ---

func TestVolumeParamsValidate(t *testing.T) {
	tests := []struct {
		name    string
		vp      volumeParams
		wantErr bool
	}{
		{name: "all_empty", vp: volumeParams{}},
		{name: "valid_all", vp: volumeParams{UID: "1000", GID: "1000", Mode: "0755"}},
		{name: "invalid_uid", vp: volumeParams{UID: "abc"}, wantErr: true},
		{name: "invalid_gid", vp: volumeParams{GID: "-1.5"}, wantErr: true},
		{name: "invalid_mode_not_octal", vp: volumeParams{Mode: "999"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.vp.validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// --- TestToUpdateRequest ---

func TestToUpdateRequest(t *testing.T) {
	t.Run("empty_no_change", func(t *testing.T) {
		vp := volumeParams{}
		_, changed := vp.toUpdateRequest()
		assert.False(t, changed)
	})

	t.Run("uid_gid_set", func(t *testing.T) {
		vp := volumeParams{UID: "1000", GID: "2000"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.UID)
		require.NotNil(t, req.GID)
		assert.Equal(t, 1000, *req.UID)
		assert.Equal(t, 2000, *req.GID)
	})

	t.Run("nocow_true", func(t *testing.T) {
		vp := volumeParams{NoCOW: "true"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.NoCOW)
		assert.True(t, *req.NoCOW)
	})

	t.Run("nocow_false", func(t *testing.T) {
		vp := volumeParams{NoCOW: "false"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.NoCOW)
		assert.False(t, *req.NoCOW)
	})

	t.Run("compression", func(t *testing.T) {
		vp := volumeParams{Compression: "zstd"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.Compression)
		assert.Equal(t, "zstd", *req.Compression)
	})

	t.Run("mode", func(t *testing.T) {
		vp := volumeParams{Mode: "0750"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.Mode)
		assert.Equal(t, "0750", *req.Mode)
	})

	t.Run("labels", func(t *testing.T) {
		labels := map[string]string{"env": "prod"}
		vp := volumeParams{Labels: labels}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.Labels)
		assert.Equal(t, labels, *req.Labels)
	})
}

// --- TestMergeUserLabels ---

func testServer() *Server {
	return &Server{recorder: record.NewFakeRecorder(10)}
}

func TestMergeUserLabels(t *testing.T) {
	srv := testServer()
	pvc := &corev1.PersistentVolumeClaim{}

	t.Run("basic_merge", func(t *testing.T) {
		dst := map[string]string{"created-by": "csi"}
		user := map[string]string{"env": "prod", "team": "be"}
		srv.mergeUserLabels(pvc, dst, user, 4)
		assert.Equal(t, "csi", dst["created-by"])
		assert.Equal(t, "prod", dst["env"])
		assert.Equal(t, "be", dst["team"])
	})

	t.Run("reserved_keys_skipped", func(t *testing.T) {
		dst := map[string]string{"kubernetes.pvc.name": "my-pvc"}
		user := map[string]string{"kubernetes.pvc.name": "hacked", "env": "prod"}
		srv.mergeUserLabels(pvc, dst, user, 4)
		assert.Equal(t, "my-pvc", dst["kubernetes.pvc.name"])
		assert.Equal(t, "prod", dst["env"])
	})

	t.Run("max_user_labels", func(t *testing.T) {
		dst := map[string]string{}
		user := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
		srv.mergeUserLabels(pvc, dst, user, 4)
		assert.Len(t, dst, 4)
	})

	t.Run("deterministic_truncation", func(t *testing.T) {
		dst1 := map[string]string{}
		dst2 := map[string]string{}
		user := map[string]string{"z": "1", "a": "2", "m": "3", "b": "4", "x": "5"}
		srv.mergeUserLabels(pvc, dst1, user, 3)
		srv.mergeUserLabels(pvc, dst2, user, 3)
		assert.Equal(t, dst1, dst2)
		// alphabetically first 3: a, b, m
		assert.Contains(t, dst1, "a")
		assert.Contains(t, dst1, "b")
		assert.Contains(t, dst1, "m")
	})

	t.Run("created_by_reserved", func(t *testing.T) {
		dst := map[string]string{"created-by": "csi"}
		user := map[string]string{"created-by": "terraform"}
		srv.mergeUserLabels(pvc, dst, user, 4)
		assert.Equal(t, "csi", dst["created-by"])
	})
}

// --- TestInitDefaultLabels ---

func TestInitDefaultLabels(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		envDefaultLabels = nil
		initDefaultLabels("")
		assert.Nil(t, envDefaultLabels)
	})

	t.Run("valid_labels", func(t *testing.T) {
		envDefaultLabels = nil
		initDefaultLabels("env=prod,team=backend")
		assert.Equal(t, "prod", envDefaultLabels["env"])
		assert.Equal(t, "backend", envDefaultLabels["team"])
	})

	t.Run("reserved_keys_filtered", func(t *testing.T) {
		envDefaultLabels = nil
		initDefaultLabels("kubernetes.pvc.name=hacked,env=prod")
		assert.NotContains(t, envDefaultLabels, "kubernetes.pvc.name")
		assert.Equal(t, "prod", envDefaultLabels["env"])
	})

	t.Run("invalid_labels_filtered", func(t *testing.T) {
		envDefaultLabels = nil
		initDefaultLabels("BADKEY=val,env=prod")
		assert.NotContains(t, envDefaultLabels, "BADKEY")
		assert.Equal(t, "prod", envDefaultLabels["env"])
	})
}

// --- TestResolveVolumeParams ---

func TestResolveVolumeParams(t *testing.T) {
	t.Run("reads_pvc_annotations", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-pvc",
				Namespace: "default",
				Annotations: map[string]string{
					config.AnnoPrefix + config.ParamNoCOW: "true",
					config.AnnoPrefix + config.ParamUID:   "1000",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("my-sc"),
			},
		}
		srv := &Server{kubeClient: fakekube.NewSimpleClientset(pvc), recorder: record.NewFakeRecorder(10)}

		params := map[string]string{
			config.PvcNameKey:      "my-pvc",
			config.PvcNamespaceKey: "default",
		}
		vp := srv.resolveVolumeParams(context.Background(), params)

		assert.Equal(t, "my-sc", vp.StorageClass)
		assert.Equal(t, "true", vp.NoCOW)
		assert.Equal(t, "1000", vp.UID)
		assert.Equal(t, "my-pvc", vp.Labels["kubernetes.pvc.name"])
		assert.Equal(t, "default", vp.Labels["kubernetes.pvc.namespace"])
		assert.Equal(t, "my-sc", vp.Labels["kubernetes.pvc.storageclass"])
		assert.Equal(t, "csi", vp.Labels["created-by"])
	})

	t.Run("no_pvc_info", func(t *testing.T) {
		srv := &Server{kubeClient: fakekube.NewSimpleClientset(), recorder: record.NewFakeRecorder(10)}

		vp := srv.resolveVolumeParams(context.Background(), map[string]string{
			config.ParamNoCOW: "true",
		})

		assert.Equal(t, "true", vp.NoCOW)
		assert.Empty(t, vp.StorageClass)
		assert.Nil(t, vp.Labels)
	})

	t.Run("pvc_not_found", func(t *testing.T) {
		srv := &Server{kubeClient: fakekube.NewSimpleClientset(), recorder: record.NewFakeRecorder(10)}

		vp := srv.resolveVolumeParams(context.Background(), map[string]string{
			config.PvcNameKey:      "missing",
			config.PvcNamespaceKey: "default",
		})

		assert.Empty(t, vp.StorageClass)
		assert.Nil(t, vp.Labels)
	})
}
