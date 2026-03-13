#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric}"
NAMESPACE="${NAMESPACE:-nifi}"
SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-nifi-system}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_IMAGE="${CONTROLLER_IMAGE:-nifi-fabric-controller:dev}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
PHASE="${PHASE:-full}"
START_EPOCH="$(date +%s)"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

log_step() {
  printf '\n==> %s\n' "$1"
}

elapsed() {
  echo "$(( $(date +%s) - START_EPOCH ))"
}

usage() {
  cat <<'EOF'
Usage: hack/kind-alpha-e2e.sh [--phase full|rollout|config-drift|tls|hibernate] [--artifacts-dir DIR]
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --phase)
      PHASE="${2:-}"
      shift 2
      ;;
    --artifacts-dir)
      ARTIFACT_DIR="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

case "${PHASE}" in
  full|rollout|config-drift|tls|hibernate)
    ;;
  *)
    echo "invalid phase: ${PHASE}" >&2
    usage
    exit 1
    ;;
esac

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

controller_metrics_snapshot() {
  local log_file="${1:-/tmp/nifi-alpha-metrics.log}"
  local pf_pid=""
  local metrics=""

  cleanup() {
    if [[ -n "${pf_pid}" ]]; then
      kill "${pf_pid}" >/dev/null 2>&1 || true
      wait "${pf_pid}" >/dev/null 2>&1 || true
    fi
  }

  kubectl -n "${SYSTEM_NAMESPACE}" port-forward --address 127.0.0.1 deployment/nifi-fabric-controller-manager 18080:8080 >"${log_file}" 2>&1 &
  pf_pid=$!

  if ! wait_for "controller metrics port-forward" 30 bash -ec '
    curl --silent --show-error --fail --max-time 2 http://127.0.0.1:18080/metrics >/dev/null
  '; then
    cleanup
    echo "controller metrics port-forward did not become ready" >&2
    [[ -f "${log_file}" ]] && cat "${log_file}" >&2
    return 1
  fi

  if ! metrics="$(curl --silent --show-error --fail --max-time 10 http://127.0.0.1:18080/metrics)"; then
    cleanup
    echo "controller metrics endpoint did not return a response" >&2
    [[ -f "${log_file}" ]] && cat "${log_file}" >&2
    return 1
  fi

  cleanup
  printf '%s' "${metrics}"
}

dump_diagnostics() {
  set +e
  echo
  echo "==> diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl get ns || true
  kubectl -n "${SYSTEM_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,pvc,secret,configmap || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}" || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}{"\n"}{.status.readyReplicas}{"\n"}{.status.currentRevision}{"\n"}{.status.updateRevision}{"\n"}' || true
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp || true
  kubectl -n "${NAMESPACE}" describe pods || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${SYSTEM_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi-fabric-controller-manager --tail=300 || true

  capture_cmd cluster-info kubectl cluster-info --context "kind-${KIND_CLUSTER_NAME}"
  capture_cmd namespaces kubectl get ns -o wide
  capture_cmd system-workloads kubectl -n "${SYSTEM_NAMESPACE}" get deployment,pod -o wide
  capture_cmd nificluster-yaml kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml
  capture_cmd nificluster-status kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
  capture_cmd nificluster-describe kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}"
  capture_cmd statefulset-yaml kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml
  capture_cmd statefulset-status kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}{"\n"}{.status.readyReplicas}{"\n"}{.status.currentRevision}{"\n"}{.status.updateRevision}{"\n"}'
  capture_cmd pods-summary kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp
  capture_cmd pods-describe kubectl -n "${NAMESPACE}" describe pods
  capture_cmd nifi-events bash -lc "kubectl -n '${NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 200"
  capture_cmd system-events bash -lc "kubectl -n '${SYSTEM_NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 200"
  capture_cmd controller-logs kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi-fabric-controller-manager --tail=500
  if [[ -n "${ARTIFACT_DIR}" ]]; then
    {
      echo "### controller-metrics"
      controller_metrics_snapshot "${ARTIFACT_DIR}/controller-metrics-port-forward.log"
    } >"${ARTIFACT_DIR}/controller-metrics.log" 2>&1 || true
  fi
}

dump_tls_restart_diagnostics() {
  set +e
  echo
  echo "==> TLS restart diagnostics at +$(elapsed)s"
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.rollout.trigger}{"\n"}{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{.status.nodeOperation.podName}{" "}{.status.nodeOperation.stage}{" "}{.status.nodeOperation.nodeID}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 80 || true
  kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi-fabric-controller-manager --tail=300 || true

  capture_cmd tls-restart-status kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml
  capture_cmd tls-restart-pods kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp
}

fail() {
  echo "FAIL: $*" >&2
  dump_diagnostics
  exit 1
}

trap 'fail "alpha e2e workflow aborted"' ERR

run_make() {
  (cd "${ROOT_DIR}" && make "$@")
}

wait_for() {
  local description="$1"
  local timeout="$2"
  shift 2

  local deadline=$(( $(date +%s) + timeout ))
  local attempts=0
  while true; do
    attempts=$((attempts + 1))
    if "$@"; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${description} after ${timeout}s" >&2
      return 1
    fi
    if (( attempts == 1 || attempts % 12 == 0 )); then
      echo "waiting for ${description} (+$(( timeout - (deadline - $(date +%s)) ))s elapsed)" >&2
    fi
    sleep 5
  done
}

cluster_jsonpath() {
  local path="$1"
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o "jsonpath=${path}"
}

sts_jsonpath() {
  local path="$1"
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o "jsonpath=${path}"
}

cluster_condition_status() {
  local condition_type="$1"
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o "jsonpath={.status.conditions[?(@.type==\"${condition_type}\")].status}"
}

cluster_last_operation_phase() {
  cluster_jsonpath '{.status.lastOperation.phase}'
}

print_hibernate_restore_state() {
  echo "hibernation state snapshot:" >&2
  echo "  desiredState=$(cluster_jsonpath '{.spec.desiredState}')" >&2
  echo "  statefulset.spec.replicas=$(sts_jsonpath '{.spec.replicas}')" >&2
  echo "  statefulset.status.readyReplicas=$(sts_jsonpath '{.status.readyReplicas}')" >&2
  echo "  Available=$(cluster_condition_status 'Available')" >&2
  echo "  Progressing=$(cluster_condition_status 'Progressing')" >&2
  echo "  Hibernated=$(cluster_condition_status 'Hibernated')" >&2
  echo "  lastOperation.phase=$(cluster_last_operation_phase)" >&2
  echo "  lastOperation.message=$(cluster_jsonpath '{.status.lastOperation.message}')" >&2
}

pod_uid_snapshot() {
  local replicas
  replicas="$(sts_jsonpath '{.spec.replicas}')"
  local output=""
  local ordinal
  for ((ordinal = 0; ordinal < replicas; ordinal++)); do
    local pod="${HELM_RELEASE}-${ordinal}"
    local uid
    uid="$(kubectl -n "${NAMESPACE}" get pod "${pod}" -o jsonpath='{.metadata.uid}')"
    output+="${pod}=${uid}"$'\n'
  done
  printf '%s' "${output}" | sort
}

assert_all_pods_changed() {
  local before="$1"
  local description="$2"
  local after
  after="$(pod_uid_snapshot)"
  while IFS='=' read -r pod uid_before; do
    [[ -z "${pod}" ]] && continue
    uid_after="$(printf '%s\n' "${after}" | awk -F= -v pod="${pod}" '$1 == pod { print $2 }')"
    if [[ -z "${uid_after}" ]]; then
      fail "${description} is missing pod ${pod} after the workflow step"
    fi
    if [[ "${uid_before}" == "${uid_after}" ]]; then
      fail "${description} did not recreate pod ${pod}"
    fi
  done <<< "${before}"
}

assert_pods_unchanged() {
  local before="$1"
  local description="$2"
  local after
  after="$(pod_uid_snapshot)"
  if [[ "${before}" != "${after}" ]]; then
    echo "before:"
    printf '%s\n' "${before}"
    echo "after:"
    printf '%s\n' "${after}"
    fail "${description} unexpectedly recreated pods"
  fi
}

wait_for_rollout_clear() {
  wait_for "rollout status to clear" 900 bash -ec '
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    phase="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.lastOperation.phase}")"
    [[ -z "${trigger}" && "${phase}" == "Succeeded" ]]
  '
}

wait_for_revision_rollout_observed() {
  local expected_revision="$1"
  wait_for "revision rollout to start or settle" 300 bash -ec '
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    observed="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.observedStatefulSetRevision}")"
    [[ "${trigger}" == "StatefulSetRevision" || "${observed}" == "'"${expected_revision}"'" ]]
  '
}

wait_for_revision_rollout_complete() {
  local expected_revision="$1"
  wait_for "revision rollout to complete" 900 bash -ec '
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    phase="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.lastOperation.phase}")"
    observed="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.observedStatefulSetRevision}")"
    [[ -z "${trigger}" && "${phase}" == "Succeeded" && "${observed}" == "'"${expected_revision}"'" ]]
  '
}

wait_for_tls_observed_hash() {
  local previous_hash="$1"
  wait_for "TLS observed hash to advance without rollout" 600 bash -ec '
    current_hash="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.observedCertificateHash}")"
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    progressing_reason="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.conditions[?(@.type==\"Progressing\")].reason}")"
    [[ -n "${current_hash}" && "${current_hash}" != "'"${previous_hash}"'" && -z "${trigger}" && "${progressing_reason}" != "TLSAutoreloadObserving" ]]
  '
}

wait_for_hibernated() {
  wait_for "hibernation to finish" 1200 bash -ec '
    replicas="$(kubectl -n "'"${NAMESPACE}"'" get statefulset "'"${HELM_RELEASE}"'" -o jsonpath="{.spec.replicas}")"
    hibernated="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.conditions[?(@.type==\"Hibernated\")].status}")"
    [[ "${replicas}" == "0" && "${hibernated}" == "True" ]]
  '
}

wait_for_restore_complete() {
  local expected="$1"
  wait_for "restore to settle at ${expected} replicas" 1200 bash -ec '
    replicas="$(kubectl -n "'"${NAMESPACE}"'" get statefulset "'"${HELM_RELEASE}"'" -o jsonpath="{.spec.replicas}")"
    available="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.conditions[?(@.type==\"Available\")].status}")"
    progressing="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.conditions[?(@.type==\"Progressing\")].status}")"
    hibernated="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.conditions[?(@.type==\"Hibernated\")].status}")"
    phase="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.lastOperation.phase}")"
    [[ "${replicas}" == "'"${expected}"'" && "${available}" == "True" && "${progressing}" == "False" && "${hibernated}" == "False" && "${phase}" == "Succeeded" ]]
  '
}

verify_metrics() {
  local metrics
  metrics="$(controller_metrics_snapshot)"
  if ! grep -q 'nifi_platform_lifecycle_transitions_total' <<<"${metrics}" || \
     ! grep -q 'nifi_platform_rollouts_total' <<<"${metrics}" || \
     ! grep -q 'nifi_platform_tls_actions_total' <<<"${metrics}" || \
     ! grep -q 'nifi_platform_hibernation_operations_total' <<<"${metrics}" || \
     ! grep -q 'nifi_platform_node_preparation_outcomes_total' <<<"${metrics}"; then
    fail "controller metrics endpoint did not expose the expected lifecycle metric set"
  fi
}

verify_events() {
  local count
  count="$(kubectl -n "${NAMESPACE}" get events --field-selector "involvedObject.kind=NiFiCluster,involvedObject.name=${HELM_RELEASE}" --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "${count}" == "0" ]]; then
    fail "expected Kubernetes events for NiFiCluster/${HELM_RELEASE}, found none"
  fi
}

bootstrap_managed_cluster() {
  log_step "creating a fresh kind cluster"
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null
  log_step "preloading the NiFi runtime image into kind"
  run_make kind-load-nifi-image

  log_step "installing controller prerequisites and the managed chart"
  run_make kind-secrets
  run_make install-crd
  run_make docker-build-controller CONTROLLER_IMAGE="${CONTROLLER_IMAGE}"
  run_make kind-load-controller CONTROLLER_IMAGE="${CONTROLLER_IMAGE}"
  run_make deploy-controller
  kubectl -n "${SYSTEM_NAMESPACE}" rollout status deployment/nifi-fabric-controller-manager --timeout=5m
  run_make helm-install-managed
  run_make apply-managed
  run_make kind-health
}

run_phase_rollout() {
  log_step "exercising a managed StatefulSet revision rollout"
  rollout_uids_before="$(pod_uid_snapshot)"
  (cd "${ROOT_DIR}" && helm upgrade --install "${HELM_RELEASE}" charts/nifi --namespace "${NAMESPACE}" -f examples/managed/values.yaml --reuse-values --set-string podAnnotations.rolloutNonce="$(date +%s)")
  target_revision="$(sts_jsonpath '{.status.updateRevision}')"
  wait_for_revision_rollout_observed "${target_revision}" || fail "managed rollout was not observed by the controller"
  wait_for_revision_rollout_complete "${target_revision}" || fail "managed rollout did not finish"
  run_make kind-health
  assert_all_pods_changed "${rollout_uids_before}" "managed rollout"
}

run_phase_config_drift() {
  log_step "exercising watched ConfigMap drift rollout"
  config_hash_before="$(cluster_jsonpath '{.status.observedConfigHash}')"
  config_uids_before="$(pod_uid_snapshot)"
  run_make kind-config-drift
  wait_for "config drift hash to advance" 900 bash -ec '
    current="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.observedConfigHash}")"
    [[ -n "${current}" && "${current}" != "'"${config_hash_before}"'" ]]
  '
  wait_for_rollout_clear || fail "config drift rollout did not finish"
  run_make kind-health
  assert_all_pods_changed "${config_uids_before}" "config drift rollout"
}

run_phase_tls() {
  log_step "exercising TLS observe-only drift handling"
  kubectl -n "${NAMESPACE}" patch nificluster "${HELM_RELEASE}" --type merge -p '{"spec":{"restartPolicy":{"tlsDrift":"ObserveOnly"}}}' >/dev/null
  tls_uids_before="$(pod_uid_snapshot)"
  tls_hash_before="$(cluster_jsonpath '{.status.observedCertificateHash}')"
  run_make kind-tls-drift
  wait_for_tls_observed_hash "${tls_hash_before}" || fail "TLS observe-only path did not reconcile the certificate hash"
  run_make kind-health
  assert_pods_unchanged "${tls_uids_before}" "TLS observe-only drift"

  log_step "exercising restart-required TLS drift handling"
  tls_config_hash_before="$(cluster_jsonpath '{.status.observedTLSConfigurationHash}')"
  tls_restart_uids_before="$(pod_uid_snapshot)"
  run_make kind-tls-config-drift
  wait_for "TLS configuration hash to advance" 900 bash -ec '
    current="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.observedTLSConfigurationHash}")"
    [[ -n "${current}" && "${current}" != "'"${tls_config_hash_before}"'" ]]
  '
  if ! wait_for_rollout_clear; then
    dump_tls_restart_diagnostics
    fail "TLS restart-required rollout did not finish"
  fi
  run_make kind-health
  assert_all_pods_changed "${tls_restart_uids_before}" "TLS restart-required rollout"
}

run_phase_hibernate() {
  log_step "hibernating the managed cluster"
  restore_target="$(cluster_jsonpath '{.status.hibernation.lastRunningReplicas}')"
  if [[ -z "${restore_target}" || "${restore_target}" == "0" ]]; then
    restore_target="$(cluster_jsonpath '{.status.hibernation.baselineReplicas}')"
  fi
  pvc_count_before="$(kubectl -n "${NAMESPACE}" get pvc --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  run_make kind-hibernate
  wait_for_hibernated || fail "hibernation did not complete"
  pvc_count_after="$(kubectl -n "${NAMESPACE}" get pvc --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "${pvc_count_before}" != "${pvc_count_after}" ]]; then
    fail "PVC count changed during hibernation (${pvc_count_before} -> ${pvc_count_after})"
  fi

  log_step "restoring the managed cluster"
  run_make kind-restore
  if [[ -z "${restore_target}" || "${restore_target}" == "0" ]]; then
    restore_target="1"
  fi
  if ! wait_for_restore_complete "${restore_target}"; then
    print_hibernate_restore_state
    fail "restore did not settle cleanly at ${restore_target} replicas"
  fi
  run_make kind-health
}

verify_alpha_observability() {
  log_step "verifying basic alpha observability"
  verify_events
  verify_metrics
}

print_success() {
  echo
  echo "PASS: ${PHASE} private-alpha workflow completed successfully in +$(elapsed)s"
  case "${PHASE}" in
    full)
      echo "  managed install"
      echo "  health check"
      echo "  managed rollout"
      echo "  config drift rollout"
      echo "  TLS observe-only path"
      echo "  TLS restart-required path"
      echo "  hibernation"
      echo "  restore"
      ;;
    rollout)
      echo "  managed install"
      echo "  health check"
      echo "  managed rollout"
      ;;
    config-drift)
      echo "  managed install"
      echo "  health check"
      echo "  config drift rollout"
      ;;
    tls)
      echo "  managed install"
      echo "  health check"
      echo "  TLS observe-only path"
      echo "  TLS restart-required path"
      ;;
    hibernate)
      echo "  managed install"
      echo "  health check"
      echo "  hibernation"
      echo "  restore"
      ;;
  esac
}

require_command kind
require_command kubectl
require_command helm
require_command curl
require_command grep

bootstrap_managed_cluster

case "${PHASE}" in
  rollout)
    run_phase_rollout
    ;;
  config-drift)
    run_phase_config_drift
    ;;
  tls)
    run_phase_tls
    ;;
  hibernate)
    run_phase_hibernate
    ;;
  full)
    run_phase_rollout
    run_phase_config_drift
    run_phase_tls
    run_phase_hibernate
    ;;
esac

verify_alpha_observability
print_success
