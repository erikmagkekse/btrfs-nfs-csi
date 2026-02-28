#!/usr/bin/env bash
# create-100-pvcs.sh - creates/deletes PVCs using the btrfs-nfs StorageClass
#
# Usage: create-100-pvcs.sh [count] [size] [namespace] [storageclass]
#        create-100-pvcs.sh delete [count] [namespace]
set -euo pipefail

MANIFEST=$(mktemp /tmp/pvcs-XXXXXX.yaml)
trap 'rm -f "${MANIFEST}"' EXIT

if [[ "${1:-}" == "delete" ]]; then
    COUNT="${2:-100}"
    NAMESPACE="${3:-default}"
    kubectl delete pvc -n "${NAMESPACE}" $(for i in $(seq 1 "${COUNT}"); do printf "test-pvc-%03d " "${i}"; done) --ignore-not-found
    echo "Done. Deleted ${COUNT} PVCs in namespace ${NAMESPACE}."
    exit 0
fi

COUNT="${1:-100}"
SIZE="${2:-1Gi}"
NAMESPACE="${3:-default}"
STORAGE_CLASS="${4:-btrfs-nfs}"

for i in $(seq 1 "${COUNT}"); do
    name="test-pvc-$(printf '%03d' "${i}")"
    cat >> "${MANIFEST}" <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${name}
  namespace: ${NAMESPACE}
spec:
  accessModes:
    - ReadWriteMany
  storageClassName: ${STORAGE_CLASS}
  resources:
    requests:
      storage: ${SIZE}
---
EOF
done

echo "Rendered ${COUNT} PVCs to ${MANIFEST}"
kubectl apply -f "${MANIFEST}"
echo -e "\nDone. Created ${COUNT} PVCs in namespace ${NAMESPACE}."
echo "Check status: kubectl get pvc -n ${NAMESPACE}"
