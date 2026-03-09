#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi2-platform}"
NAMESPACE="${NAMESPACE:-nifi}"
SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-nifi-system}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_IMAGE="${CONTROLLER_IMAGE:-nifi2-platform-controller:dev}"
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

dump_diagnostics() {
  set +e
  echo
  echo "==> diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl get ns || true
  kubectl -n "${SYSTEM_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,pvc,secret,configmap || true
  kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}" || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 50 || true
  kubectl -n "${SYSTEM_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 50 || true
  kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi2-platform-controller-manager --tail=200 || true
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
  while true; do
    if "$@"; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      return 1
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

wait_for_restore_target() {
  local expected="$1"
  wait_for "restore to target replicas" 1200 bash -ec '
    replicas="$(kubectl -n "'"${NAMESPACE}"'" get statefulset "'"${HELM_RELEASE}"'" -o jsonpath="{.spec.replicas}")"
    [[ "${replicas}" == "'"${expected}"'" ]]
  '
}

verify_metrics() {
  local pf_pid=""
  kubectl -n "${SYSTEM_NAMESPACE}" port-forward deployment/nifi2-platform-controller-manager 18080:8080 >/tmp/nifi-alpha-metrics.log 2>&1 &
  pf_pid=$!
  sleep 5
  if ! curl --silent --show-error --fail http://127.0.0.1:18080/metrics | grep -q 'nifi_platform_lifecycle_transitions_total'; then
    kill "${pf_pid}" >/dev/null 2>&1 || true
    wait "${pf_pid}" >/dev/null 2>&1 || true
    fail "controller metrics endpoint did not expose nifi_platform_lifecycle_transitions_total"
  fi
  kill "${pf_pid}" >/dev/null 2>&1 || true
  wait "${pf_pid}" >/dev/null 2>&1 || true
}

verify_events() {
  local count
  count="$(kubectl -n "${NAMESPACE}" get events --field-selector "involvedObject.kind=NiFiCluster,involvedObject.name=${HELM_RELEASE}" --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "${count}" == "0" ]]; then
    fail "expected Kubernetes events for NiFiCluster/${HELM_RELEASE}, found none"
  fi
}

require_command kind
require_command kubectl
require_command helm
require_command curl
require_command grep

log_step "creating a fresh kind cluster"
kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
run_make kind-up
kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null

log_step "installing controller prerequisites and the managed chart"
run_make kind-secrets
run_make install-crd
run_make docker-build-controller CONTROLLER_IMAGE="${CONTROLLER_IMAGE}"
run_make kind-load-controller CONTROLLER_IMAGE="${CONTROLLER_IMAGE}"
run_make deploy-controller
kubectl -n "${SYSTEM_NAMESPACE}" rollout status deployment/nifi2-platform-controller-manager --timeout=5m
run_make helm-install-managed
run_make apply-managed
run_make kind-health

log_step "exercising a managed StatefulSet revision rollout"
rollout_uids_before="$(pod_uid_snapshot)"
(cd "${ROOT_DIR}" && helm upgrade --install "${HELM_RELEASE}" charts/nifi --namespace "${NAMESPACE}" -f examples/managed/values.yaml --reuse-values --set-string podAnnotations.rolloutNonce="$(date +%s)")
target_revision="$(sts_jsonpath '{.status.updateRevision}')"
wait_for_revision_rollout_observed "${target_revision}" || fail "managed rollout was not observed by the controller"
wait_for_revision_rollout_complete "${target_revision}" || fail "managed rollout did not finish"
run_make kind-health
assert_all_pods_changed "${rollout_uids_before}" "managed rollout"

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
wait_for_rollout_clear || fail "TLS restart-required rollout did not finish"
run_make kind-health
assert_all_pods_changed "${tls_restart_uids_before}" "TLS restart-required rollout"

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
wait_for_restore_target "${restore_target}" || fail "restore target replicas were not applied"
run_make kind-health

log_step "verifying basic alpha observability"
verify_events
verify_metrics

echo
echo "PASS: fresh-kind alpha e2e completed successfully in +$(elapsed)s"
echo "  managed install"
echo "  health check"
echo "  managed rollout"
echo "  config drift rollout"
echo "  TLS observe-only path"
echo "  TLS restart-required path"
echo "  hibernation"
echo "  restore"
