package controller

import (
	"context"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/csiserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
)

// --- TestApplyStringParams ---

func TestApplyStringParams(t *testing.T) {
	t.Run("all_empty", func(t *testing.T) {
		var vp volumeParams
		require.NoError(t, vp.applyStringParams(map[string]string{}))
		assert.False(t, vp.hasUpdates())
	})

	t.Run("valid_all", func(t *testing.T) {
		var vp volumeParams
		require.NoError(t, vp.applyStringParams(map[string]string{
			paramUID: "1000", paramGID: "2000", paramMode: "0755",
			paramNoCOW: "true", paramCompression: "zstd",
		}))
		assert.True(t, vp.hasUpdates())
		assert.Equal(t, 1000, *vp.UID)
		assert.Equal(t, 2000, *vp.GID)
		assert.Equal(t, "0755", *vp.Mode)
		assert.True(t, *vp.NoCOW)
		assert.Equal(t, "zstd", *vp.Compression)
	})

	t.Run("invalid_uid", func(t *testing.T) {
		var vp volumeParams
		require.Error(t, vp.applyStringParams(map[string]string{paramUID: "abc"}))
	})

	t.Run("invalid_gid", func(t *testing.T) {
		var vp volumeParams
		require.Error(t, vp.applyStringParams(map[string]string{paramGID: "-1.5"}))
	})

	t.Run("invalid_mode", func(t *testing.T) {
		var vp volumeParams
		require.Error(t, vp.applyStringParams(map[string]string{paramMode: "999"}))
	})

	t.Run("invalid_nocow", func(t *testing.T) {
		var vp volumeParams
		require.Error(t, vp.applyStringParams(map[string]string{paramNoCOW: "yes"}))
	})

	t.Run("invalid_compression", func(t *testing.T) {
		var vp volumeParams
		require.Error(t, vp.applyStringParams(map[string]string{paramCompression: "bzip2"}))
	})

	t.Run("update_request", func(t *testing.T) {
		var vp volumeParams
		require.NoError(t, vp.applyStringParams(map[string]string{paramUID: "1000"}))
		req := vp.updateRequest()
		require.NotNil(t, req.UID)
		assert.Equal(t, 1000, *req.UID)
	})

	t.Run("labels_in_update", func(t *testing.T) {
		labels := map[string]string{"env": "prod"}
		vp := volumeParams{Labels: labels}
		assert.True(t, vp.hasUpdates())
		req := vp.updateRequest()
		require.NotNil(t, req.Labels)
		assert.Equal(t, labels, *req.Labels)
	})
}

// --- TestMergeUserLabels ---

func TestMergeUserLabels(t *testing.T) {
	t.Run("basic_merge", func(t *testing.T) {
		dst := map[string]string{"created-by": "k8s"}
		user := map[string]string{"env": "prod", "team": "be"}
		skipped := mergeUserLabels(dst, user, 4)
		assert.Empty(t, skipped)
		assert.Equal(t, "k8s", dst["created-by"])
		assert.Equal(t, "prod", dst["env"])
		assert.Equal(t, "be", dst["team"])
	})

	t.Run("reserved_keys_skipped", func(t *testing.T) {
		dst := map[string]string{"kubernetes.pvc.name": "my-pvc"}
		user := map[string]string{"kubernetes.pvc.name": "hacked", "env": "prod"}
		skipped := mergeUserLabels(dst, user, 4)
		assert.Equal(t, []string{"kubernetes.pvc.name"}, skipped)
		assert.Equal(t, "my-pvc", dst["kubernetes.pvc.name"])
		assert.Equal(t, "prod", dst["env"])
	})

	t.Run("max_user_labels", func(t *testing.T) {
		dst := map[string]string{}
		user := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
		skipped := mergeUserLabels(dst, user, 4)
		assert.Len(t, skipped, 1)
		assert.Equal(t, "e", skipped[0])
		assert.Len(t, dst, 4)
	})

	t.Run("deterministic_truncation", func(t *testing.T) {
		dst1 := map[string]string{}
		dst2 := map[string]string{}
		user := map[string]string{"z": "1", "a": "2", "m": "3", "b": "4", "x": "5"}
		mergeUserLabels(dst1, user, 3)
		mergeUserLabels(dst2, user, 3)
		assert.Equal(t, dst1, dst2)
		assert.Contains(t, dst1, "a")
		assert.Contains(t, dst1, "b")
		assert.Contains(t, dst1, "m")
	})

	t.Run("created_by_reserved", func(t *testing.T) {
		dst := map[string]string{"created-by": "k8s"}
		user := map[string]string{"created-by": "terraform"}
		skipped := mergeUserLabels(dst, user, 4)
		assert.Equal(t, []string{"created-by"}, skipped)
		assert.Equal(t, "k8s", dst["created-by"])
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
					annoPrefix + paramNoCOW: "true",
					annoPrefix + paramUID:   "1000",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("my-sc"),
			},
		}
		srv := &Server{kubeClient: fakekube.NewSimpleClientset(pvc), recorder: record.NewFakeRecorder(10)}

		params := map[string]string{
			csiserver.PvcNameKey:      "my-pvc",
			csiserver.PvcNamespaceKey: "default",
		}
		vp, err := srv.resolveVolumeParams(context.Background(), params)
		require.NoError(t, err)

		assert.Equal(t, "my-sc", vp.StorageClass)
		require.NotNil(t, vp.NoCOW)
		assert.True(t, *vp.NoCOW)
		require.NotNil(t, vp.UID)
		assert.Equal(t, 1000, *vp.UID)
		assert.Equal(t, "my-pvc", vp.Labels["kubernetes.pvc.name"])
		assert.Equal(t, "default", vp.Labels["kubernetes.pvc.namespace"])
		assert.Equal(t, "my-sc", vp.Labels["kubernetes.pvc.storageclassname"])
	})

	t.Run("no_pvc_info", func(t *testing.T) {
		srv := &Server{kubeClient: fakekube.NewSimpleClientset(), recorder: record.NewFakeRecorder(10)}

		vp, err := srv.resolveVolumeParams(context.Background(), map[string]string{
			paramNoCOW: "true",
		})
		require.NoError(t, err)

		require.NotNil(t, vp.NoCOW)
		assert.True(t, *vp.NoCOW)
		assert.Empty(t, vp.StorageClass)
		assert.Nil(t, vp.Labels)
	})

	t.Run("pvc_not_found", func(t *testing.T) {
		srv := &Server{kubeClient: fakekube.NewSimpleClientset(), recorder: record.NewFakeRecorder(10)}

		vp, err := srv.resolveVolumeParams(context.Background(), map[string]string{
			csiserver.PvcNameKey:      "missing",
			csiserver.PvcNamespaceKey: "default",
		})
		require.NoError(t, err)

		assert.Empty(t, vp.StorageClass)
		assert.Nil(t, vp.Labels)
	})
}
