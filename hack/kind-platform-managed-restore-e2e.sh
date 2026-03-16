#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-platform-restore}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
RESTORE_VALUES_FILE="${RESTORE_VALUES_FILE:-examples/platform-managed-restore-kind-values.yaml}"
FLOW_REGISTRY_SECRET_NAME="${FLOW_REGISTRY_SECRET_NAME:-github-flow-registry}"
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

dump_diagnostics() {
  set +e
  echo
  echo "==> platform restore diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl get crd nificlusters.platform.nifi.io -o yaml || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,pvc,secret,configmap || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 120 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 120 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs deployment/github-mock --tail=300 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

helm_values_args+=(-f "${ROOT_DIR}/${RESTORE_VALUES_FILE}")

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

phase "Checking prerequisites"
check_prereqs
require_command curl
require_command jq
require_command python3

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for bounded platform restore proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for bounded platform restore proof"
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

phase "Creating GitHub Flow Registry token Secret"
kubectl -n "${NAMESPACE}" create secret generic "${FLOW_REGISTRY_SECRET_NAME}" \
  --from-literal=token=dummytoken \
  --dry-run=client -o yaml | kubectl apply -f -

phase "Deploying GitHub-compatible evaluator service"
NAMESPACE="${NAMESPACE}" NIFI_IMAGE="${NIFI_IMAGE}" bash "${ROOT_DIR}/hack/deploy-github-flow-registry-mock.sh"

phase "Installing product chart managed release${profile_label}"
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
wait_for_condition_true TargetResolved
wait_for_condition_true Available

phase "Building functional flow configuration from bounded supported features"
bash "${ROOT_DIR}/hack/prove-bounded-flow-config-restore.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --import-name payments-catalog-selection

phase "Exporting control-plane backup bundle"
backup_dir="${tmpdir}/control-plane-backup"
bash "${ROOT_DIR}/hack/export-control-plane-backup.sh" \
  --release "${HELM_RELEASE}" \
  --namespace "${NAMESPACE}" \
  --output-dir "${backup_dir}"

phase "Simulating config-only loss by uninstalling the release and deleting NiFi PVCs"
helm -n "${NAMESPACE}" uninstall "${HELM_RELEASE}"
kubectl -n "${NAMESPACE}" wait --for=delete pod -l app.kubernetes.io/instance="${HELM_RELEASE}",app.kubernetes.io/name=nifi --timeout=5m || true
kubectl -n "${NAMESPACE}" delete pvc -l app.kubernetes.io/instance="${HELM_RELEASE}",app.kubernetes.io/name=nifi --ignore-not-found --wait=true

phase "Recovering the control plane from the exported backup bundle"
bash "${ROOT_DIR}/hack/recover-control-plane-backup.sh" \
  --backup-dir "${backup_dir}"

phase "Verifying recovered platform resources and controller rollout"
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

phase "Verifying recovered cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available

phase "Rebuilding functional flow configuration from the restored bounded features"
bash "${ROOT_DIR}/hack/prove-bounded-flow-config-restore.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --import-name payments-catalog-selection

print_success_footer "platform chart bounded restore workflow proof completed" \
  "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
  "bash hack/export-control-plane-backup.sh --release ${HELM_RELEASE} --namespace ${NAMESPACE} --output-dir ./backup/nifi-control-plane" \
  "bash hack/recover-control-plane-backup.sh --backup-dir ./backup/nifi-control-plane" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get pods,pvc,configmap"
