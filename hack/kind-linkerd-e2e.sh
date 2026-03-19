#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-linkerd}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_LINKERD_VALUES_FILE="${PLATFORM_LINKERD_VALUES_FILE:-examples/platform-managed-linkerd-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
LINKERD_NAMESPACE="${LINKERD_NAMESPACE:-linkerd}"
LINKERD_TEMP_HOME="${LINKERD_TEMP_HOME:-${TMPDIR:-/tmp}/nifi-fabric-linkerd-home}"
GATEWAY_API_MANIFEST_URL="${GATEWAY_API_MANIFEST_URL:-https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml}"
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

ensure_linkerd_cli() {
  if command -v linkerd >/dev/null 2>&1; then
    return 0
  fi

  mkdir -p "${LINKERD_TEMP_HOME}"
  env HOME="${LINKERD_TEMP_HOME}" sh -c 'curl --proto "=https" --tlsv1.2 -sSfL https://run.linkerd.io/install | sh >/dev/null'
  export PATH="${LINKERD_TEMP_HOME}/.linkerd2/bin:${PATH}"
  require_command linkerd
}

install_gateway_api_crds() {
  if kubectl get crd gateways.gateway.networking.k8s.io >/dev/null 2>&1; then
    return 0
  fi

  phase "Installing Gateway API CRDs required by the Linkerd control plane"
  kubectl apply --server-side -f "${GATEWAY_API_MANIFEST_URL}" >/dev/null
  kubectl wait --for=condition=Established crd/gatewayclasses.gateway.networking.k8s.io --timeout=2m >/dev/null
  kubectl wait --for=condition=Established crd/gateways.gateway.networking.k8s.io --timeout=2m >/dev/null
  kubectl wait --for=condition=Established crd/httproutes.gateway.networking.k8s.io --timeout=2m >/dev/null
  kubectl wait --for=condition=Established crd/referencegrants.gateway.networking.k8s.io --timeout=2m >/dev/null
}

install_linkerd_control_plane() {
  phase "Installing Linkerd control plane"
  install_gateway_api_crds
  linkerd check --pre --wait 5m >/dev/null
  linkerd install --crds | kubectl apply -f - >/dev/null
  linkerd install | kubectl apply -f - >/dev/null
  linkerd check --wait 5m >/dev/null
}

verify_linkerd_data_plane() {
  phase "Verifying Linkerd data-plane health for meshed NiFi pods"
  linkerd check --proxy --namespace "${NAMESPACE}" --wait 5m >/dev/null
}

verify_sidecar_injection() {
  phase "Verifying Linkerd sidecar injection on NiFi pods"
  local containers
  containers="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.spec.containers[*].name}')"
  if [[ "${containers}" != *"linkerd-proxy"* ]]; then
    echo "expected pod ${HELM_RELEASE}-0 to include linkerd-proxy, got: ${containers}" >&2
    return 1
  fi
}

verify_headless_cross_pod_https() {
  phase "Verifying direct pod-to-pod HTTPS over the headless Service"

  local expected_replicas username password target_host summary_json parsed connected connected_count total_count
  expected_replicas="$(kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}')"
  if [[ -z "${expected_replicas}" || "${expected_replicas}" -lt 2 ]]; then
    echo "expected at least two replicas for the bounded Linkerd proof, got ${expected_replicas:-<empty>}" >&2
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
  echo "==> Linkerd compatibility diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  linkerd version || true
  linkerd check --proxy --namespace "${NAMESPACE}" || true
  kubectl -n "${LINKERD_NAMESPACE}" get deploy,pod,svc || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,svc,pod || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-headless" -o yaml || true
  kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o yaml || true
  kubectl -n "${NAMESPACE}" describe pod "${HELM_RELEASE}-0" || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c linkerd-proxy --tail=200 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
  -f "${ROOT_DIR}/${PLATFORM_LINKERD_VALUES_FILE}"
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
ensure_linkerd_cli

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for Linkerd compatibility proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for Linkerd compatibility proof"
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

install_linkerd_control_plane

phase "Installing product chart managed Linkerd profile${profile_label}"
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
verify_linkerd_data_plane

phase "Verifying secured cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

verify_headless_cross_pod_https

print_success_footer "Linkerd compatibility proof completed" \
  "make kind-linkerd-fast-e2e-reuse" \
  "linkerd check --proxy --namespace ${NAMESPACE}" \
  "kubectl -n ${NAMESPACE} get sts ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get svc ${HELM_RELEASE}-headless -o yaml" \
  "kubectl -n ${NAMESPACE} get pods -o custom-columns=NAME:.metadata.name,CONTAINERS:.spec.containers[*].name"
