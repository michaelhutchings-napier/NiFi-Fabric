#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-parameter-contexts}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PARAMETER_CONTEXTS_VALUES_FILE="${PARAMETER_CONTEXTS_VALUES_FILE:-examples/platform-managed-parameter-contexts-values.yaml}"
PARAMETER_CONTEXTS_KIND_VALUES_FILE="${PARAMETER_CONTEXTS_KIND_VALUES_FILE:-examples/platform-managed-parameter-contexts-kind-values.yaml}"
PARAMETER_CONTEXTS_UPDATE_VALUES_FILE="${PARAMETER_CONTEXTS_UPDATE_VALUES_FILE:-examples/platform-managed-parameter-contexts-update-kind-values.yaml}"
PARAMETER_CONTEXTS_DELETE_VALUES_FILE="${PARAMETER_CONTEXTS_DELETE_VALUES_FILE:-examples/platform-managed-parameter-contexts-delete-kind-values.yaml}"
PARAMETER_CONTEXTS_SECRET_NAME="${PARAMETER_CONTEXTS_SECRET_NAME:-platform-parameter-context}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"

run_make() {
  make -C "${ROOT_DIR}" "$@"
}

retry_proof() {
  local description="$1"
  shift

  local deadline=$(( $(date +%s) + 240 ))
  while true; do
    if "$@"; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${description}" >&2
      return 1
    fi
    sleep 5
  done
}

configure_kind_kubeconfig() {
  local kubeconfig_path="${TMPDIR:-/tmp}/${KIND_CLUSTER_NAME}.kubeconfig"
  kind get kubeconfig --name "${KIND_CLUSTER_NAME}" >"${kubeconfig_path}"
  export KUBECONFIG="${kubeconfig_path}"
}

dump_diagnostics() {
  set +e
  echo
  echo "==> parameter context runtime diagnostics after failure"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,secret,configmap || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 120 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --tail=300 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
)

if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
fi

helm_values_args+=(
  -f "${ROOT_DIR}/${PARAMETER_CONTEXTS_VALUES_FILE}"
  -f "${ROOT_DIR}/${PARAMETER_CONTEXTS_KIND_VALUES_FILE}"
)

phase "Checking prerequisites"
check_prereqs
require_command python3

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for runtime-managed parameter context proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for runtime-managed parameter context proof"
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

phase "Creating sensitive parameter Secret"
kubectl -n "${NAMESPACE}" create secret generic "${PARAMETER_CONTEXTS_SECRET_NAME}" \
  --from-literal=external-api-token=initial-sensitive-token \
  --dry-run=client -o yaml | kubectl apply -f -

phase "Installing product chart managed release"
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

phase "Verifying initial cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

phase "Proving runtime-managed Parameter Context creation"
initial_expected_action="created"
if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  initial_expected_action="created,unchanged,updated"
fi
retry_proof "runtime-managed Parameter Context creation" \
  bash "${ROOT_DIR}/hack/prove-parameter-contexts-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --context-name platform-runtime \
    --expected-inline-parameter external.api.baseUrl \
    --expected-inline-value https://api.internal.example.com \
    --expected-sensitive-parameter external.api.token \
    --expected-root-process-group-name platform-target \
    --expected-action "${initial_expected_action}"

phase "Updating declared Parameter Context values"
previous_pod_uid="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.metadata.uid}')"
kubectl -n "${NAMESPACE}" create secret generic "${PARAMETER_CONTEXTS_SECRET_NAME}" \
  --from-literal=external-api-token=rotated-sensitive-token \
  --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}" \
  -f "${ROOT_DIR}/${PARAMETER_CONTEXTS_UPDATE_VALUES_FILE}"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

current_pod_uid="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.metadata.uid}')"
if [[ "${current_pod_uid}" != "${previous_pod_uid}" ]]; then
  echo "expected live Parameter Context update to reconcile without replacing pod ${HELM_RELEASE}-0" >&2
  exit 1
fi

phase "Proving runtime-managed Parameter Context update and drift reconciliation"
retry_proof "runtime-managed Parameter Context live update" \
  bash "${ROOT_DIR}/hack/prove-parameter-contexts-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --context-name platform-runtime \
    --expected-inline-parameter external.api.baseUrl \
    --expected-inline-value https://api-v2.internal.example.com \
    --expected-sensitive-parameter external.api.token \
    --expected-root-process-group-name platform-target \
    --expected-action updated,created,unchanged

phase "Replacing the declared owned Parameter Context to prove delete reconciliation"
previous_pod_uid="${current_pod_uid}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}" \
  -f "${ROOT_DIR}/${PARAMETER_CONTEXTS_DELETE_VALUES_FILE}"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

current_pod_uid="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.metadata.uid}')"
if [[ "${current_pod_uid}" != "${previous_pod_uid}" ]]; then
  echo "expected live Parameter Context delete and replacement reconcile to keep pod ${HELM_RELEASE}-0 in place" >&2
  exit 1
fi

phase "Proving runtime-managed Parameter Context deletion and bounded attachment reassignment"
retry_proof "runtime-managed Parameter Context delete and replacement" \
  bash "${ROOT_DIR}/hack/prove-parameter-contexts-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --context-name platform-runtime-v2 \
    --expected-inline-parameter external.api.baseUrl \
    --expected-inline-value https://api-v3.internal.example.com \
    --expected-sensitive-parameter external.api.token \
    --expected-root-process-group-name platform-target \
    --expected-deleted-context-name platform-runtime \
    --expected-action created,updated,unchanged

print_success_footer "platform chart runtime-managed Parameter Context proof completed" \
  "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
  "kubectl -n ${NAMESPACE} exec -i ${HELM_RELEASE}-0 -c nifi -- cat /opt/nifi/nifi-current/logs/parameter-contexts-bootstrap-status.json" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get pods,configmap,secret"
