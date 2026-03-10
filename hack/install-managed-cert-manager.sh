#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"
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
    info "Creating kind cluster ${KIND_CLUSTER_NAME}"
    run_make kind-up
  else
    info "Reusing kind cluster ${KIND_CLUSTER_NAME}"
  fi

  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
}

trap 'print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

phase "Checking prerequisites"
check_cert_manager_prereqs

phase "Installing managed NiFi-Fabric evaluator path with cert-manager"
ensure_kind_cluster

phase "Loading NiFi image into kind"
run_make kind-load-nifi-image

phase "Bootstrapping cert-manager and issuer flow"
run_make kind-bootstrap-cert-manager
run_make kind-cert-manager-secrets

phase "Installing CRD"
run_make install-crd

phase "Building and loading controller image"
run_make docker-build-controller
run_make kind-load-controller

phase "Deploying controller"
run_make deploy-controller
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m

phase "Installing managed Helm release with cert-manager overlay"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  -f "${ROOT_DIR}/examples/managed/values.yaml" \
  -f "${ROOT_DIR}/examples/cert-manager-values.yaml"

phase "Applying NiFiCluster"
kubectl apply -f "${ROOT_DIR}/examples/managed/nificluster.yaml"

print_success_footer "managed cert-manager install submitted" \
  "make kind-health" \
  "kubectl -n ${NAMESPACE} get certificate,secret" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE}" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200" \
  "kubectl -n ${NAMESPACE} get events --sort-by=.lastTimestamp | tail -n 50"
