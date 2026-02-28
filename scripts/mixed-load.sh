#!/usr/bin/env bash
# mixed-load.sh - fio I/O load + API chaos for btrfs-nfs-csi
#
# Usage: mixed-load.sh [pods] [read_rate] [write_rate] [namespace]
#        mixed-load.sh cleanup [namespace]
set -euo pipefail

KUBECTL="kubectl"
PODS="${1:-10}"
READ_RATE="${2:-10m}"
WRITE_RATE="${3:-10m}"
NAMESPACE="${4:-default}"
SC="btrfs-nfs"
SNAP_CLASS="btrfs-nfs"
PREFIX="mixload"
FIO_IMAGE="nixery.dev/fio"
ERRORS=0

info()  { printf '\033[1;34m[INFO]\033[0m  %s\n' "$*"; }
warn()  { printf '\033[1;33m[WARN]\033[0m  %s\n' "$*"; }
error() { printf '\033[1;31m[ERROR]\033[0m %s\n' "$*" >&2; ERRORS=$((ERRORS + 1)); }
pass()  { printf '\033[1;32m[PASS]\033[0m  %s\n' "$*"; }
fail()  { printf '\033[1;31m[FAIL]\033[0m  %s\n' "$*"; }

ms() { echo $(( ($2 - $1) / 1000000 )); }

# --- cleanup ---
if [[ "${1:-}" == "cleanup" ]]; then
    NAMESPACE="${2:-default}"
    info "Cleaning up mixed-load resources..."
    ${KUBECTL} delete pod -n "${NAMESPACE}" -l app=${PREFIX} --ignore-not-found --force --grace-period=0 2>/dev/null || true
    ${KUBECTL} delete volumesnapshot -n "${NAMESPACE}" -l app=${PREFIX} --ignore-not-found
    ${KUBECTL} delete pvc -n "${NAMESPACE}" -l app=${PREFIX} --ignore-not-found
    info "Cleanup done."
    exit 0
fi

MANIFEST=$(mktemp /tmp/mixload-XXXXXX.yaml)
trap 'rm -f "${MANIFEST}"' EXIT

declare -A TIMINGS
TOTAL_START=$(date +%s%N)

info "=== btrfs-nfs-csi mixed load test ==="
info "Pods: ${PODS} | Read: ${READ_RATE}/s | Write: ${WRITE_RATE}/s | Namespace: ${NAMESPACE}"
echo ""

# --- phase 1: create PVCs + fio pods ---
info "Phase 1: Creating ${PODS} PVCs + fio pods..."
for i in $(seq 1 "${PODS}"); do
    name="${PREFIX}-$(printf '%03d' "${i}")"
    cat >> "${MANIFEST}" <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${name}
  namespace: ${NAMESPACE}
  labels:
    app: ${PREFIX}
spec:
  accessModes: [ReadWriteMany]
  storageClassName: ${SC}
  resources:
    requests:
      storage: 1Gi
---
apiVersion: v1
kind: Pod
metadata:
  name: ${name}
  namespace: ${NAMESPACE}
  labels:
    app: ${PREFIX}
spec:
  restartPolicy: Never
  containers:
    - name: fio
      image: ${FIO_IMAGE}
      command: ["fio"]
      args: ["--name=mixed", "--directory=/data", "--rw=randrw", "--rwmixread=70", "--bs=4k", "--size=256m", "--rate=${READ_RATE},${WRITE_RATE}", "--runtime=120", "--time_based", "--group_reporting", "--output-format=terse", "--terse-version=3"]
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: ${name}
---
EOF
done

START=$(date +%s%N)
${KUBECTL} apply -f "${MANIFEST}"
END=$(date +%s%N)
TIMINGS[create]=$(ms "$START" "$END")
info "Apply took ${TIMINGS[create]}ms"

# wait for pods to be running
info "Waiting for pods to start..."
TIMEOUT=300; ELAPSED=0
while [[ $ELAPSED -lt $TIMEOUT ]]; do
    running=$(${KUBECTL} get pod -n "${NAMESPACE}" -l app=${PREFIX} --no-headers 2>/dev/null | grep -c Running || true)
    [[ $running -ge $PODS ]] && break
    printf "\r  %d/%d running (%ds)" "${running}" "${PODS}" "${ELAPSED}"
    sleep 2
    ELAPSED=$((ELAPSED + 2))
done
echo ""
if [[ $running -lt $PODS ]]; then
    error "Only ${running}/${PODS} pods running after ${TIMEOUT}s"
else
    info "All ${PODS} pods running."
fi
echo ""

# --- phase 2: API chaos while fio runs ---
info "Phase 2: API chaos while fio writes..."

# snapshot all volumes
info "  Snapshotting all ${PODS} volumes..."
SNAP_MANIFEST=$(mktemp /tmp/mixload-snap-XXXXXX.yaml)
trap 'rm -f "${MANIFEST}" "${SNAP_MANIFEST}"' EXIT
for i in $(seq 1 "${PODS}"); do
    name="${PREFIX}-$(printf '%03d' "${i}")"
    cat >> "${SNAP_MANIFEST}" <<EOF
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: ${name}-snap
  namespace: ${NAMESPACE}
  labels:
    app: ${PREFIX}
spec:
  volumeSnapshotClassName: ${SNAP_CLASS}
  source:
    persistentVolumeClaimName: ${name}
---
EOF
done
START=$(date +%s%N)
${KUBECTL} apply -f "${SNAP_MANIFEST}"
END=$(date +%s%N)
TIMINGS[snap_during_io]=$(ms "$START" "$END")
info "  Snapshots applied in ${TIMINGS[snap_during_io]}ms"

# resize half while fio runs
HALF=$((PODS / 2))
info "  Resizing first ${HALF} PVCs to 2Gi..."
START=$(date +%s%N)
for i in $(seq 1 "${HALF}"); do
    name="${PREFIX}-$(printf '%03d' "${i}")"
    ${KUBECTL} patch pvc -n "${NAMESPACE}" "${name}" -p '{"spec":{"resources":{"requests":{"storage":"2Gi"}}}}' &
done
wait
END=$(date +%s%N)
TIMINGS[resize_during_io]=$(ms "$START" "$END")
info "  Resize patches in ${TIMINGS[resize_during_io]}ms"

# delete snapshots while fio still runs
info "  Deleting snapshots..."
START=$(date +%s%N)
${KUBECTL} delete volumesnapshot -n "${NAMESPACE}" -l app=${PREFIX} --wait=false
END=$(date +%s%N)
TIMINGS[snap_delete]=$(ms "$START" "$END")
info "  Snapshots deleted in ${TIMINGS[snap_delete]}ms"
echo ""

# --- phase 3: wait for fio to finish ---
info "Phase 3: Waiting for fio pods to complete (max 180s)..."
TIMEOUT=180; ELAPSED=0
while [[ $ELAPSED -lt $TIMEOUT ]]; do
    completed=$(${KUBECTL} get pod -n "${NAMESPACE}" -l app=${PREFIX} --no-headers 2>/dev/null | grep -c "Completed\|Succeeded" || true)
    failed=$(${KUBECTL} get pod -n "${NAMESPACE}" -l app=${PREFIX} --no-headers 2>/dev/null | grep -c "Error\|CrashLoopBackOff" || true)
    running=$(${KUBECTL} get pod -n "${NAMESPACE}" -l app=${PREFIX} --no-headers 2>/dev/null | grep -c Running || true)
    done_count=$((completed + failed))
    [[ $done_count -ge $PODS ]] && break
    printf "\r  running=%d completed=%d failed=%d (%ds)" "${running}" "${completed}" "${failed}" "${ELAPSED}"
    sleep 5
    ELAPSED=$((ELAPSED + 5))
done
echo ""

if [[ $failed -gt 0 ]]; then
    error "${failed} fio pod(s) failed"
fi
info "Completed: ${completed}/${PODS}, Failed: ${failed}/${PODS}"
echo ""

# --- phase 4: collect fio results ---
info "Phase 4: Collecting fio results..."
TOTAL_READ_BW=0
TOTAL_WRITE_BW=0
TOTAL_READ_IOPS=0
TOTAL_WRITE_IOPS=0

for i in $(seq 1 "${PODS}"); do
    name="${PREFIX}-$(printf '%03d' "${i}")"
    output=$(${KUBECTL} logs -n "${NAMESPACE}" "${name}" 2>/dev/null || true)
    if [[ -n "${output}" ]]; then
        # terse v3 format: field 7=read_bw_kib, 8=read_iops, 48=write_bw_kib, 49=write_iops
        read_bw=$(echo "${output}" | awk -F';' '{print $7}' | tail -1)
        read_iops=$(echo "${output}" | awk -F';' '{print $8}' | tail -1)
        write_bw=$(echo "${output}" | awk -F';' '{print $48}' | tail -1)
        write_iops=$(echo "${output}" | awk -F';' '{print $49}' | tail -1)
        TOTAL_READ_BW=$((TOTAL_READ_BW + ${read_bw:-0}))
        TOTAL_WRITE_BW=$((TOTAL_WRITE_BW + ${write_bw:-0}))
        TOTAL_READ_IOPS=$((TOTAL_READ_IOPS + ${read_iops:-0}))
        TOTAL_WRITE_IOPS=$((TOTAL_WRITE_IOPS + ${write_iops:-0}))
    fi
done

TOTAL_END=$(date +%s%N)
TOTAL_MS=$(ms "$TOTAL_START" "$TOTAL_END")

# --- summary ---
echo ""
info "=== Results ==="
info "Config:          ${PODS} pods, read=${READ_RATE}/s, write=${WRITE_RATE}/s"
info "Create:          ${TIMINGS[create]}ms"
info "Snap during IO:  ${TIMINGS[snap_during_io]}ms"
info "Resize during IO:${TIMINGS[resize_during_io]}ms"
info "Snap delete:     ${TIMINGS[snap_delete]}ms"
info "Aggregate Read:  $((TOTAL_READ_BW / 1024))MB/s, ${TOTAL_READ_IOPS} IOPS"
info "Aggregate Write: $((TOTAL_WRITE_BW / 1024))MB/s, ${TOTAL_WRITE_IOPS} IOPS"
info "Total:           ${TOTAL_MS}ms"
echo ""

if [[ $ERRORS -eq 0 ]]; then
    pass "Mixed load test completed — 0 errors"
    exit 0
else
    fail "${ERRORS} error(s) during test"
    exit 1
fi
