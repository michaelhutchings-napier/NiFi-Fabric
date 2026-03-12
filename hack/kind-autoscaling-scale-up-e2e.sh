#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-autoscaling-scale-up}"
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
TLS_SECRET="${TLS_SECRET:-nifi-tls}"
CONFIGMAP_NAME="${CONFIGMAP_NAME:-nifi-config}"
UNRESOLVED_CLUSTER_NAME="${UNRESOLVED_CLUSTER_NAME:-autoscaling-target-missing}"
UNMANAGED_CLUSTER_NAME="${UNMANAGED_CLUSTER_NAME:-autoscaling-unmanaged}"
UNMANAGED_TARGET_NAME="${UNMANAGED_TARGET_NAME:-autoscaling-unmanaged}"
START_EPOCH="$(date +%s)"

ORIGINAL_TLS_SECRET_FILE=""

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

require_contains() {
  local description="$1"
  local needle="$2"
  shift 2
  local actual
  actual="$("$@" | tr '\n' ' ')"
  if [[ "${actual}" != *"${needle}"* ]]; then
    echo "expected ${description} to contain ${needle}, got ${actual:-<empty>}" >&2
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

wait_for_cluster_recommended_empty() {
  local cluster_name="$1"
  local timeout_seconds="${2:-300}"
  wait_for_empty "recommended replicas on ${cluster_name}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" '{.status.autoscaling.recommendedReplicas}'
}

wait_for_cluster_decision_contains() {
  local cluster_name="$1"
  local needle="$2"
  local timeout_seconds="${3:-300}"
  wait_for_contains "last scaling decision on ${cluster_name}" "${needle}" "${timeout_seconds}" \
    cluster_jsonpath "${cluster_name}" '{.status.autoscaling.lastScalingDecision}'
}

wait_for_cluster_last_scale_up_time() {
  local cluster_name="$1"
  local timeout_seconds="${2:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local actual=""

  while true; do
    actual="$(cluster_jsonpath "${cluster_name}" '{.status.autoscaling.lastScaleUpTime}' | tr -d '\n')"
    if [[ -n "${actual}" ]]; then
      printf '%s\n' "${actual}"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for lastScaleUpTime on ${cluster_name}" >&2
      return 1
    fi
    sleep 5
  done
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

require_main_replicas() {
  local expected="$1"
  require_output "StatefulSet replicas" "${expected}" sts_jsonpath '{.spec.replicas}'
}

print_autoscaling_summary() {
  local cluster_name="$1"
  kubectl -n "${NAMESPACE}" get nificluster "${cluster_name}" -o jsonpath='{.metadata.name}{" recommended="}{.status.autoscaling.recommendedReplicas}{" reason="}{.status.autoscaling.reason}{" decision="}{.status.autoscaling.lastScalingDecision}{" lastScaleUpTime="}{.status.autoscaling.lastScaleUpTime}{" desired="}{.status.replicas.desired}{" ready="}{.status.replicas.ready}{"\n"}' 2>/dev/null || true
}

patch_main_cluster() {
  local patch="$1"
  kubectl -n "${NAMESPACE}" patch nificluster "${HELM_RELEASE}" --type merge -p "${patch}" >/dev/null
}

configure_advisory_autoscaling() {
  local min_replicas="$1"
  local max_replicas="$2"
  patch_main_cluster "$(cat <<EOF
{"spec":{"autoscaling":{"mode":"Advisory","scaleUp":{"enabled":false,"cooldown":"5m"},"scaleDown":{"enabled":false},"minReplicas":${min_replicas},"maxReplicas":${max_replicas},"signals":["QueuePressure","CPU"]}}}
EOF
)"
}

configure_enforced_autoscaling() {
  local min_replicas="$1"
  local max_replicas="$2"
  patch_main_cluster "$(cat <<EOF
{"spec":{"autoscaling":{"mode":"Enforced","scaleUp":{"enabled":true,"cooldown":"5m"},"scaleDown":{"enabled":false},"minReplicas":${min_replicas},"maxReplicas":${max_replicas},"signals":["QueuePressure","CPU"]}}}
EOF
)"
}

configure_disabled_autoscaling() {
  patch_main_cluster '{"spec":{"autoscaling":{"mode":"Disabled","scaleUp":{"enabled":false,"cooldown":"5m"},"scaleDown":{"enabled":false},"minReplicas":0,"maxReplicas":0,"signals":[]}}}'
}

set_desired_state() {
  local desired_state="$1"
  patch_main_cluster "{\"spec\":{\"desiredState\":\"${desired_state}\"}}"
}

set_tls_diff_policy() {
  local policy="$1"
  patch_main_cluster "{\"spec\":{\"restartPolicy\":{\"tlsDrift\":\"${policy}\"}}}"
}

break_controller_pod_delete_rbac() {
  local mutated_role
  mutated_role="$(mktemp)"
  kubectl get clusterrole nifi-fabric-controller-manager -o json >"${mutated_role}"
  python3 - "${mutated_role}" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
payload = json.loads(path.read_text())
for rule in payload.get("rules", []):
    if rule.get("apiGroups") == [""] and rule.get("resources") == ["pods"]:
        rule["verbs"] = [verb for verb in rule.get("verbs", []) if verb != "delete"]
path.write_text(json.dumps(payload))
PY
  kubectl apply -f "${mutated_role}" >/dev/null
  rm -f "${mutated_role}"
}

restore_controller_rbac() {
  kubectl apply -f "${ROOT_DIR}/config/rbac/role.yaml" >/dev/null
}

nudge_main_cluster_reconcile() {
  kubectl -n "${NAMESPACE}" annotate nificluster "${HELM_RELEASE}" autoscaling-proof-reconcile="$(date +%s%N)" --overwrite >/dev/null
}

backup_tls_secret() {
  ORIGINAL_TLS_SECRET_FILE="$(mktemp)"
  kubectl -n "${NAMESPACE}" get secret "${TLS_SECRET}" -o json >"${ORIGINAL_TLS_SECRET_FILE}"
}

corrupt_tls_secret_ca() {
  local mutated_secret
  mutated_secret="$(mktemp)"
  kubectl -n "${NAMESPACE}" get secret "${TLS_SECRET}" -o json >"${mutated_secret}"
  python3 - "${mutated_secret}" <<'PY'
import base64
import json
import sys
import time
from pathlib import Path

path = Path(sys.argv[1])
payload = json.loads(path.read_text())
payload.setdefault("data", {})["ca.crt"] = base64.b64encode(b"not-a-valid-ca").decode()
payload["data"]["drift-marker"] = base64.b64encode(time.strftime("%Y%m%dT%H%M%SZ").encode()).decode()
path.write_text(json.dumps(payload))
PY
  kubectl apply -f "${mutated_secret}" >/dev/null
  rm -f "${mutated_secret}"
}

restore_tls_secret() {
  if [[ -n "${ORIGINAL_TLS_SECRET_FILE}" && -f "${ORIGINAL_TLS_SECRET_FILE}" ]]; then
    kubectl apply -f "${ORIGINAL_TLS_SECRET_FILE}" >/dev/null || true
  fi
}

create_temp_clusters() {
  kubectl -n "${NAMESPACE}" apply -f - <<EOF >/dev/null
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiCluster
metadata:
  name: ${UNRESOLVED_CLUSTER_NAME}
spec:
  targetRef:
    name: missing-target
  desiredState: Running
  autoscaling:
    mode: Advisory
    scaleUp:
      enabled: false
      cooldown: 2m
    scaleDown:
      enabled: false
    minReplicas: 2
    maxReplicas: 3
    signals:
    - QueuePressure
EOF

  kubectl -n "${NAMESPACE}" apply -f - <<EOF >/dev/null
apiVersion: v1
kind: Service
metadata:
  name: ${UNMANAGED_TARGET_NAME}-headless
spec:
  clusterIP: None
  selector:
    app: ${UNMANAGED_TARGET_NAME}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ${UNMANAGED_TARGET_NAME}
spec:
  serviceName: ${UNMANAGED_TARGET_NAME}-headless
  replicas: 1
  selector:
    matchLabels:
      app: ${UNMANAGED_TARGET_NAME}
  template:
    metadata:
      labels:
        app: ${UNMANAGED_TARGET_NAME}
    spec:
      containers:
      - name: pause
        image: registry.k8s.io/pause:3.10
---
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiCluster
metadata:
  name: ${UNMANAGED_CLUSTER_NAME}
spec:
  targetRef:
    name: ${UNMANAGED_TARGET_NAME}
  desiredState: Running
  autoscaling:
    mode: Advisory
    scaleUp:
      enabled: false
      cooldown: 2m
    scaleDown:
      enabled: false
    minReplicas: 2
    maxReplicas: 3
    signals:
    - QueuePressure
EOF

}

cleanup() {
  restore_tls_secret
  restore_controller_rbac >/dev/null 2>&1 || true
  kubectl -n "${NAMESPACE}" delete nificluster "${UNRESOLVED_CLUSTER_NAME}" "${UNMANAGED_CLUSTER_NAME}" --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n "${NAMESPACE}" delete statefulset "${UNMANAGED_TARGET_NAME}" --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n "${NAMESPACE}" delete service "${UNMANAGED_TARGET_NAME}-headless" --ignore-not-found >/dev/null 2>&1 || true
  rm -f "${ORIGINAL_TLS_SECRET_FILE}" >/dev/null 2>&1 || true
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

reinstall_main_release_after_degraded_proof() {
  phase "Reinstalling managed release after degraded autoscaling proof"
  phase "Restarting controller after degraded autoscaling proof"
  kubectl -n "${CONTROLLER_NAMESPACE}" rollout restart deployment/"${CONTROLLER_DEPLOYMENT}" >/dev/null
  kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
  kubectl -n "${NAMESPACE}" delete nificluster "${HELM_RELEASE}" --ignore-not-found=true --wait=true >/dev/null || true
  helm uninstall "${HELM_RELEASE}" --namespace "${NAMESPACE}" >/dev/null 2>&1 || true
  kubectl -n "${NAMESPACE}" wait --for=delete statefulset/"${HELM_RELEASE}" --timeout=5m >/dev/null 2>&1 || true
  install_main_release

  phase "Re-verifying health after degraded autoscaling proof"
  run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
  wait_for_condition "${HELM_RELEASE}" TargetResolved True 300
  wait_for_condition "${HELM_RELEASE}" Available True 300
}

dump_diagnostics() {
  set +e
  echo
  echo "==> autoscaling scale-up diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  echo
  echo "Main autoscaling summary:"
  print_autoscaling_summary "${HELM_RELEASE}"
  echo
  echo "Temporary autoscaling summaries:"
  print_autoscaling_summary "${UNRESOLVED_CLUSTER_NAME}"
  print_autoscaling_summary "${UNMANAGED_CLUSTER_NAME}"
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nificluster "${UNRESOLVED_CLUSTER_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nificluster "${UNMANAGED_CLUSTER_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${UNMANAGED_TARGET_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get pod -o wide || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 200 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 200 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=400 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR
trap cleanup EXIT

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
  phase "Reusing existing kind cluster for autoscaling scale-up proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for autoscaling scale-up proof"
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

phase "Proving advisory mode stays status-only"
configure_advisory_autoscaling 3 4
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonBelowMinReplicas:-BelowMinReplicas}" 180
wait_for_cluster_recommended "${HELM_RELEASE}" 3 180
wait_for_cluster_decision_contains "${HELM_RELEASE}" "autoscaling is not in enforced mode" 180
require_main_replicas 2
require_contains "autoscaling signal status" "QueuePressure" kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.autoscaling.signals[*].type}'

phase "Proving enforced mode scales up by one bounded step"
configure_enforced_autoscaling 4 4
wait_for_sts_replicas 3 600
wait_for_event_reason AutoscalingScaleUp 180
scale_up_time="$(wait_for_cluster_last_scale_up_time "${HELM_RELEASE}" 300)"
wait_for_cluster_decision_contains "${HELM_RELEASE}" "ScaleUp: increased target StatefulSet replicas from 2 to 3" 300
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition "${HELM_RELEASE}" Available True 300
wait_for_cluster_recommended "${HELM_RELEASE}" 4 180

phase "Proving cooldown blocks repeated immediate scale-up"
wait_for_cluster_decision_contains "${HELM_RELEASE}" "cooldown is active until" 180
sleep 20
require_main_replicas 3
require_output "lastScaleUpTime stability" "${scale_up_time}" cluster_jsonpath "${HELM_RELEASE}" '{.status.autoscaling.lastScaleUpTime}'

phase "Proving automatic scale-down remains disabled"
configure_enforced_autoscaling 1 2
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonAboveMaxReplicas:-AboveMaxReplicas}" 180
wait_for_cluster_recommended "${HELM_RELEASE}" 2 180
wait_for_cluster_decision_contains "${HELM_RELEASE}" "scale-down is not enabled" 180
require_main_replicas 3

phase "Proving target unresolved and unmanaged branches stay status-only"
create_temp_clusters
wait_for_cluster_reason "${UNRESOLVED_CLUSTER_NAME}" "${autoscalingReasonTargetNotResolved:-TargetNotResolved}" 180
wait_for_cluster_recommended_empty "${UNRESOLVED_CLUSTER_NAME}" 60
wait_for_cluster_reason "${UNMANAGED_CLUSTER_NAME}" "${autoscalingReasonUnmanagedTarget:-UnmanagedTarget}" 180
wait_for_cluster_recommended_empty "${UNMANAGED_CLUSTER_NAME}" 60

phase "Proving autoscaling is blocked while rollout is progressing"
configure_advisory_autoscaling 4 4
bash "${ROOT_DIR}/hack/trigger-config-drift.sh" --namespace "${NAMESPACE}" --configmap "${CONFIGMAP_NAME}"
wait_for_condition "${HELM_RELEASE}" Progressing True 300
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonProgressing:-Progressing}" 180
wait_for_cluster_recommended_empty "${HELM_RELEASE}" 60
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition "${HELM_RELEASE}" Available True 600

phase "Proving autoscaling is blocked while hibernated and restoring"
configure_advisory_autoscaling 4 4
set_desired_state Hibernated
wait_for_condition "${HELM_RELEASE}" Hibernated True 900
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonHibernated:-Hibernated}" 300
wait_for_cluster_recommended_empty "${HELM_RELEASE}" 60
set_desired_state Running
wait_for_condition "${HELM_RELEASE}" Progressing True 300
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonProgressing:-Progressing}" 180
wait_for_cluster_recommended_empty "${HELM_RELEASE}" 60
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition "${HELM_RELEASE}" Available True 900

phase "Proving autoscaling is blocked while degraded"
configure_enforced_autoscaling 4 4
break_controller_pod_delete_rbac
bash "${ROOT_DIR}/hack/trigger-config-drift.sh" --namespace "${NAMESPACE}" --configmap "${CONFIGMAP_NAME}"
wait_for_event_reason RolloutStarted 120
wait_for_event_reason RolloutFailed 180
wait_for_condition "${HELM_RELEASE}" Degraded True 180
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonDegraded:-Degraded}" 180
wait_for_cluster_recommended_empty "${HELM_RELEASE}" 60
wait_for_cluster_decision_contains "${HELM_RELEASE}" "recommendation is unavailable because Degraded" 180
restore_controller_rbac
reinstall_main_release_after_degraded_proof

phase "Restoring disabled autoscaling baseline"
configure_disabled_autoscaling
wait_for_cluster_reason "${HELM_RELEASE}" "${autoscalingReasonDisabled:-Disabled}" 180

print_success_footer "autoscaling scale-up-only runtime proof completed" \
  "make kind-autoscaling-scale-up-fast-e2e-reuse" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get events --sort-by=.lastTimestamp | tail -n 100" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=300"
