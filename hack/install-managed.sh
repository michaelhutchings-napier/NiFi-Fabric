#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-nifi-fabric-controller-manager}"

run_make() {
  make -C "${ROOT_DIR}" "$@"
}

ensure_kind_cluster() {
  if ! kind get clusters 2>/dev/null | grep -qx "${KIND_CLUSTER_NAME}"; then
    echo "==> Creating kind cluster ${KIND_CLUSTER_NAME}"
    run_make kind-up
  else
    echo "==> Reusing kind cluster ${KIND_CLUSTER_NAME}"
  fi

  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
}

echo "==> Installing managed NiFi-Fabric evaluator path"
ensure_kind_cluster
run_make kind-load-nifi-image
run_make kind-secrets
run_make install-crd
run_make docker-build-controller
run_make kind-load-controller
run_make deploy-controller
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
run_make helm-install-managed
run_make apply-managed

cat <<EOF

Managed install submitted.

Next commands:
  make kind-health
  kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE}
  kubectl -n ${NAMESPACE} get pods
  kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200
EOF
