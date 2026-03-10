#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"

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

echo "==> Installing standalone NiFi-Fabric evaluator path"
ensure_kind_cluster
run_make kind-load-nifi-image
run_make kind-secrets
run_make helm-install-standalone

cat <<EOF

Standalone install submitted.

Next commands:
  make kind-health
  kubectl -n ${NAMESPACE} get pods
  kubectl -n ${NAMESPACE} get sts ${HELM_RELEASE}
  kubectl -n ${NAMESPACE} get secrets
EOF
