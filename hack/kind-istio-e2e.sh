#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-istio}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_ISTIO_VALUES_FILE="${PLATFORM_ISTIO_VALUES_FILE:-examples/platform-managed-istio-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
ISTIO_NAMESPACE="${ISTIO_NAMESPACE:-istio-system}"
ISTIO_TEMP_DIR="${ISTIO_TEMP_DIR:-${TMPDIR:-/tmp}/nifi-fabric-istio}"
START_EPOCH="$(date +%s)"

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

ensure_istioctl() {
  if command -v istioctl >/dev/null 2>&1; then
    return 0
  fi

  local download_dir="${ISTIO_TEMP_DIR}/download"
  rm -rf "${download_dir}"
  mkdir -p "${download_dir}"
  (
    cd "${download_dir}"
    curl -sSfL https://istio.io/downloadIstio | sh - >/dev/null
  )
  local istio_dir
  istio_dir="$(find "${download_dir}" -maxdepth 1 -mindepth 1 -type d -name 'istio-*' | head -n 1)"
  if [[ -z "${istio_dir}" ]]; then
    echo "failed to discover downloaded istioctl bundle under ${download_dir}" >&2
    return 1
  fi
  export PATH="${istio_dir}/bin:${PATH}"
  require_command istioctl
}

install_istio_control_plane() {
  phase "Installing Istio control plane"
  kubectl create namespace "${ISTIO_NAMESPACE}" >/dev/null 2>&1 || true
  istioctl install -y >/dev/null
  kubectl -n "${ISTIO_NAMESPACE}" rollout status deployment/istiod --timeout=5m >/dev/null
}

prepare_istio_namespace_injection() {
  phase "Enabling Istio sidecar injection for the NiFi namespace only"
  kubectl create namespace "${NAMESPACE}" >/dev/null 2>&1 || true
  kubectl label namespace "${NAMESPACE}" istio-injection=enabled --overwrite >/dev/null
}

verify_sidecar_injection() {
  phase "Verifying Istio sidecar injection on NiFi pods"
  local containers
  containers="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.spec.containers[*].name}')"
  if [[ "${containers}" != *"istio-proxy"* ]]; then
    echo "expected pod ${HELM_RELEASE}-0 to include istio-proxy, got: ${containers}" >&2
    return 1
  fi
}

verify_controller_not_meshed() {
  phase "Verifying the controller remains outside the mesh"
  local containers
  containers="$(kubectl -n "${CONTROLLER_NAMESPACE}" get pod -l app.kubernetes.io/component=controller -o jsonpath='{.items[0].spec.containers[*].name}')"
  if [[ "${containers}" == *"istio-proxy"* ]]; then
    echo "expected controller pod to remain outside the mesh, got containers: ${containers}" >&2
    return 1
  fi
}

verify_headless_cross_pod_https() {
  phase "Verifying direct pod-to-pod HTTPS over the headless Service"

  local expected_replicas username password target_host summary_json parsed connected connected_count total_count
  expected_replicas="$(kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}')"
  if [[ -z "${expected_replicas}" || "${expected_replicas}" -lt 2 ]]; then
    echo "expected at least two replicas for the bounded Istio proof, got ${expected_replicas:-<empty>}" >&2
    return 1
  fi
  username="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.username}' | base64 -d)"
  password="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.password}' | base64 -d)"
  target_host="${HELM_RELEASE}-1.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local"

  summary_json="$(
    kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- \
      env NIFI_HOST="${target_host}" NIFI_USERNAME="${username}" NIFI_PASSWORD="${password}" TLS_CA_PATH="/opt/nifi/tls/ca.crt" sh -ec '
        TOKEN=$(curl --silent --show-error --fail \
          --cacert "${TLS_CA_PATH}" \
          -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
          --data-urlencode "username=${NIFI_USERNAME}" \
          --data-urlencode "password=${NIFI_PASSWORD}" \
          "https://${NIFI_HOST}:8443/nifi-api/access/token")

        curl --silent --show-error --fail \
          --cacert "${TLS_CA_PATH}" \
          -H "Authorization: Bearer ${TOKEN}" \
          "https://${NIFI_HOST}:8443/nifi-api/flow/cluster/summary"
      '
  )"

  parsed="$(
    printf '%s' "${summary_json}" | python3 -c '
import json
import sys

summary = json.load(sys.stdin)["clusterSummary"]
print("\t".join([
    str(summary.get("connectedToCluster", False)).lower(),
    str(summary.get("connectedNodeCount", "")),
    str(summary.get("totalNodeCount", "")),
]))
'
  )"
  IFS=$'\t' read -r connected connected_count total_count <<< "${parsed}"

  if [[ "${connected}" != "true" || "${connected_count}" != "${expected_replicas}" || "${total_count}" != "${expected_replicas}" ]]; then
    echo "expected cross-pod cluster summary via ${target_host} to report connected=true and ${expected_replicas}/${expected_replicas} nodes, got connected=${connected} connectedNodeCount=${connected_count} totalNodeCount=${total_count}" >&2
    return 1
  fi
}

dump_diagnostics() {
  set +e
  echo
  echo "==> Istio compatibility diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  istioctl version || true
  kubectl -n "${ISTIO_NAMESPACE}" get deploy,pod,svc || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,svc,pod || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-headless" -o yaml || true
  kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o yaml || true
  kubectl -n "${NAMESPACE}" describe pod "${HELM_RELEASE}-0" || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c istio-proxy --tail=200 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
  -f "${ROOT_DIR}/${PLATFORM_ISTIO_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
check_prereqs
require_command curl
require_command python3
ensure_istioctl

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for Istio compatibility proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for Istio compatibility proof"
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
  configure_kind_kubeconfig

  phase "Loading NiFi image into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Creating TLS and auth Secrets"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
fi

install_istio_control_plane
prepare_istio_namespace_injection

phase "Installing product chart managed Istio profile${profile_label}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

phase "Verifying platform resources and controller rollout"
kubectl get crd nificlusters.platform.nifi.io >/dev/null
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

verify_sidecar_injection
verify_controller_not_meshed

phase "Verifying secured cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

verify_headless_cross_pod_https

print_success_footer "Istio compatibility proof completed" \
  "make kind-istio-fast-e2e-reuse" \
  "kubectl -n ${NAMESPACE} get sts ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get pods -o custom-columns=NAME:.metadata.name,CONTAINERS:.spec.containers[*].name" \
  "kubectl -n ${CONTROLLER_NAMESPACE} get pods -o custom-columns=NAME:.metadata.name,CONTAINERS:.spec.containers[*].name"
