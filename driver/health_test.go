package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/mount-utils"
	"k8s.io/utils/ptr"
)

func newTestNodeServer(mps []mount.MountPoint) *NodeServer {
	return &NodeServer{
		nodeID:  "test-node",
		nodeIP:  "10.0.0.1",
		mounter: mount.NewFakeMounter(mps),
	}
}

func newTestNodeServerWithKube(mps []mount.MountPoint, objects ...metav1.Object) *NodeServer {
	ns := newTestNodeServer(mps)
	// Build fake clientset with VolumeAttachments + PVs
	var runtimeObjects []metav1.Object
	runtimeObjects = append(runtimeObjects, objects...)
	// Convert to runtime.Object for fake clientset
	var objs []interface {
		GetObjectKind() interface{ GroupVersionKind() interface{} }
	}
	_ = objs // unused, use the simpler approach below

	fakeClient := fakekube.NewSimpleClientset()
	// Manually create objects via the fake client
	for _, obj := range objects {
		switch o := obj.(type) {
		case *storagev1.VolumeAttachment:
			fakeClient.StorageV1().VolumeAttachments().Create(context.Background(), o, metav1.CreateOptions{})
		case *corev1.PersistentVolume:
			fakeClient.CoreV1().PersistentVolumes().Create(context.Background(), o, metav1.CreateOptions{})
		}
	}
	ns.kubeClient = fakeClient
	return ns
}

// --- Mount filtering tests ---

func TestCheckMountHealth_SkipsNonNFS(t *testing.T) {
	ns := newTestNodeServer([]mount.MountPoint{
		{Device: "/dev/sda1", Path: "/var/lib/kubelet/globalmount/vol", Type: "ext4"},
		{Device: "tmpfs", Path: "/tmp", Type: "tmpfs"},
	})

	ns.checkMountHealth(context.Background())

	fm := ns.mounter.(*mount.FakeMounter)
	assert.Empty(t, fm.GetLog())
}

func TestCheckMountHealth_SkipsNonGlobalmount(t *testing.T) {
	ns := newTestNodeServer([]mount.MountPoint{
		{Device: "10.0.0.5:/exports/vol", Path: "/some/other/path", Type: "nfs4"},
	})

	ns.checkMountHealth(context.Background())

	fm := ns.mounter.(*mount.FakeMounter)
	assert.Empty(t, fm.GetLog())
}

func TestCheckMountHealth_HealthyMount(t *testing.T) {
	globalmountDir := filepath.Join(t.TempDir(), "globalmount", "vol")
	require.NoError(t, os.MkdirAll(filepath.Join(globalmountDir, "data"), 0755))

	ns := newTestNodeServer([]mount.MountPoint{
		{Device: "10.0.0.5:/exports/vol", Path: globalmountDir, Type: "nfs4"},
	})

	ns.checkMountHealth(context.Background())

	fm := ns.mounter.(*mount.FakeMounter)
	assert.Empty(t, fm.GetLog())
}

// --- Heal mount tests ---

func TestHealMount_Success(t *testing.T) {
	ns := newTestNodeServer(nil)

	mp := mount.MountPoint{
		Device: "10.0.0.5:/exports/vol",
		Path:   "/var/lib/kubelet/plugins/csi/globalmount/vol",
		Type:   "nfs4",
		Opts:   []string{"rw", "hard", "vers=4.2"},
	}

	ns.healMount(context.Background(), mp, nil)

	fm := ns.mounter.(*mount.FakeMounter)
	actions := fm.GetLog()
	require.Len(t, actions, 1)
	assert.Equal(t, "mount", actions[0].Action)
	assert.Equal(t, mp.Device, actions[0].Source)
	assert.Equal(t, mp.Path, actions[0].Target)
	assert.Equal(t, "nfs4", actions[0].FSType)
}

func TestHealMount_SetsHealthStateHealed(t *testing.T) {
	ns := newTestNodeServer(nil)

	mp := mount.MountPoint{
		Device: "10.0.0.5:/exports/vol",
		Path:   "/var/lib/kubelet/plugins/csi/globalmount/vol",
		Type:   "nfs4",
	}
	vi := &volumeInfo{volumeID: "sc|vol-123", pvcName: "my-pvc", pvcNamespace: "default"}

	ns.healMount(context.Background(), mp, vi)

	h, ok := ns.healthState.Load("sc|vol-123")
	require.True(t, ok)
	vh := h.(*volumeHealth)
	assert.False(t, vh.abnormal)
	assert.Contains(t, vh.message, "auto-healed")
}

func TestHealMount_SetsHealthStateAbnormalOnFailure(t *testing.T) {
	ns := newTestNodeServer(nil)
	vi := &volumeInfo{volumeID: "sc|vol-fail"}

	ns.reportVolumeEvent(context.Background(), vi, eventMountRemountFailed, "remount failed: connection refused")

	h, ok := ns.healthState.Load("sc|vol-fail")
	require.True(t, ok)
	vh := h.(*volumeHealth)
	assert.True(t, vh.abnormal)
	assert.Contains(t, vh.message, "remount failed")
}

// --- Event tests (with fake kubeClient) ---

func TestReportVolumeEvent_CreatesK8sEvent(t *testing.T) {
	ns := newTestNodeServer(nil)
	ns.kubeClient = fakekube.NewSimpleClientset()

	vi := &volumeInfo{volumeID: "sc|vol-1", pvcName: "my-pvc", pvcNamespace: "default"}

	ns.reportVolumeEvent(context.Background(), vi, eventMountAutoHealed, "NFS mount was stale, auto-healed")

	events, err := ns.kubeClient.CoreV1().Events("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, events.Items, 1)

	ev := events.Items[0]
	assert.Equal(t, eventMountAutoHealed, ev.Reason)
	assert.Equal(t, "NFS mount was stale, auto-healed", ev.Message)
	assert.Equal(t, "PersistentVolumeClaim", ev.InvolvedObject.Kind)
	assert.Equal(t, "my-pvc", ev.InvolvedObject.Name)
	assert.Equal(t, "default", ev.InvolvedObject.Namespace)
	assert.Equal(t, corev1.EventTypeWarning, ev.Type)
	assert.Equal(t, "btrfs-nfs-csi-node", ev.Source.Component)
}

func TestReportVolumeEvent_RemountFailedEvent(t *testing.T) {
	ns := newTestNodeServer(nil)
	ns.kubeClient = fakekube.NewSimpleClientset()

	vi := &volumeInfo{volumeID: "sc|vol-1", pvcName: "my-pvc", pvcNamespace: "default"}

	ns.reportVolumeEvent(context.Background(), vi, eventMountRemountFailed, "remount failed: server down")

	events, err := ns.kubeClient.CoreV1().Events("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, events.Items, 1)
	assert.Equal(t, eventMountRemountFailed, events.Items[0].Reason)
}

func TestReportVolumeEvent_NilVolumeInfo(t *testing.T) {
	ns := newTestNodeServer(nil)
	ns.reportVolumeEvent(context.Background(), nil, eventMountAutoHealed, "test")
}

func TestReportVolumeEvent_NoKubeClient(t *testing.T) {
	ns := newTestNodeServer(nil)
	// kubeClient is nil, should still set healthState
	vi := &volumeInfo{volumeID: "sc|vol-1", pvcName: "pvc", pvcNamespace: "ns"}

	ns.reportVolumeEvent(context.Background(), vi, eventMountAutoHealed, "healed")

	h, ok := ns.healthState.Load("sc|vol-1")
	require.True(t, ok)
	assert.False(t, h.(*volumeHealth).abnormal)
}

func TestReportVolumeEvent_NoPVCSkipsEvent(t *testing.T) {
	ns := newTestNodeServer(nil)
	ns.kubeClient = fakekube.NewSimpleClientset()

	// No pvcName/Namespace, should still set healthState but no event
	vi := &volumeInfo{volumeID: "sc|vol-1"}

	ns.reportVolumeEvent(context.Background(), vi, eventMountAutoHealed, "healed")

	events, _ := ns.kubeClient.CoreV1().Events("").List(context.Background(), metav1.ListOptions{})
	assert.Empty(t, events.Items)

	_, ok := ns.healthState.Load("sc|vol-1")
	assert.True(t, ok)
}

// --- BuildVolumeMap tests ---

func TestBuildVolumeMap_ResolvesVolumes(t *testing.T) {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-1"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "btrfs-nfs-csi",
					VolumeHandle: "sc|vol-abc",
					VolumeAttributes: map[string]string{
						"nfsServer":    "10.0.0.5",
						"nfsSharePath": "/exports/tenant/vol-abc",
					},
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Name:      "my-pvc",
				Namespace: "default",
			},
		},
	}
	va := &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{Name: "va-1"},
		Spec: storagev1.VolumeAttachmentSpec{
			Attacher: "btrfs-nfs-csi",
			NodeName: "test-node",
			Source:   storagev1.VolumeAttachmentSource{PersistentVolumeName: ptr.To("pv-1")},
		},
		Status: storagev1.VolumeAttachmentStatus{Attached: true},
	}

	ns := newTestNodeServerWithKube(nil, va, pv)

	vm := ns.buildVolumeMap(context.Background())

	require.Len(t, vm, 1)
	vi, ok := vm["10.0.0.5:/exports/tenant/vol-abc"]
	require.True(t, ok)
	assert.Equal(t, "sc|vol-abc", vi.volumeID)
	assert.Equal(t, "my-pvc", vi.pvcName)
	assert.Equal(t, "default", vi.pvcNamespace)
}

func TestBuildVolumeMap_SkipsOtherDrivers(t *testing.T) {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-other"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "other-driver",
					VolumeHandle: "other|vol",
				},
			},
		},
	}
	va := &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{Name: "va-other"},
		Spec: storagev1.VolumeAttachmentSpec{
			Attacher: "other-driver",
			NodeName: "test-node",
			Source:   storagev1.VolumeAttachmentSource{PersistentVolumeName: ptr.To("pv-other")},
		},
		Status: storagev1.VolumeAttachmentStatus{Attached: true},
	}

	ns := newTestNodeServerWithKube(nil, va, pv)
	vm := ns.buildVolumeMap(context.Background())
	assert.Empty(t, vm)
}

func TestBuildVolumeMap_SkipsDetached(t *testing.T) {
	va := &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{Name: "va-detached"},
		Spec: storagev1.VolumeAttachmentSpec{
			Attacher: "btrfs-nfs-csi",
			NodeName: "test-node",
			Source:   storagev1.VolumeAttachmentSource{PersistentVolumeName: ptr.To("pv-1")},
		},
		Status: storagev1.VolumeAttachmentStatus{Attached: false},
	}

	ns := newTestNodeServerWithKube(nil, va)
	vm := ns.buildVolumeMap(context.Background())
	assert.Empty(t, vm)
}

func TestBuildVolumeMap_NoKubeClient(t *testing.T) {
	ns := newTestNodeServer(nil)
	vm := ns.buildVolumeMap(context.Background())
	assert.Empty(t, vm)
}

// --- healthState cleanup ---

func TestHealthStateCleanup(t *testing.T) {
	ns := newTestNodeServer(nil)
	ns.healthState.Store("vol-1", &volumeHealth{abnormal: true, message: "stale"})

	_, ok := ns.healthState.Load("vol-1")
	require.True(t, ok)

	ns.healthState.Delete("vol-1")

	_, ok = ns.healthState.Load("vol-1")
	assert.False(t, ok)
}

// --- VOLUME_CONDITION tests ---

func TestAttachVolumeCondition_Abnormal(t *testing.T) {
	ns := newTestNodeServer(nil)
	ns.healthState.Store("vol-1", &volumeHealth{abnormal: true, message: "stale mount"})

	resp := &csi.NodeGetVolumeStatsResponse{}
	ns.attachVolumeCondition("vol-1", resp)

	require.NotNil(t, resp.VolumeCondition)
	assert.True(t, resp.VolumeCondition.Abnormal)
	assert.Equal(t, "stale mount", resp.VolumeCondition.Message)
}

func TestAttachVolumeCondition_Healthy(t *testing.T) {
	ns := newTestNodeServer(nil)
	ns.healthState.Store("vol-1", &volumeHealth{abnormal: false, message: "healed"})

	resp := &csi.NodeGetVolumeStatsResponse{}
	ns.attachVolumeCondition("vol-1", resp)

	assert.Nil(t, resp.VolumeCondition)
}

func TestAttachVolumeCondition_NoEntry(t *testing.T) {
	ns := newTestNodeServer(nil)

	resp := &csi.NodeGetVolumeStatsResponse{}
	ns.attachVolumeCondition("vol-unknown", resp)

	assert.Nil(t, resp.VolumeCondition)
}

// --- findStagingPath tests ---

func TestFindStagingPath(t *testing.T) {
	ns := newTestNodeServer([]mount.MountPoint{
		{Device: "10.0.0.5:/exports/tenant/vol-abc", Path: "/var/lib/kubelet/plugins/csi/pv/pv-1/globalmount", Type: "nfs4"},
		{Device: "10.0.0.5:/exports/tenant/vol-xyz", Path: "/var/lib/kubelet/plugins/csi/pv/pv-2/globalmount", Type: "nfs4"},
		{Device: "/dev/sda1", Path: "/", Type: "ext4"},
	})

	t.Run("found", func(t *testing.T) {
		path := ns.findStagingPath("sc|vol-abc")
		assert.Equal(t, "/var/lib/kubelet/plugins/csi/pv/pv-1/globalmount", path)
	})

	t.Run("not_found", func(t *testing.T) {
		path := ns.findStagingPath("sc|vol-missing")
		assert.Empty(t, path)
	})

	t.Run("invalid_volume_id", func(t *testing.T) {
		path := ns.findStagingPath("invalid")
		assert.Empty(t, path)
	})
}
