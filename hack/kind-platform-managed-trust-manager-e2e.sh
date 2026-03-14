#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-platform-managed-trust-manager}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
TRUST_BUNDLE_NAME="${TRUST_BUNDLE_NAME:-${HELM_RELEASE}-trust-bundle}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
TRUST_MANAGER_NAMESPACE="${TRUST_MANAGER_NAMESPACE:-cert-manager}"
TRUST_MANAGER_RELEASE="${TRUST_MANAGER_RELEASE:-trust-manager}"
TRUST_MANAGER_DEPLOYMENT="${TRUST_MANAGER_DEPLOYMENT:-trust-manager}"
TRUST_SOURCE_SECRET_NAME="${TRUST_SOURCE_SECRET_NAME:-${HELM_RELEASE}-tls-ca-source}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
TRUST_MANAGER_VALUES_FILE="${TRUST_MANAGER_VALUES_FILE:-examples/platform-managed-trust-manager-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"
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

wait_for_nificluster_condition() {
  local type="$1"
  local timeout_seconds="${2:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    local actual
    actual="$(kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath="{.status.conditions[?(@.type==\"${type}\")].status}" 2>/dev/null || true)"
    if [[ "${actual}" == "True" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "expected condition ${type}=True, got ${actual:-<empty>}" >&2
      return 1
    fi
    sleep 5
  done
}

wait_for_configmap_key() {
  local name="$1"
  local key="$2"
  local timeout_seconds="${3:-180}"
  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    local value
    value="$(kubectl -n "${NAMESPACE}" get configmap "${name}" -o "jsonpath={.data.${key//./\\.}}" 2>/dev/null || true)"
    if [[ -n "${value}" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "expected ConfigMap/${name} key ${key} to be populated" >&2
      return 1
    fi
    sleep 5
  done
}

dump_diagnostics() {
  set +e
  echo
  echo "==> platform managed trust-manager diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  helm -n "${TRUST_MANAGER_NAMESPACE}" status "${TRUST_MANAGER_RELEASE}" || true
  kubectl get crd nificlusters.platform.nifi.io bundles.trust.cert-manager.io -o name || true
  kubectl -n "${TRUST_MANAGER_NAMESPACE}" get deployment,pod,secret || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,configmap,secret || true
  kubectl get bundle || true
  kubectl get bundle "${TRUST_BUNDLE_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get configmap "${TRUST_BUNDLE_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${TRUST_MANAGER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${TRUST_MANAGER_NAMESPACE}" logs deployment/"${TRUST_MANAGER_DEPLOYMENT}" --tail=200 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=200 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
  -f "${ROOT_DIR}/${TRUST_MANAGER_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
check_prereqs

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for trust-manager runtime proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for trust-manager runtime proof"
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

phase "Installing cert-manager prerequisite for trust-manager"
run_make kind-bootstrap-cert-manager KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"

phase "Installing trust-manager"
helm upgrade --install "${TRUST_MANAGER_RELEASE}" jetstack/trust-manager \
  --namespace "${TRUST_MANAGER_NAMESPACE}" \
  --create-namespace \
  --wait \
  --timeout 5m
kubectl -n "${TRUST_MANAGER_NAMESPACE}" rollout status deployment/"${TRUST_MANAGER_DEPLOYMENT}" --timeout=5m

phase "Installing product chart managed release${profile_label} with trust-manager integration"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

phase "Verifying mirrored trust-manager source Secret"
kubectl -n "${TRUST_MANAGER_NAMESPACE}" wait --for=create "secret/${TRUST_SOURCE_SECRET_NAME}" --timeout=3m >/dev/null
kubectl -n "${TRUST_MANAGER_NAMESPACE}" get secret "${TRUST_SOURCE_SECRET_NAME}" -o "jsonpath={.data.ca\\.crt}" | grep -q .

phase "Verifying trust-manager bundle reconciliation"
kubectl get bundle "${TRUST_BUNDLE_NAME}" >/dev/null
wait_for_configmap_key "${TRUST_BUNDLE_NAME}" "ca.crt"

phase "Verifying managed cluster health"
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_nificluster_condition TargetResolved
wait_for_nificluster_condition Available

phase "Verifying managed restart trigger includes the trust bundle"
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.spec.restartTriggers.configMaps[*].name}' | grep -q "${TRUST_BUNDLE_NAME}"

print_success_footer "platform chart trust-manager runtime proof completed" \
  "kubectl get bundle ${TRUST_BUNDLE_NAME} -o yaml" \
  "kubectl -n ${NAMESPACE} get configmap ${TRUST_BUNDLE_NAME} -o yaml" \
  "make kind-health KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200" \
  "kubectl -n ${TRUST_MANAGER_NAMESPACE} logs deployment/${TRUST_MANAGER_DEPLOYMENT} --tail=200"
