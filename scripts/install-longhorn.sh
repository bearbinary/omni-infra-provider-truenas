#!/usr/bin/env bash
# install-longhorn.sh — Install Longhorn on an Omni-managed Talos cluster
#
# This script:
#   1. Applies the Talos machine config patch for Longhorn (via omnictl)
#   2. Installs Longhorn via Helm
#   3. Sets Longhorn as the default StorageClass
#   4. Verifies with a test PVC
#
# Prerequisites:
#   - omnictl authenticated and configured
#   - kubectl context set to the target cluster
#   - helm v3 installed
#
# Usage:
#   ./scripts/install-longhorn.sh <cluster-name>
#   ./scripts/install-longhorn.sh talos-default

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info() { echo -e "${YELLOW}[INFO]${NC} $1"; }
pass() { echo -e "${GREEN}[DONE]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; exit 1; }

# ─── Args ───
CLUSTER="${1:-}"
if [[ -z "$CLUSTER" ]]; then
    echo "Usage: $0 <cluster-name>"
    echo "Example: $0 talos-default"
    exit 1
fi

# ─── Preflight ───
info "Checking prerequisites..."
command -v omnictl >/dev/null 2>&1 || fail "omnictl not found. Install from https://omni.siderolabs.com"
command -v kubectl >/dev/null 2>&1 || fail "kubectl not found"
command -v helm >/dev/null 2>&1    || fail "helm not found. Install from https://helm.sh"

kubectl get nodes >/dev/null 2>&1 || fail "kubectl cannot reach the cluster. Set the right context."
pass "Prerequisites OK"

# ─── Step 1: Talos config patch for Longhorn ───
info "Step 1/4: Applying Talos config patch for Longhorn..."

PATCH_ID="longhorn-${CLUSTER}"

# Check if patch already exists
if omnictl get configpatch "$PATCH_ID" >/dev/null 2>&1; then
    info "Config patch '$PATCH_ID' already exists, skipping"
else
    cat <<EOF | omnictl apply -f -
metadata:
  namespace: default
  type: ConfigPatches.omni.sidero.dev
  id: ${PATCH_ID}
  labels:
    omni.sidero.dev/cluster: ${CLUSTER}
spec:
  data: |
    machine:
      kubelet:
        extraMounts:
          - destination: /var/lib/longhorn
            type: bind
            source: /var/lib/longhorn
            options:
              - bind
              - rshared
              - rw
      sysctls:
        vm.overcommit_memory: "1"
EOF

    if [[ $? -eq 0 ]]; then
        pass "Talos config patch applied"
    else
        fail "Failed to apply Talos config patch"
    fi
fi

info "Waiting for nodes to reconcile the config patch (this may take up to 60s)..."
sleep 10

# ─── Step 2: Install Longhorn via Helm ───
info "Step 2/4: Installing Longhorn via Helm..."

# Pin to a tested chart version for reproducibility. Update after testing new releases.
LONGHORN_VERSION="1.7.2"

helm repo add longhorn https://charts.longhorn.io 2>/dev/null
helm repo update longhorn 2>/dev/null

if helm status longhorn -n longhorn-system >/dev/null 2>&1; then
    info "Longhorn already installed, upgrading to v${LONGHORN_VERSION}..."
    helm upgrade longhorn longhorn/longhorn \
        --namespace longhorn-system \
        --version "$LONGHORN_VERSION" \
        --set defaultSettings.defaultDataPath=/var/lib/longhorn \
        --wait --timeout 5m
else
    helm install longhorn longhorn/longhorn \
        --namespace longhorn-system \
        --create-namespace \
        --version "$LONGHORN_VERSION" \
        --set defaultSettings.defaultDataPath=/var/lib/longhorn \
        --wait --timeout 5m
fi

if [[ $? -eq 0 ]]; then
    pass "Longhorn installed"
else
    fail "Longhorn Helm install failed"
fi

# ─── Step 3: Set as default StorageClass ───
info "Step 3/4: Setting Longhorn as default StorageClass..."

# Remove default annotation from any existing default SC
for sc in $(kubectl get sc -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}' 2>/dev/null); do
    if [[ "$sc" != "longhorn" ]]; then
        kubectl patch storageclass "$sc" -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"false"}}}' 2>/dev/null
        info "Removed default from StorageClass '$sc'"
    fi
done

kubectl patch storageclass longhorn -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}' 2>/dev/null
pass "Longhorn is the default StorageClass"

# ─── Step 4: Verify with test PVC ───
info "Step 4/4: Verifying with a test PVC..."

kubectl apply -f - <<EOF 2>/dev/null
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: longhorn-test-pvc
  namespace: default
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 256Mi
  storageClassName: longhorn
EOF

# Wait for bind
BOUND=false
for i in $(seq 1 60); do
    STATUS=$(kubectl get pvc longhorn-test-pvc -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ "$STATUS" == "Bound" ]]; then
        BOUND=true
        break
    fi
    sleep 2
done

# Cleanup test PVC
kubectl delete pvc longhorn-test-pvc --ignore-not-found 2>/dev/null

if [[ "$BOUND" == "true" ]]; then
    pass "Test PVC bound successfully — Longhorn is working"
else
    echo -e "${YELLOW}[WARN]${NC} Test PVC did not bind within 120s. This may be normal if:"
    echo "  - Worker nodes are still reconciling the Talos config patch"
    echo "  - Worker VMs don't have a data disk (add storage_disk_size to MachineClass)"
    echo "  Check: kubectl get pods -n longhorn-system"
fi

echo ""
echo "========================================="
echo " Longhorn installed on cluster: $CLUSTER"
echo "========================================="
echo ""
echo "Next steps:"
echo "  kubectl get sc              # Verify 'longhorn' is default"
echo "  kubectl get pods -n longhorn-system  # Check Longhorn pods"
echo ""
echo "Longhorn UI (port-forward):"
echo "  kubectl -n longhorn-system port-forward svc/longhorn-frontend 8080:80"
echo "  Open http://localhost:8080"
