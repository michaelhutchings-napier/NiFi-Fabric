#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-compat}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
SKIP_CONTROLLER_BUILD="${SKIP_CONTROLLER_BUILD:-false}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
COMPATIBILITY_VERSIONS="${COMPATIBILITY_VERSIONS:-2.0.0 2.1.0 2.2.0 2.3.0 2.4.0 2.5.0 2.6.0 2.7.0 2.8.0}"
START_EPOCH="$(date +%s)"
CURRENT_PHASE="${CURRENT_PHASE:-bootstrap}"
CURRENT_VERSION="${CURRENT_VERSION:-}"

run_make() {
  make -C "${ROOT_DIR}" "$@"
}

elapsed() {
  echo "$(( $(date +%s) - START_EPOCH ))"
}

configure_kind_kubeconfig() {
  local kubeconfig_path="${TMPDIR:-/tmp}/${KIND_CLUSTER_NAME}.kubeconfig"
  kind get kubeconfig --name "${KIND_CLUSTER_NAME}" >"${kubeconfig_path}"
  export KUBECONFIG="${kubeconfig_path}"
}

wait_for_namespace_deleted() {
  local namespace="$1"
  local deadline=$(( $(date +%s) + 300 ))

  while kubectl get namespace "${namespace}" >/dev/null 2>&1; do
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for namespace ${namespace} to delete" >&2
      return 1
    fi
    sleep 5
  done
}

reset_install_state() {
  helm uninstall "${HELM_RELEASE}" -n "${NAMESPACE}" >/dev/null 2>&1 || true
  kubectl delete namespace "${NAMESPACE}" --ignore-not-found >/dev/null 2>&1 || true
  if [[ "${CONTROLLER_NAMESPACE}" != "${NAMESPACE}" ]]; then
    kubectl delete namespace "${CONTROLLER_NAMESPACE}" --ignore-not-found >/dev/null 2>&1 || true
  fi
  wait_for_namespace_deleted "${NAMESPACE}"
  if [[ "${CONTROLLER_NAMESPACE}" != "${NAMESPACE}" ]]; then
    wait_for_namespace_deleted "${CONTROLLER_NAMESPACE}"
  fi
}

sts_image() {
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null || true
}

assert_sts_image() {
  local expected="$1"
  local actual
  actual="$(sts_image)"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "expected StatefulSet image ${expected}, got ${actual:-<empty>}" >&2
    return 1
  fi
}

dump_diagnostics() {
  set +e
  echo
  echo "==> NiFi version sweep diagnostics after failure at +$(elapsed)s"
  echo "  failed phase: ${CURRENT_PHASE}"
  echo "  current version: ${CURRENT_VERSION:-n/a}"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,svc,pvc,secret || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

phase "Checking prerequisites"
check_prereqs

CURRENT_PHASE="bootstrap-kind"
phase "Creating fresh shared kind cluster for NiFi 2.x version sweep"
kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
configure_kind_kubeconfig

CURRENT_PHASE="create-core-secrets"
phase "Creating shared TLS and auth Secrets"
CURRENT_PHASE="build-load-controller"
if [[ "${SKIP_CONTROLLER_BUILD}" == "true" ]]; then
  phase "Loading prebuilt controller image"
else
  phase "Building and loading controller image"
  run_make docker-build-controller
fi
run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"

for version in ${COMPATIBILITY_VERSIONS}; do
  CURRENT_VERSION="${version}"
  CURRENT_PHASE="reset-install-state"
  phase "Resetting install state for NiFi ${version}"
  reset_install_state

  CURRENT_PHASE="create-core-secrets"
  phase "Creating TLS and auth Secrets for NiFi ${version}"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

  CURRENT_PHASE="load-nifi-image"
  phase "Loading NiFi image apache/nifi:${version} into shared kind cluster"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="apache/nifi:${version}"

  CURRENT_PHASE="helm-upgrade"
  phase "Installing managed release for NiFi ${version}"
  helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
    --namespace "${NAMESPACE}" \
    --create-namespace \
    -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}" \
    -f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}" \
    --set-string "nifi.image.repository=apache/nifi" \
    --set-string "nifi.image.tag=${version}" \
    --set-string "nifi.image.pullPolicy=IfNotPresent"

  CURRENT_PHASE="verify-controller"
  phase "Verifying controller rollout for NiFi ${version}"
  kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m

  CURRENT_PHASE="verify-image"
  phase "Verifying StatefulSet image for NiFi ${version}"
  assert_sts_image "apache/nifi:${version}"

  CURRENT_PHASE="verify-health"
  phase "Verifying NiFi ${version} health"
  run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
done

print_success_footer "NiFi 2.x version sweep completed in +$(elapsed)s" \
  "make kind-health KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}" \
  "kubectl -n ${NAMESPACE} get statefulset ${HELM_RELEASE} -o jsonpath='{.spec.template.spec.containers[0].image}{\"\\n\"}'" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml"
