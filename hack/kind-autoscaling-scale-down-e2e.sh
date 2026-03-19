#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-autoscaling-scale-down}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-nifi-fabric-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
VERSION_VALUES_FILE="${VERSION_VALUES_FILE:-examples/nifi-2.8.0-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
FAST_VALUES_FILE="${FAST_VALUES_FILE:-examples/test-fast-values.yaml}"
AUTH_SECRET="${AUTH_SECRET:-nifi-auth}"
AUTOSCALING_CHURN_MODE="${AUTOSCALING_CHURN_MODE:-false}"
START_EPOCH="$(date +%s)"

run_make() {
  make -C "${ROOT_DIR}" "$@"
}

run_scale_down_health() {
  bash "${ROOT_DIR}/hack/check-nifi-health.sh" \
    --namespace "${NAMESPACE}" \
    --statefulset "${HELM_RELEASE}" \
    --auth-secret "${AUTH_SECRET}" \
    --allow-former-nodes
}

elapsed() {
  echo "$(( $(date +%s) - START_EPOCH ))"
}

configure_kind_kubeconfig() {
  local kubeconfig_path="${TMPDIR:-/tmp}/${KIND_CLUSTER_NAME}.kubeconfig"
  kind get kubeconfig --name "${KIND_CLUSTER_NAME}" >"${kubeconfig_path}"
  export KUBECONFIG="${kubeconfig_path}"
}

ensure_fresh_cluster() {
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1
}

cluster_jsonpath() {
  local cluster_name="$1"
  local jsonpath="$2"
  kubectl -n "${NAMESPACE}" get nificluster "${cluster_name}" -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

sts_jsonpath() {
  local jsonpath="$1"
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

wait_for_output() {
  local description="$1"
  local expected="$2"
  local timeout_seconds="$3"
  shift 3
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local actual=""

  while true; do
    actual="$("$@" | tr -d '\n')"
    if [[ "${actual}" == "${expected}" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${description}: expected ${expected}, got ${actual:-<empty>}" >&2
      return 1
    fi
    sleep 5
  done
}

wait_for_contains() {
  local description="$1"
  local needle="$2"
  local timeout_seconds="$3"
  shift 3
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local actual=""

  while true; do
    actual="$("$@" | tr '\n' ' ')"
    if [[ "${actual}" == *"${needle}"* ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${description} to contain ${needle}: got ${actual:-<empty>}" >&2
      return 1
    fi
    sleep 5
  done
}

wait_for_empty() {
  local description="$1"
  local timeout_seconds="$2"
  shift 2
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local actual=""

  while true; do
    actual="$("$@" | tr -d '\n')"
    if [[ -z "${actual}" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for empty ${description}: got ${actual}" >&2
      return 1
    fi
    sleep 5
  done
}

require_output() {
  local description="$1"
  local expected="$2"
  shift 2
  local actual
  actual="$("$@" | tr -d '\n')"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "expected ${description} ${expected}, got ${actual:-<empty>}" >&2
    return 1
  fi
}

wait_for_condition() {
  local cluster_name="$1"
  local condition_type="$2"
  local expected_status="$3"
  local timeout_seconds="${4:-300}"
  wait_for_output "condition ${condition_type}=${expected_status} on ${cluster_name}" "${expected_status}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" "{.status.conditions[?(@.type==\"${condition_type}\")].status}"
}

wait_for_cluster_reason() {
  local cluster_name="$1"
  local reason="$2"
  local timeout_seconds="${3:-300}"
  wait_for_output "autoscaling reason ${reason} on ${cluster_name}" "${reason}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" '{.status.autoscaling.reason}'
}

wait_for_cluster_recommended() {
  local cluster_name="$1"
  local replicas="$2"
  local timeout_seconds="${3:-300}"
  wait_for_output "recommended replicas ${replicas} on ${cluster_name}" "${replicas}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" '{.status.autoscaling.recommendedReplicas}'
}

wait_for_cluster_spec_mode() {
  local cluster_name="$1"
  local mode="$2"
  local timeout_seconds="${3:-300}"
  wait_for_output "autoscaling spec mode ${mode} on ${cluster_name}" "${mode}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" '{.spec.autoscaling.mode}'
}

wait_for_cluster_decision_contains() {
  local cluster_name="$1"
  local needle="$2"
  local timeout_seconds="${3:-300}"
  wait_for_contains "last scaling decision on ${cluster_name}" "${needle}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" '{.status.autoscaling.lastScalingDecision}'
}

wait_for_cluster_last_scale_down_time() {
  local cluster_name="$1"
  local timeout_seconds="${2:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local actual=""

  while true; do
    actual="$(cluster_jsonpath "${cluster_name}" '{.status.autoscaling.lastScaleDownTime}' | tr -d '\n')"
    if [[ -n "${actual}" ]]; then
      printf '%s\n' "${actual}"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for lastScaleDownTime on ${cluster_name}" >&2
      return 1
    fi
    sleep 5
  done
}

wait_for_cluster_execution_phase_empty() {
  local cluster_name="$1"
  local timeout_seconds="${2:-300}"
  wait_for_empty "autoscaling execution phase on ${cluster_name}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" '{.status.autoscaling.execution.phase}'
}

wait_for_last_operation_phase() {
  local cluster_name="$1"
  local phase="$2"
  local timeout_seconds="${3:-300}"
  wait_for_output "lastOperation.phase ${phase} on ${cluster_name}" "${phase}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" '{.status.lastOperation.phase}'
}

wait_for_sts_replicas() {
  local replicas="$1"
  local timeout_seconds="${2:-300}"
  wait_for_output "StatefulSet replicas ${replicas}" "${replicas}" "${timeout_seconds}" \
    sts_jsonpath '{.spec.replicas}'
}

wait_for_event_reason() {
  local reason="$1"
  local timeout_seconds="${2:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local actual=""

  while true; do
    actual="$(kubectl -n "${NAMESPACE}" get events --field-selector "involvedObject.kind=NiFiCluster,involvedObject.name=${HELM_RELEASE},reason=${reason}" -o jsonpath='{.items[*].reason}' 2>/dev/null || true)"
    if [[ -n "${actual}" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for event reason ${reason}" >&2
      return 1
    fi
    sleep 5
  done
}

patch_main_cluster() {
  local patch="$1"
  kubectl -n "${NAMESPACE}" patch nificluster "${HELM_RELEASE}" --type merge -p "${patch}" >/dev/null
}

nudge_main_cluster_reconcile() {
  kubectl -n "${NAMESPACE}" annotate nificluster "${HELM_RELEASE}" autoscaling-scale-down-proof-reconcile="$(date +%s%N)" --overwrite >/dev/null
}

configure_advisory_scale_down() {
  local min_replicas="$1"
  local max_replicas="$2"
  patch_main_cluster "$(cat <<EOF
{"spec":{"autoscaling":{"mode":"Advisory","scaleUp":{"enabled":false,"cooldown":"5m"},"scaleDown":{"enabled":true,"cooldown":"5m","stabilizationWindow":"30s"},"minReplicas":${min_replicas},"maxReplicas":${max_replicas},"signals":["QueuePressure","CPU"]}}}
EOF
)"
  nudge_main_cluster_reconcile
}

configure_enforced_scale_up() {
  local min_replicas="$1"
  local max_replicas="$2"
  patch_main_cluster "$(cat <<EOF
{"spec":{"autoscaling":{"mode":"Enforced","scaleUp":{"enabled":true,"cooldown":"5m"},"scaleDown":{"enabled":false,"cooldown":"5m","stabilizationWindow":"30s"},"minReplicas":${min_replicas},"maxReplicas":${max_replicas},"signals":["QueuePressure","CPU"]}}}
EOF
)"
  nudge_main_cluster_reconcile
}

configure_enforced_scale_down() {
  local min_replicas="$1"
  local max_replicas="$2"
  patch_main_cluster "$(cat <<EOF
{"spec":{"autoscaling":{"mode":"Enforced","scaleUp":{"enabled":false,"cooldown":"5m"},"scaleDown":{"enabled":true,"cooldown":"5m","stabilizationWindow":"30s"},"minReplicas":${min_replicas},"maxReplicas":${max_replicas},"signals":["QueuePressure","CPU"]}}}
EOF
)"
  nudge_main_cluster_reconcile
}

configure_disabled_autoscaling() {
  patch_main_cluster '{"spec":{"autoscaling":{"mode":"Disabled","scaleUp":{"enabled":false,"cooldown":"5m"},"scaleDown":{"enabled":false,"cooldown":"5m","stabilizationWindow":"30s"},"minReplicas":0,"maxReplicas":0,"signals":[]}}}'
  nudge_main_cluster_reconcile
}

print_autoscaling_summary() {
  local cluster_name="$1"
  kubectl -n "${NAMESPACE}" get nificluster "${cluster_name}" -o jsonpath='{.metadata.name}{" recommended="}{.status.autoscaling.recommendedReplicas}{" reason="}{.status.autoscaling.reason}{" decision="}{.status.autoscaling.lastScalingDecision}{" executionPhase="}{.status.autoscaling.execution.phase}{" executionState="}{.status.autoscaling.execution.state}{" executionBlockedReason="}{.status.autoscaling.execution.blockedReason}{" executionFailureReason="}{.status.autoscaling.execution.failureReason}{" executionTarget="}{.status.autoscaling.execution.targetReplicas}{" executionStartedAt="}{.status.autoscaling.execution.startedAt}{" lowPressureSince="}{.status.autoscaling.lowPressureSince}{" lowPressureSamples="}{.status.autoscaling.lowPressure.consecutiveSamples}{"/"}{.status.autoscaling.lowPressure.requiredConsecutiveSamples}{" lastScaleUpTime="}{.status.autoscaling.lastScaleUpTime}{" lastScaleDownTime="}{.status.autoscaling.lastScaleDownTime}{" desired="}{.status.replicas.desired}{" ready="}{.status.replicas.ready}{"\n"}' 2>/dev/null || true
}

wait_for_pod_presence() {
  local pod_name="$1"
  local should_exist="$2"
  local timeout_seconds="${3:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    if kubectl -n "${NAMESPACE}" get pod "${pod_name}" >/dev/null 2>&1; then
      if [[ "${should_exist}" == "true" ]]; then
        return 0
      fi
    else
      if [[ "${should_exist}" == "false" ]]; then
        return 0
      fi
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for pod ${pod_name} presence=${should_exist}" >&2
      return 1
    fi
    sleep 5
  done
}

pod_uid() {
  local pod_name="$1"
  kubectl -n "${NAMESPACE}" get pod "${pod_name}" -o jsonpath='{.metadata.uid}' 2>/dev/null || true
}

pvc_uid() {
  local pvc_name="$1"
  kubectl -n "${NAMESPACE}" get pvc "${pvc_name}" -o jsonpath='{.metadata.uid}' 2>/dev/null || true
}

assert_pvc_uid_stable() {
  local pvc_name="$1"
  local expected_uid="$2"
  local actual_uid
  actual_uid="$(pvc_uid "${pvc_name}" | tr -d '\n')"
  if [[ -z "${actual_uid}" || "${actual_uid}" != "${expected_uid}" ]]; then
    echo "expected PVC ${pvc_name} uid ${expected_uid}, got ${actual_uid:-<missing>}" >&2
    return 1
  fi
}

assert_pods_exactly() {
  local expected="$1"
  local actual
  actual="$(
    kubectl -n "${NAMESPACE}" get pod -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null |
      awk -v prefix="${HELM_RELEASE}-" 'index($0, prefix) == 1 { print }' |
      sort |
      xargs
  )"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "expected pods [${expected}], got [${actual:-<empty>}]" >&2
    return 1
  fi
}

install_main_release() {
  phase "Installing managed NiFi ${NIFI_IMAGE##*:} release${profile_label}"
  helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi" \
    --namespace "${NAMESPACE}" \
    --create-namespace \
    "${helm_values_args[@]}"

  phase "Applying NiFiCluster"
  kubectl apply -f "${ROOT_DIR}/examples/managed/nificluster.yaml" >/dev/null
}

dump_diagnostics() {
  set +e
  echo
  echo "==> autoscaling scale-down diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  echo
  echo "Autoscaling summary:"
  print_autoscaling_summary "${HELM_RELEASE}"
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get pod -o wide || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 200 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 200 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=400 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

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
  phase "Reusing existing kind cluster for autoscaling scale-down proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for autoscaling scale-down proof"
  ensure_fresh_cluster
  configure_kind_kubeconfig

  phase "Loading NiFi image into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Creating TLS and auth Secrets"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Installing CRD"
  run_make install-crd

  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"

  phase "Deploying controller"
  run_make deploy-controller
  kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
fi

install_main_release

phase "Verifying baseline fast-path health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition "${HELM_RELEASE}" TargetResolved True 300
wait_for_condition "${HELM_RELEASE}" Available True 300

phase "Preparing a bounded 3-node starting point through enforced scale-up"
configure_enforced_scale_up 3 3
wait_for_sts_replicas 3 600
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition "${HELM_RELEASE}" Available True 600

if [[ "${AUTOSCALING_CHURN_MODE}" == "true" ]]; then
  phase "Capturing ordinal-2 pod and PVC identities before churn"
  wait_for_pod_presence "${HELM_RELEASE}-2" true 300
  ordinal_two_uid_before_scale_down="$(pod_uid "${HELM_RELEASE}-2" | tr -d '\n')"
  declare -A ordinal_two_pvc_uids=()
  for repository in database-repository flowfile-repository content-repository provenance-repository; do
    pvc_name="${repository}-${HELM_RELEASE}-2"
    ordinal_two_pvc_uids["${pvc_name}"]="$(pvc_uid "${pvc_name}" | tr -d '\n')"
    if [[ -z "${ordinal_two_pvc_uids["${pvc_name}"]}" ]]; then
      echo "expected PVC ${pvc_name} to exist before churn" >&2
      exit 1
    fi
  done

  phase "Proving one-step scale-down removes only the highest ordinal during churn"
  configure_enforced_scale_down 2 3
  wait_for_sts_replicas 2 900
  run_scale_down_health
  wait_for_condition "${HELM_RELEASE}" Available True 600
  wait_for_cluster_execution_phase_empty "${HELM_RELEASE}" 600
  wait_for_pod_presence "${HELM_RELEASE}-2" false 300
  assert_pods_exactly "${HELM_RELEASE}-0 ${HELM_RELEASE}-1"
  for pvc_name in "${!ordinal_two_pvc_uids[@]}"; do
    assert_pvc_uid_stable "${pvc_name}" "${ordinal_two_pvc_uids["${pvc_name}"]}"
  done

  phase "Proving scale-up reuses the same ordinal and PVC set on the next cycle"
  configure_enforced_scale_up 3 3
  wait_for_sts_replicas 3 900
  run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
  wait_for_condition "${HELM_RELEASE}" Available True 600
  wait_for_cluster_execution_phase_empty "${HELM_RELEASE}" 600
  wait_for_pod_presence "${HELM_RELEASE}-2" true 300
  assert_pods_exactly "${HELM_RELEASE}-0 ${HELM_RELEASE}-1 ${HELM_RELEASE}-2"
  ordinal_two_uid_after_scale_up="$(pod_uid "${HELM_RELEASE}-2" | tr -d '\n')"
  if [[ -z "${ordinal_two_uid_after_scale_up}" || "${ordinal_two_uid_after_scale_up}" == "${ordinal_two_uid_before_scale_down}" ]]; then
    echo "expected ordinal 2 pod to be recreated after churn, got uid ${ordinal_two_uid_after_scale_up:-<missing>}" >&2
    exit 1
  fi
  for pvc_name in "${!ordinal_two_pvc_uids[@]}"; do
    assert_pvc_uid_stable "${pvc_name}" "${ordinal_two_pvc_uids["${pvc_name}"]}"
  done

  phase "Restoring disabled autoscaling baseline"
  configure_disabled_autoscaling
  nudge_main_cluster_reconcile
  wait_for_cluster_spec_mode "${HELM_RELEASE}" "Disabled" 60
  wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonDisabled:-Disabled}" 30

  print_success_footer "autoscaling churn runtime proof completed" \
    "make kind-autoscaling-churn-fast-e2e-reuse" \
    "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
    "kubectl -n ${NAMESPACE} get pvc" \
    "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=300"
  exit 0
fi

phase "Proving advisory mode remains status-only for low-pressure scale-down recommendations"
configure_advisory_scale_down 1 3
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonLowPressure:-LowPressureDetected}" 300
wait_for_cluster_recommended "${HELM_RELEASE}" 2 300
wait_for_cluster_decision_contains "${HELM_RELEASE}" "autoscaling is not in enforced mode" 180
require_output "StatefulSet replicas" "3" sts_jsonpath '{.spec.replicas}'

phase "Proving enforced mode scales down by one safe step after sustained low pressure"
configure_enforced_scale_down 1 3
wait_for_event_reason AutoscalingScaleDownStarted 300
wait_for_sts_replicas 2 900
scale_down_time="$(wait_for_cluster_last_scale_down_time "${HELM_RELEASE}" 300)"
run_scale_down_health
wait_for_condition "${HELM_RELEASE}" Available True 600
wait_for_last_operation_phase "${HELM_RELEASE}" Succeeded 600
wait_for_cluster_execution_phase_empty "${HELM_RELEASE}" 600
wait_for_event_reason AutoscalingScaleDownCompleted 60 || true

phase "Proving cooldown blocks an immediate second scale-down"
wait_for_cluster_decision_contains "${HELM_RELEASE}" "cooldown is active until" 300
sleep 20
require_output "StatefulSet replicas" "2" sts_jsonpath '{.spec.replicas}'
require_output "lastScaleDownTime stability" "${scale_down_time}" cluster_jsonpath "${HELM_RELEASE}" '{.status.autoscaling.lastScaleDownTime}'

phase "Restoring disabled autoscaling baseline"
configure_disabled_autoscaling
nudge_main_cluster_reconcile
wait_for_cluster_spec_mode "${HELM_RELEASE}" "Disabled" 60
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonDisabled:-Disabled}" 30

print_success_footer "autoscaling scale-down runtime proof completed" \
  "make kind-autoscaling-scale-down-fast-e2e-reuse" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get events --sort-by=.lastTimestamp | tail -n 100" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=300"
