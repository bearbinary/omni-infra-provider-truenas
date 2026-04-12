#!/usr/bin/env bash
# test-longhorn.sh — Validate Longhorn storage on a live cluster
#
# Prerequisites:
#   - kubectl context set to the target cluster
#   - Longhorn installed in longhorn-system namespace
#   - NFS StorageClass is the default (Longhorn is non-default)
#
# Usage:
#   ./scripts/test-longhorn.sh [--cleanup-only]

set -uo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'
PASS=0
FAIL=0
NAMESPACE="longhorn-test"

log()  { echo -e "${GREEN}[PASS]${NC} $1"; ((PASS++)); }
fail() { echo -e "${RED}[FAIL]${NC} $1"; ((FAIL++)); }
info() { echo -e "${YELLOW}[INFO]${NC} $1"; }

cleanup() {
    info "Cleaning up test resources..."
    kubectl delete namespace "$NAMESPACE" --ignore-not-found --grace-period=0 --force 2>/dev/null || true
    kubectl delete pvc longhorn-test-pvc default-sc-test --ignore-not-found 2>/dev/null || true
    info "Cleanup complete"
}

if [[ "${1:-}" == "--cleanup-only" ]]; then
    cleanup
    exit 0
fi

trap cleanup EXIT

echo "========================================="
echo " Longhorn Storage Validation Test Suite"
echo "========================================="
echo ""

# ─── Test 1: Longhorn StorageClass exists and is NOT default ───
info "Test 1: StorageClass 'longhorn' exists and is not default"
SC_EXISTS=$(kubectl get sc longhorn -o name 2>/dev/null || echo "")
SC_DEFAULT=$(kubectl get sc longhorn -o jsonpath='{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}' 2>/dev/null || echo "")
if [[ -n "$SC_EXISTS" && "$SC_DEFAULT" != "true" ]]; then
    log "StorageClass 'longhorn' exists and is not default"
elif [[ -z "$SC_EXISTS" ]]; then
    fail "StorageClass 'longhorn' does not exist"
else
    fail "StorageClass 'longhorn' is marked as default (should not be)"
fi

# ─── Test 2: Longhorn pods are healthy ───
info "Test 2: All Longhorn pods are Running"
NOT_RUNNING=$(kubectl -n longhorn-system get pods --no-headers 2>/dev/null | grep -cv "Running\|Completed" | tr -d '[:space:]' || echo "0")
TOTAL=$(kubectl -n longhorn-system get pods --no-headers 2>/dev/null | wc -l | tr -d '[:space:]')
if [[ "$NOT_RUNNING" -eq 0 && "$TOTAL" -gt 0 ]]; then
    log "All $TOTAL Longhorn pods are Running"
else
    fail "$NOT_RUNNING of $TOTAL Longhorn pods are not Running"
    kubectl -n longhorn-system get pods --no-headers 2>/dev/null | grep -v "Running\|Completed" || true
fi

# ─── Test 3: Longhorn nodes are schedulable ───
info "Test 3: Longhorn nodes are Ready and Schedulable"
LH_NODES=$(kubectl -n longhorn-system get nodes.longhorn.io --no-headers 2>/dev/null | wc -l | tr -d ' ')
LH_READY=$(kubectl -n longhorn-system get nodes.longhorn.io --no-headers 2>/dev/null | awk '$2=="True" && $3=="true" && $4=="True"' | wc -l | tr -d ' ')
if [[ "$LH_READY" -eq "$LH_NODES" && "$LH_NODES" -gt 0 ]]; then
    log "All $LH_NODES Longhorn nodes are Ready and Schedulable"
else
    fail "Only $LH_READY of $LH_NODES Longhorn nodes are Ready+Schedulable"
    kubectl -n longhorn-system get nodes.longhorn.io 2>/dev/null || true
fi

# ─── Test 4: Longhorn nodes use /var/lib/longhorn on the data disk ───
info "Test 4: Longhorn data path is /var/lib/longhorn"
WORKERS=$(kubectl -n longhorn-system get nodes.longhorn.io -o name 2>/dev/null)
DATA_PATH_OK=true
for NODE in $WORKERS; do
    NODE_NAME=$(echo "$NODE" | sed 's|node.longhorn.io/||')
    PATH_VAL=$(kubectl -n longhorn-system get "$NODE" -o jsonpath='{.spec.disks}' 2>/dev/null | python3 -c "
import sys, json
disks = json.load(sys.stdin)
paths = [d['path'] for d in disks.values()]
print(' '.join(paths))
" 2>/dev/null || echo "")
    if [[ "$PATH_VAL" != *"/var/lib/longhorn"* ]]; then
        fail "Node $NODE_NAME data path is '$PATH_VAL' (expected /var/lib/longhorn)"
        DATA_PATH_OK=false
    fi
done
if $DATA_PATH_OK; then
    log "All Longhorn nodes use /var/lib/longhorn"
fi

# ─── Test 5: PVC provisioning ───
info "Test 5: PVC with storageClassName=longhorn binds"
kubectl apply -f - <<EOF 2>/dev/null
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: longhorn-test-pvc
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: longhorn
  resources:
    requests:
      storage: 1Gi
EOF

for i in $(seq 1 60); do
    PVC_STATUS=$(kubectl get pvc longhorn-test-pvc -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ "$PVC_STATUS" == "Bound" ]]; then break; fi
    sleep 1
done

if [[ "$PVC_STATUS" == "Bound" ]]; then
    log "Longhorn PVC bound successfully"
else
    fail "Longhorn PVC did not bind within 60s (status: '$PVC_STATUS')"
fi

# ─── Test 6: Pod write/read ───
info "Test 6: Pod can write and read data on Longhorn volume"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - 2>/dev/null
kubectl label namespace "$NAMESPACE" pod-security.kubernetes.io/enforce=privileged --overwrite 2>/dev/null

kubectl apply -n "$NAMESPACE" -f - <<EOF 2>/dev/null
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: writer-data
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: longhorn
  resources:
    requests:
      storage: 1Gi
---
apiVersion: v1
kind: Pod
metadata:
  name: longhorn-writer
spec:
  containers:
    - name: writer
      image: busybox:1.36
      command: ["sh", "-c", "echo 'longhorn-sentinel-2026' > /data/test.txt && cat /data/test.txt"]
      volumeMounts:
        - name: data
          mountPath: /data
  restartPolicy: Never
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: writer-data
EOF

for i in $(seq 1 90); do
    PHASE=$(kubectl get pod -n "$NAMESPACE" longhorn-writer -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ "$PHASE" == "Succeeded" || "$PHASE" == "Failed" ]]; then break; fi
    sleep 1
done

if [[ "$PHASE" == "Succeeded" ]]; then
    OUTPUT=$(kubectl logs -n "$NAMESPACE" longhorn-writer 2>/dev/null || echo "")
    if [[ "$OUTPUT" == *"longhorn-sentinel-2026"* ]]; then
        log "Pod wrote and read data from Longhorn volume"
    else
        fail "Pod completed but output unexpected: '$OUTPUT'"
    fi
else
    fail "Writer pod did not succeed (phase: '$PHASE')"
    kubectl describe pod -n "$NAMESPACE" longhorn-writer 2>/dev/null | tail -10 || true
fi

# ─── Test 7: Data persistence across pod deletion ───
info "Test 7: Data survives pod deletion"
kubectl delete pod -n "$NAMESPACE" longhorn-writer --wait 2>/dev/null || true

kubectl apply -n "$NAMESPACE" -f - <<EOF 2>/dev/null
apiVersion: v1
kind: Pod
metadata:
  name: longhorn-reader
spec:
  containers:
    - name: reader
      image: busybox:1.36
      command: ["sh", "-c", "cat /data/test.txt"]
      volumeMounts:
        - name: data
          mountPath: /data
  restartPolicy: Never
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: writer-data
EOF

for i in $(seq 1 90); do
    PHASE=$(kubectl get pod -n "$NAMESPACE" longhorn-reader -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ "$PHASE" == "Succeeded" || "$PHASE" == "Failed" ]]; then break; fi
    sleep 1
done

if [[ "$PHASE" == "Succeeded" ]]; then
    OUTPUT=$(kubectl logs -n "$NAMESPACE" longhorn-reader 2>/dev/null || echo "")
    if [[ "$OUTPUT" == *"longhorn-sentinel-2026"* ]]; then
        log "Data persisted across pod deletion"
    else
        fail "Reader pod completed but data mismatch: '$OUTPUT'"
    fi
else
    fail "Reader pod did not succeed (phase: '$PHASE')"
fi

# ─── Test 8: Default StorageClass still routes to NFS ───
info "Test 8: PVC without storageClassName defaults to NFS"
kubectl apply -f - <<EOF 2>/dev/null
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: default-sc-test
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
EOF

for i in $(seq 1 30); do
    PVC_STATUS=$(kubectl get pvc default-sc-test -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ "$PVC_STATUS" == "Bound" ]]; then break; fi
    sleep 1
done

PROVISIONER=$(kubectl get pvc default-sc-test -o jsonpath='{.spec.storageClassName}' 2>/dev/null || echo "")
if [[ "$PROVISIONER" == "nfs" ]]; then
    log "Default PVC routed to NFS StorageClass"
else
    fail "Default PVC used '$PROVISIONER' instead of 'nfs'"
fi

# ─── Test 9: PVC deletion cleans up PV ───
info "Test 9: Longhorn PVC deletion triggers PV cleanup"
PV_NAME=$(kubectl get pvc longhorn-test-pvc -o jsonpath='{.spec.volumeName}' 2>/dev/null || echo "")
kubectl delete pvc longhorn-test-pvc 2>/dev/null || true

if [[ -n "$PV_NAME" ]]; then
    for i in $(seq 1 60); do
        PV_EXISTS=$(kubectl get pv "$PV_NAME" --no-headers 2>/dev/null | wc -l | tr -d ' ')
        if [[ "$PV_EXISTS" == "0" ]]; then break; fi
        sleep 1
    done

    if [[ "$PV_EXISTS" == "0" ]]; then
        log "PV '$PV_NAME' cleaned up after PVC deletion"
    else
        fail "PV '$PV_NAME' still exists after 60s"
    fi
else
    fail "Could not get PV name from PVC"
fi

# ─── Summary ───
echo ""
echo "========================================="
echo " Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"
echo "========================================="

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
