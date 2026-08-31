#!/usr/bin/env bash
# setup.sh — Create a Kind cluster and deploy the Incident Investigator controller.
# Usage: ./deploy/kind/setup.sh [--skip-build]
#
# Prerequisites: kind, kubectl, docker, make

set -euo pipefail

CLUSTER_NAME="incident-investigator"
IMAGE_NAME="incident-investigator:latest"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SKIP_BUILD="${1:-}"

echo "==> Incident Investigator — Kind cluster setup"
echo "    Repo root : ${REPO_ROOT}"
echo "    Cluster   : ${CLUSTER_NAME}"
echo "    Image     : ${IMAGE_NAME}"
echo ""

# ── Step 1: Create the Kind cluster ──────────────────────────────────────────
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  echo ">>> Kind cluster '${CLUSTER_NAME}' already exists, reusing."
else
  echo ">>> Creating Kind cluster '${CLUSTER_NAME}'..."
  kind create cluster \
    --name "${CLUSTER_NAME}" \
    --config "${SCRIPT_DIR}/kind-config.yaml" \
    --wait 60s
  echo ">>> Cluster created."
fi

# Switch kubectl context to the new cluster.
kubectl config use-context "kind-${CLUSTER_NAME}"

# ── Step 2: Build the controller image ───────────────────────────────────────
if [[ "${SKIP_BUILD}" != "--skip-build" ]]; then
  echo ">>> Building controller image ${IMAGE_NAME}..."
  cd "${REPO_ROOT}"
  make docker-build IMG="${IMAGE_NAME}"
  echo ">>> Image built."
else
  echo ">>> Skipping image build (--skip-build specified)."
fi

# ── Step 3: Load the image into Kind ─────────────────────────────────────────
echo ">>> Loading image into Kind cluster..."
kind load docker-image "${IMAGE_NAME}" --name "${CLUSTER_NAME}"
echo ">>> Image loaded."

# ── Step 4: Install CRDs ─────────────────────────────────────────────────────
echo ">>> Installing CRDs..."
cd "${REPO_ROOT}"
kubectl apply -f config/crd/bases/
echo ">>> CRDs installed."

# ── Step 5: Apply RBAC ───────────────────────────────────────────────────────
echo ">>> Applying RBAC..."
kubectl apply -f config/rbac/service_account.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -f config/rbac/role_binding.yaml
kubectl apply -f config/rbac/leader_election_role.yaml
kubectl apply -f config/rbac/leader_election_role_binding.yaml
echo ">>> RBAC applied."

# ── Step 6: Deploy the controller manager ────────────────────────────────────
echo ">>> Deploying controller manager..."
# Patch the manager.yaml image to use the locally loaded image (not pulled from registry).
kubectl apply -f config/manager/manager.yaml
kubectl set image deployment/controller-manager \
  manager="${IMAGE_NAME}" \
  -n incident-investigator-system 2>/dev/null || true
kubectl patch deployment controller-manager \
  -n incident-investigator-system \
  --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"Never"}]' \
  2>/dev/null || true

echo ">>> Waiting for controller manager to be ready..."
kubectl rollout status deployment/controller-manager \
  -n incident-investigator-system \
  --timeout=120s

echo ""
echo "==> Setup complete!"
echo "    Run './deploy/kind/validate.sh' to run the e2e validation."
echo "    Run 'kubectl get incidentreports -A' to inspect incidents."
