#!/usr/bin/env bash
# stress-test.sh - API stress + lifecycle chaos for btrfs-nfs-csi
#
# Usage: stress-test.sh [count] [rapid] [namespace]
#        stress-test.sh cleanup [namespace]
set -euo pipefail

KUBECTL="kubectl"
COUNT="${1:-50}"
RAPID="${2:-20}"
NAMESPACE="${3:-default}"
SC="btrfs-nfs"
SNAP_CLASS="btrfs-nfs"
PREFIX="stress"
ERRORS=0

info()  { printf '\033[1;34m[INFO]\033[0m  %s\n' "$*"; }
warn()  { printf '\033[1;33m[WARN]\033[0m  %s\n' "$*"; }
error() { printf '\033[1;31m[ERROR]\033[0m %s\n' "$*" >&2; ERRORS=$((ERRORS + 1)); }
pass()  { printf '\033[1;32m[PASS]\033[0m  %s\n' "$*"; }
fail()  { printf '\033[1;31m[FAIL]\033[0m  %s\n' "$*"; }

ms() { echo $(( ($2 - $1) / 1000000 )); }

wait_for_pvcs() {
    local target="$1" elapsed=0
    local timeout=$(( target > 120 ? target : 120 ))
    while [[ $elapsed -lt $timeout ]]; do
        bound=$(${KUBECTL} get pvc -n "${NAMESPACE}" -l app=${PREFIX} --no-headers 2>/dev/null | grep -c Bound || true)
        [[ $bound -ge $target ]] && return 0
        printf "\r  %d/%d bound (%ds/%ds)" "${bound}" "${target}" "${elapsed}" "${timeout}"
        sleep 2
        elapsed=$((elapsed + 2))
    done
    echo ""
    error "Timeout: only ${bound}/${target} PVCs bound after ${timeout}s — continuing anyway"
}

wait_for_snapshots() {
    local target="$1" elapsed=0
    local timeout=$(( target > 120 ? target : 120 ))
    while [[ $elapsed -lt $timeout ]]; do
        ready=$(${KUBECTL} get volumesnapshot -n "${NAMESPACE}" -l app=${PREFIX} -o jsonpath='{range .items[*]}{.status.readyToUse}{"\n"}{end}' 2>/dev/null | grep -c true || true)
        [[ $ready -ge $target ]] && return 0
        printf "\r  %d/%d ready (%ds/%ds)" "${ready}" "${target}" "${elapsed}" "${timeout}"
        sleep 2
        elapsed=$((elapsed + 2))
    done
    echo ""
    error "Timeout: only ${ready}/${target} snapshots ready after ${timeout}s — continuing anyway"
}

wait_for_deletes() {
    local elapsed=0 timeout=120
    while [[ $elapsed -lt $timeout ]]; do
        remaining=$(${KUBECTL} get pvc -n "${NAMESPACE}" -l app=${PREFIX} --no-headers 2>/dev/null | wc -l || true)
        [[ $remaining -eq 0 ]] && return 0
        printf "\r  %d remaining (%ds/%ds)" "${remaining}" "${elapsed}" "${timeout}"
        sleep 2
        elapsed=$((elapsed + 2))
    done
    echo ""
    error "${remaining} PVCs still remaining after ${timeout}s"
}

# --- cleanup ---
if [[ "${1:-}" == "cleanup" ]]; then
    NAMESPACE="${2:-default}"
    info "Cleaning up stress test resources..."
    ${KUBECTL} delete volumesnapshot -n "${NAMESPACE}" -l app=${PREFIX} --ignore-not-found
    ${KUBECTL} delete pvc -n "${NAMESPACE}" -l app=${PREFIX} --ignore-not-found
    info "Cleanup done."
    exit 0
fi

MANIFEST=$(mktemp /tmp/stress-XXXXXX.yaml)
SNAP_MANIFEST=$(mktemp /tmp/stress-snap-XXXXXX.yaml)
trap 'rm -f "${MANIFEST}" "${SNAP_MANIFEST}"' EXIT

declare -A TIMINGS

info "=== btrfs-nfs-csi stress test ==="
info "Count: ${COUNT} | Rapid: ${RAPID} | Namespace: ${NAMESPACE}"
TOTAL_START=$(date +%s%N)
echo ""

# --- phase 1: bulk create ---
info "Phase 1: Creating ${COUNT} PVCs..."
for i in $(seq 1 "${COUNT}"); do
    cat >> "${MANIFEST}" <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${PREFIX}-$(printf '%03d' "${i}")
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
EOF
done
START=$(date +%s%N)
${KUBECTL} apply -f "${MANIFEST}"
END=$(date +%s%N)
TIMINGS[create_apply]=$(ms "$START" "$END")
info "Apply took ${TIMINGS[create_apply]}ms"

info "Waiting for all PVCs to bind..."
START=$(date +%s%N)
wait_for_pvcs "${COUNT}"
END=$(date +%s%N)
TIMINGS[create_bind]=$(ms "$START" "$END")
echo ""

# --- phase 2: snapshot all ---
info "Phase 2: Snapshotting all ${COUNT} PVCs..."
for i in $(seq 1 "${COUNT}"); do
    cat >> "${SNAP_MANIFEST}" <<EOF
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: ${PREFIX}-snap-$(printf '%03d' "${i}")
  namespace: ${NAMESPACE}
  labels:
    app: ${PREFIX}
spec:
  volumeSnapshotClassName: ${SNAP_CLASS}
  source:
    persistentVolumeClaimName: ${PREFIX}-$(printf '%03d' "${i}")
---
EOF
done
START=$(date +%s%N)
${KUBECTL} apply -f "${SNAP_MANIFEST}"
END=$(date +%s%N)
TIMINGS[snap_apply]=$(ms "$START" "$END")
info "Snapshot apply took ${TIMINGS[snap_apply]}ms"

info "Waiting for snapshots to be ready..."
START=$(date +%s%N)
wait_for_snapshots "${COUNT}"
END=$(date +%s%N)
TIMINGS[snap_ready]=$(ms "$START" "$END")
echo ""

# --- phase 3: resize half ---
HALF=$((COUNT / 2))
info "Phase 3: Resizing first ${HALF} PVCs to 2Gi..."
START=$(date +%s%N)
for i in $(seq 1 "${HALF}"); do
    name="${PREFIX}-$(printf '%03d' "${i}")"
    ${KUBECTL} patch pvc -n "${NAMESPACE}" "${name}" -p '{"spec":{"resources":{"requests":{"storage":"2Gi"}}}}' &
done
wait
END=$(date +%s%N)
TIMINGS[resize_patch]=$(ms "$START" "$END")
info "Resize patches took ${TIMINGS[resize_patch]}ms"

info "Waiting for resizes to complete..."
START=$(date +%s%N)
TIMEOUT=$(( HALF > 120 ? HALF : 120 )); ELAPSED=0
while [[ $ELAPSED -lt $TIMEOUT ]]; do
    resized=$(${KUBECTL} get pvc -n "${NAMESPACE}" -l app=${PREFIX} -o jsonpath='{range .items[*]}{.status.capacity.storage}{"\n"}{end}' 2>/dev/null | grep -c "2Gi" || true)
    [[ $resized -ge $HALF ]] && break
    printf "\r  %d/%d resized (%ds/%ds)" "${resized}" "${HALF}" "${ELAPSED}" "${TIMEOUT}"
    sleep 2
    ELAPSED=$((ELAPSED + 2))
done
END=$(date +%s%N)
TIMINGS[resize_done]=$(ms "$START" "$END")
if [[ $resized -lt $HALF ]]; then
    echo ""
    error "Timeout: only ${resized}/${HALF} PVCs resized — continuing anyway"
else
    echo ""
    info "All ${HALF} PVCs resized."
fi
echo ""

# --- phase 4: delete snapshots + PVCs ---
info "Phase 4: Deleting all snapshots..."
START=$(date +%s%N)
${KUBECTL} delete volumesnapshot -n "${NAMESPACE}" -l app=${PREFIX} --wait=false
END=$(date +%s%N)
TIMINGS[snap_delete]=$(ms "$START" "$END")
info "Snapshot delete took ${TIMINGS[snap_delete]}ms"

info "Deleting all PVCs..."
START=$(date +%s%N)
${KUBECTL} delete pvc -n "${NAMESPACE}" -l app=${PREFIX} --wait=false
END=$(date +%s%N)
TIMINGS[pvc_delete]=$(ms "$START" "$END")
info "PVC delete took ${TIMINGS[pvc_delete]}ms"

info "Waiting for deletes to finish..."
wait_for_deletes
echo ""

# --- phase 5: rapid lifecycle ---
info "Phase 5: Rapid lifecycle - ${RAPID} rounds of create+snapshot+delete..."
START=$(date +%s%N)
for i in $(seq 1 "${RAPID}"); do
    name="${PREFIX}-rapid-$(printf '%03d' "${i}")"

    ${KUBECTL} apply -n "${NAMESPACE}" -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${name}
  labels:
    app: ${PREFIX}
spec:
  accessModes: [ReadWriteMany]
  storageClassName: ${SC}
  resources:
    requests:
      storage: 1Gi
EOF

    for _ in $(seq 1 30); do
        phase=$(${KUBECTL} get pvc -n "${NAMESPACE}" "${name}" -o jsonpath='{.status.phase}' 2>/dev/null || true)
        [[ "${phase}" == "Bound" ]] && break
        sleep 1
    done

    ${KUBECTL} apply -n "${NAMESPACE}" -f - <<EOF
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: ${name}-snap
  labels:
    app: ${PREFIX}
spec:
  volumeSnapshotClassName: ${SNAP_CLASS}
  source:
    persistentVolumeClaimName: ${name}
EOF

    ${KUBECTL} delete volumesnapshot -n "${NAMESPACE}" "${name}-snap" --wait=false --ignore-not-found
    ${KUBECTL} delete pvc -n "${NAMESPACE}" "${name}" --wait=false --ignore-not-found
    echo "  Round ${i}/${RAPID} done"
done
END=$(date +%s%N)
TIMINGS[rapid]=$(ms "$START" "$END")
info "Rapid lifecycle took ${TIMINGS[rapid]}ms"

TOTAL_END=$(date +%s%N)
TOTAL_MS=$(ms "$TOTAL_START" "$TOTAL_END")
echo ""

# --- summary ---
echo ""
info "=== Results ==="
info "Config:          ${COUNT} PVCs, ${RAPID} rapid, ns=${NAMESPACE}"
info "Bulk Create:     ${TIMINGS[create_apply]}ms apply, ${TIMINGS[create_bind]}ms bind"
info "Snapshots:       ${TIMINGS[snap_apply]}ms apply, ${TIMINGS[snap_ready]}ms ready"
info "Resize:          ${TIMINGS[resize_patch]}ms patch, ${TIMINGS[resize_done]}ms complete"
info "Bulk Delete:     ${TIMINGS[snap_delete]}ms snaps, ${TIMINGS[pvc_delete]}ms pvcs"
info "Rapid Lifecycle: ${TIMINGS[rapid]}ms"
info "Total:           ${TOTAL_MS}ms"
echo ""

if [[ $ERRORS -eq 0 ]]; then
    pass "All phases completed — 0 errors"
    exit 0
else
    fail "${ERRORS} error(s) during test"
    exit 1
fi
