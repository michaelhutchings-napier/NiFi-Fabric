#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric}"
CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE:-cert-manager}"
BOOTSTRAP_ISSUER_NAME="${BOOTSTRAP_ISSUER_NAME:-nifi-selfsigned-bootstrap}"
ROOT_CA_CERT_NAME="${ROOT_CA_CERT_NAME:-nifi-root-ca}"
CLUSTER_ISSUER_NAME="${CLUSTER_ISSUER_NAME:-nifi-ca}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

dump_diagnostics() {
  set +e
  echo
  echo "==> cert-manager bootstrap diagnostics"
  kubectl config current-context || true
  kubectl get ns || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" get deployment,pod,issuer,certificate,secret || true
  kubectl get clusterissuer || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" describe issuer "${BOOTSTRAP_ISSUER_NAME}" || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" describe certificate "${ROOT_CA_CERT_NAME}" || true
  kubectl describe clusterissuer "${CLUSTER_ISSUER_NAME}" || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
}

trap 'dump_diagnostics; exit 1' ERR

require_command kind
require_command kubectl
require_command helm

kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true

bash "${ROOT_DIR}/hack/install-cert-manager.sh"
bash "${ROOT_DIR}/hack/bootstrap-cert-manager-issuer.sh"

echo
echo "PASS: cert-manager bootstrap is ready on kind cluster ${KIND_CLUSTER_NAME}"
echo "  chart source: jetstack/cert-manager"
echo "  bootstrap Issuer: ${BOOTSTRAP_ISSUER_NAME}"
echo "  CA Certificate: ${ROOT_CA_CERT_NAME}"
echo "  ClusterIssuer: ${CLUSTER_ISSUER_NAME}"
