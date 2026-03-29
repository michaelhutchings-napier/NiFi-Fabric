#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-platform-configmap-properties}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
CONFIGMAP_VALUES_FILE="${CONFIGMAP_VALUES_FILE:-examples/platform-managed-configmap-properties-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"
BASE_CONFIGMAP_NAME="${BASE_CONFIGMAP_NAME:-nifi-property-overrides-base}"
TEAM_CONFIGMAP_NAME="${TEAM_CONFIGMAP_NAME:-nifi-property-overrides-team}"
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

wait_for_command() {
  local description="$1"
  local timeout_seconds="$2"
  shift 2
  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    if "$@" >/dev/null 2>&1; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${description}" >&2
      "$@" || true
      return 1
    fi
    sleep 5
  done
}

pod_uid_snapshot() {
  kubectl -n "${NAMESPACE}" get pods \
    -l "app.kubernetes.io/instance=${HELM_RELEASE},app.kubernetes.io/name=nifi" \
    -o jsonpath='{range .items[*]}{.metadata.name}{"="}{.metadata.uid}{"\n"}{end}' | sort
}

all_pods_changed() {
  local before_file="$1"
  local after_file="$2"
  python3 - "$before_file" "$after_file" <<'PY'
import sys

before_path, after_path = sys.argv[1], sys.argv[2]

def load(path):
    data = {}
    with open(path, encoding="utf-8") as handle:
        for raw in handle:
            line = raw.strip()
            if not line:
                continue
            name, uid = line.split("=", 1)
            data[name] = uid
    return data

before = load(before_path)
after = load(after_path)

if before.keys() != after.keys():
    raise SystemExit(1)

for name, uid in before.items():
    if after[name] == uid:
      raise SystemExit(1)
PY
}

wait_for_all_pods_changed() {
  local before_file="$1"
  local timeout_seconds="${2:-900}"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local current_file
  current_file="$(mktemp)"

  while true; do
    pod_uid_snapshot >"${current_file}"
    if all_pods_changed "${before_file}" "${current_file}" >/dev/null 2>&1; then
      rm -f "${current_file}"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for all managed pods to restart" >&2
      cat "${before_file}" >&2
      echo "---" >&2
      cat "${current_file}" >&2
      rm -f "${current_file}"
      return 1
    fi
    sleep 5
  done
}

write_property_configmap() {
  local name="$1"
  local key="$2"
  local contents="$3"

  kubectl -n "${NAMESPACE}" create configmap "${name}" \
    --from-literal="${key}=${contents}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

seed_property_configmaps() {
  kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  write_property_configmap "${BASE_CONFIGMAP_NAME}" "common.properties" $'nifi.cluster.node.protocol.max.threads=61\nnifi.fabric.test.configmap.marker=base'
  write_property_configmap "${TEAM_CONFIGMAP_NAME}" "team.properties" $'nifi.cluster.node.protocol.max.threads=62\nnifi.fabric.test.configmap.marker=team-initial'
}

verify_property_on_all_pods() {
  local expected_threads="$1"
  local expected_marker="$2"
  local replicas
  replicas="$(kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}')"

  for ordinal in $(seq 0 $((replicas - 1))); do
    local pod="${HELM_RELEASE}-${ordinal}"
    kubectl -n "${NAMESPACE}" exec "${pod}" -c nifi -- \
      grep -q "^nifi.cluster.node.protocol.max.threads=${expected_threads}$" /opt/nifi/nifi-current/conf/nifi.properties
    kubectl -n "${NAMESPACE}" exec "${pod}" -c nifi -- \
      grep -q "^nifi.fabric.test.configmap.marker=${expected_marker}$" /opt/nifi/nifi-current/conf/nifi.properties
  done
}

dump_diagnostics() {
  set +e
  echo
  echo "==> configmap-properties diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,configmap,secret || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid || true
  kubectl -n "${NAMESPACE}" get configmap "${BASE_CONFIGMAP_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get configmap "${TEAM_CONFIGMAP_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- grep '^nifi.cluster.node.protocol.max.threads=' /opt/nifi/nifi-current/conf/nifi.properties || true
  kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- grep '^nifi.fabric.test.configmap.marker=' /opt/nifi/nifi-current/conf/nifi.properties || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
  -f "${ROOT_DIR}/${CONFIGMAP_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
check_prereqs

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for ConfigMap property override runtime proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for ConfigMap property override runtime proof"
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

phase "Creating external property ConfigMaps"
seed_property_configmaps

phase "Installing product chart managed release${profile_label} with external property ConfigMaps"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

phase "Verifying managed cluster health"
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available

phase "Verifying managed restart triggers include the external property ConfigMaps"
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.spec.restartTriggers.configMaps[*].name}' | grep -q "${BASE_CONFIGMAP_NAME}"
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.spec.restartTriggers.configMaps[*].name}' | grep -q "${TEAM_CONFIGMAP_NAME}"

phase "Verifying ordered ConfigMap property overrides are applied at startup"
verify_property_on_all_pods "62" "team-initial"

phase "Exercising managed rollout on watched external property ConfigMap drift"
config_hash_before="$(kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.observedConfigHash}')"
uids_before_file="$(mktemp)"
pod_uid_snapshot >"${uids_before_file}"
write_property_configmap "${TEAM_CONFIGMAP_NAME}" "team.properties" $'nifi.cluster.node.protocol.max.threads=63\nnifi.fabric.test.configmap.marker=team-updated'
wait_for_command "managed config hash to advance" 900 bash -ec '
  current="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.observedConfigHash}")"
  [[ -n "${current}" && "${current}" != "'"${config_hash_before}"'" ]]
'
wait_for_all_pods_changed "${uids_before_file}" 900
rm -f "${uids_before_file}"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true Available
verify_property_on_all_pods "63" "team-updated"

print_success_footer "platform chart ConfigMap property override runtime proof completed" \
  "kubectl -n ${NAMESPACE} get configmap ${BASE_CONFIGMAP_NAME} -o yaml" \
  "kubectl -n ${NAMESPACE} get configmap ${TEAM_CONFIGMAP_NAME} -o yaml" \
  "kubectl -n ${NAMESPACE} exec ${HELM_RELEASE}-0 -c nifi -- grep '^nifi.cluster.node.protocol.max.threads=' /opt/nifi/nifi-current/conf/nifi.properties" \
  "make kind-health KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
