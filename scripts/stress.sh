#!/bin/bash

NAMESPACE=${1:-"default"}
STORAGECLASS=${2:-"btrfs-nfs"}
TIMEOUT=${3:-"3600s"}

STS_NAME="btrfs-stress-$(cat /dev/urandom | tr -dc 'a-z0-9' | head -c 6)"

log() {
  echo "[$(date '+%H:%M:%S')] [$NAMESPACE] [$STS_NAME] $1"
}

trap "log 'Cleanup...'; kubectl delete statefulset $STS_NAME -n $NAMESPACE --ignore-not-found; exit" SIGINT SIGTERM

log "Deploying StatefulSet..."
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: $STS_NAME
  namespace: $NAMESPACE
spec:
  serviceName: $STS_NAME
  replicas: 1
  selector:
    matchLabels:
      app: $STS_NAME
  template:
    metadata:
      labels:
        app: $STS_NAME
    spec:
      terminationGracePeriodSeconds: 10
      containers:
        - name: debian
          image: debian:latest
          command: ["sleep", "infinity"]
          lifecycle:
            preStop:
              exec:
                command: ["sleep", "5"]
          volumeMounts:
            - name: data
              mountPath: /data
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: $STORAGECLASS
        resources:
          requests:
            storage: 1Gi
  persistentVolumeClaimRetentionPolicy:
    whenDeleted: Delete
    whenScaled: Delete
EOF

log "Waiting for StatefulSet to be ready..."
kubectl rollout status statefulset/$STS_NAME -n $NAMESPACE --timeout=$TIMEOUT

while true; do
  log "Scaling up to 10..."
  kubectl scale statefulset $STS_NAME -n $NAMESPACE --replicas=10
  kubectl rollout status statefulset/$STS_NAME -n $NAMESPACE --timeout=$TIMEOUT

  log "Scaling down to 5..."
  kubectl scale statefulset $STS_NAME -n $NAMESPACE --replicas=5
  kubectl rollout status statefulset/$STS_NAME -n $NAMESPACE --timeout=$TIMEOUT

  log "Scaling down to 0..."
  kubectl scale statefulset $STS_NAME -n $NAMESPACE --replicas=0

  log "Waiting for all pods to terminate..."
  while kubectl get pods -n $NAMESPACE -l app=$STS_NAME 2>/dev/null | grep -q "Running\|Terminating\|Pending"; do
    sleep 2
  done

  log "Waiting for PVC cleanup..."
  while kubectl get pvc -n $NAMESPACE -l app=$STS_NAME 2>/dev/null | grep -q "Bound\|Terminating"; do
    sleep 2
  done

  log "Loop complete, restarting..."
  sleep 2
done
