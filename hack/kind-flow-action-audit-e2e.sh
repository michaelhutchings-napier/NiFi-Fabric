#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-flow-action-audit}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-mirror.gcr.io/apache/nifi:2.8.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_AUDIT_LOCAL_ONLY_VALUES_FILE="${PLATFORM_AUDIT_LOCAL_ONLY_VALUES_FILE:-examples/platform-managed-audit-flow-actions-local-only-values.yaml}"
PLATFORM_AUDIT_VALUES_FILE="${PLATFORM_AUDIT_VALUES_FILE:-examples/platform-managed-audit-flow-actions-values.yaml}"
PLATFORM_AUDIT_KIND_VALUES_FILE="${PLATFORM_AUDIT_KIND_VALUES_FILE:-examples/platform-managed-audit-flow-actions-kind-values.yaml}"
REPORTER_IMAGE_REPOSITORY="${REPORTER_IMAGE_REPOSITORY:-nifi-flow-action-audit-reporter}"
REPORTER_IMAGE_TAG="${REPORTER_IMAGE_TAG:-0.0.1-SNAPSHOT}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
START_EPOCH="$(date +%s)"

NIFI_IMAGE_REPOSITORY="${NIFI_IMAGE%:*}"
NIFI_IMAGE_TAG="${NIFI_IMAGE##*:}"

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

require_condition_true() {
  local type="$1"
  local actual
  actual="$(kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath="{.status.conditions[?(@.type==\"${type}\")].status}")"
  if [[ "${actual}" != "True" ]]; then
    echo "expected condition ${type}=True, got ${actual:-<empty>}" >&2
    return 1
  fi
}

wait_for_condition_true() {
  local type="$1"
  local timeout_seconds="${2:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    if require_condition_true "${type}" >/dev/null 2>&1; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      require_condition_true "${type}"
      return 1
    fi
    sleep 5
  done
}

recycle_single_node_if_revision_pending() {
  local replicas current_revision update_revision

  replicas="$(kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}')"
  if [[ "${replicas}" != "1" ]]; then
    return 0
  fi

  current_revision="$(kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.status.currentRevision}')"
  update_revision="$(kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.status.updateRevision}')"
  if [[ -z "${update_revision}" || "${current_revision}" == "${update_revision}" ]]; then
    return 0
  fi

  phase "Recycling single-node NiFi pod to adopt upgraded audit config"
  kubectl -n "${NAMESPACE}" delete pod "${HELM_RELEASE}-0" --wait=false
  kubectl -n "${NAMESPACE}" wait --for=delete "pod/${HELM_RELEASE}-0" --timeout=5m
  kubectl -n "${NAMESPACE}" wait --for=condition=Ready "pod/${HELM_RELEASE}-0" --timeout=10m
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

capture_cmd() {
  local name="$1"
  shift

  if [[ -z "${ARTIFACT_DIR}" ]]; then
    return 0
  fi

  mkdir -p "${ARTIFACT_DIR}"
  {
    echo "### ${name}"
    "$@"
  } >"${ARTIFACT_DIR}/${name}.log" 2>&1 || true
}

dump_diagnostics() {
  set +e
  echo
  echo "==> flow-action audit diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  capture_cmd current-context kubectl config current-context
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  capture_cmd helm-status helm -n "${NAMESPACE}" status "${HELM_RELEASE}"
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  capture_cmd controller-get kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,configmap,secret || true
  capture_cmd workload-get kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,configmap,secret
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  capture_cmd nificluster-yaml kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml
  kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}" || true
  capture_cmd nificluster-describe kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}"
  kubectl -n "${NAMESPACE}" describe pod "${HELM_RELEASE}-0" || true
  capture_cmd nifi-pod-describe kubectl -n "${NAMESPACE}" describe pod "${HELM_RELEASE}-0"
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 120 || true
  capture_cmd events sh -ec "kubectl -n '${NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 120"
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  capture_cmd controller-logs kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --tail=300 || true
  capture_cmd nifi-logs kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --tail=300
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --previous --tail=300 || true
  capture_cmd nifi-logs-previous kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --previous --tail=300
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

common_helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  common_helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

common_helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_AUDIT_KIND_VALUES_FILE}")

local_only_helm_values_args=(
  "${common_helm_values_args[@]}"
  -f "${ROOT_DIR}/${PLATFORM_AUDIT_LOCAL_ONLY_VALUES_FILE}"
)

log_export_helm_values_args=(
  "${common_helm_values_args[@]}"
  -f "${ROOT_DIR}/${PLATFORM_AUDIT_VALUES_FILE}"
)

phase "Checking prerequisites"
check_prereqs
require_command python3

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for flow-action audit proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for flow-action audit proof"
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

phase "Building platform chart dependency"
helm dependency build "${ROOT_DIR}/charts/nifi-platform" >/dev/null

phase "Installing product chart managed release${profile_label} with local-only flow-action audit"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${local_only_helm_values_args[@]}" \
  --set "nifi.image.repository=${NIFI_IMAGE_REPOSITORY}" \
  --set "nifi.image.tag=${NIFI_IMAGE_TAG}"

phase "Verifying local-only platform resources and controller rollout"
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

phase "Verifying local-only cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available

phase "Verifying local-only audit configuration"
bash "${ROOT_DIR}/hack/verify-flow-action-audit-local-layer.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}"

phase "Proving local-only fallback audit evidence"
retry_proof "local-only flow-action audit" \
  bash "${ROOT_DIR}/hack/prove-flow-action-audit.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --expect-log-export false

phase "Building flow-action audit reporter image"
IMAGE_TAG="${REPORTER_IMAGE_REPOSITORY}:${REPORTER_IMAGE_TAG}" bash "${ROOT_DIR}/hack/build-flow-action-audit-reporter-image.sh"

phase "Loading flow-action audit reporter image into kind"
kind load docker-image --name "${KIND_CLUSTER_NAME}" "${REPORTER_IMAGE_REPOSITORY}:${REPORTER_IMAGE_TAG}"

phase "Upgrading product chart managed release${profile_label} to flow-action audit log export"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${log_export_helm_values_args[@]}" \
  --set "nifi.image.repository=${NIFI_IMAGE_REPOSITORY}" \
  --set "nifi.image.tag=${NIFI_IMAGE_TAG}" \
  --set "nifi.observability.audit.flowActions.export.log.installation.image.repository=${REPORTER_IMAGE_REPOSITORY}" \
  --set "nifi.observability.audit.flowActions.export.log.installation.image.tag=${REPORTER_IMAGE_TAG}"

phase "Verifying upgraded platform resources and controller rollout"
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

recycle_single_node_if_revision_pending

phase "Verifying upgraded cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available

phase "Proving flow-action audit log export"
retry_proof "flow-action audit log export" \
  bash "${ROOT_DIR}/hack/prove-flow-action-audit.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth

print_success_footer "flow-action audit proof completed" \
  "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
  "kubectl -n ${NAMESPACE} logs ${HELM_RELEASE}-0 -c nifi --tail=200" \
  "kubectl -n ${NAMESPACE} exec -i ${HELM_RELEASE}-0 -c nifi -- find /opt/nifi/nifi-current/database_repository/flow-audit-archive -maxdepth 1 -type f | head" \
  "make kind-flow-action-audit-fast-e2e-reuse"
