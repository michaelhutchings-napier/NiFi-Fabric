#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-nifi-2-8}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-nifi-fabric-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
VERSION_VALUES_FILE="${VERSION_VALUES_FILE:-examples/nifi-2.8.0-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"
FAST_VALUES_FILE="${FAST_VALUES_FILE:-examples/test-fast-values.yaml}"

run_make() {
  make -C "${ROOT_DIR}" "$@"
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

pod_uid_snapshot() {
  kubectl -n "${NAMESPACE}" get pods \
    -l app.kubernetes.io/instance="${HELM_RELEASE}" \
    -o jsonpath='{range .items[*]}{.metadata.name}{"="}{.metadata.uid}{"\n"}{end}' | sort
}

assert_pods_changed() {
  local before="$1"
  local after
  after="$(pod_uid_snapshot)"
  while IFS='=' read -r pod uid_before; do
    [[ -z "${pod}" ]] && continue
    local uid_after
    uid_after="$(printf '%s\n' "${after}" | awk -F= -v pod="${pod}" '$1 == pod { print $2 }')"
    if [[ -z "${uid_after}" ]]; then
      echo "missing pod ${pod} after config drift rollout" >&2
      return 1
    fi
    if [[ "${uid_before}" == "${uid_after}" ]]; then
      echo "pod ${pod} was not recreated during config drift rollout" >&2
      return 1
    fi
  done <<< "${before}"
}

wait_for_pods_changed() {
  local before="$1"
  local deadline=$(( $(date +%s) + 900 ))
  while true; do
    if assert_pods_changed "${before}" >/dev/null 2>&1; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for managed config drift rollout to recreate all pods" >&2
      return 1
    fi
    sleep 10
  done
}

ensure_fresh_cluster() {
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1
}

trap 'print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

helm_values_args=(
  -f "${ROOT_DIR}/examples/managed/values.yaml"
  -f "${ROOT_DIR}/${VERSION_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
check_prereqs

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for NiFi ${NIFI_IMAGE##*:}"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for NiFi ${NIFI_IMAGE##*:}"
  ensure_fresh_cluster
  configure_kind_kubeconfig
fi

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing NiFi image and Secrets"
else
  phase "Loading NiFi image into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Creating TLS and auth Secrets"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}" NIFI_IMAGE="${NIFI_IMAGE}"
fi

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing CRD and controller"
else
  phase "Installing CRD"
  run_make install-crd

  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"

  phase "Deploying controller"
  run_make deploy-controller
  kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
fi

phase "Installing managed NiFi ${NIFI_IMAGE##*:} release${profile_label}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

phase "Applying NiFiCluster"
kubectl apply -f "${ROOT_DIR}/examples/managed/nificluster.yaml"

phase "Verifying cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available

phase "Triggering managed config drift rollout"
before_rollout="$(pod_uid_snapshot)"
run_make kind-config-drift NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_pods_changed "${before_rollout}"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available

print_success_footer "NiFi ${NIFI_IMAGE##*:} managed compatibility proof completed" \
  "make kind-health KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get pods -o custom-columns=NAME:.metadata.name,UID:.metadata.uid,REV:.metadata.labels.controller-revision-hash" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
