#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric}"
NAMESPACE="${NAMESPACE:-nifi}"
SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-nifi-system}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_IMAGE="${CONTROLLER_IMAGE:-nifi-fabric-controller:dev}"
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

dump_tls_restart_diagnostics() {
  set +e
  echo
  echo "==> TLS restart diagnostics at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,pvc,secret,configmap || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}{"\n"}{.status.currentRevision}{"\n"}{.status.updateRevision}{"\n"}{.status.currentReplicas}{"\n"}{.status.updatedReplicas}{"\n"}{.status.readyReplicas}{"\n"}' || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 80 || true
  kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi-fabric-controller-manager --tail=300 || true
}

fail() {
  echo "FAIL: $*" >&2
  dump_tls_restart_diagnostics
  exit 1
}

trap 'fail "TLS restart-required workflow aborted"' ERR

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

wait_for_rollout_clear() {
  wait_for "TLS rollout status to clear" 1200 bash -ec '
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    phase="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.lastOperation.phase}")"
    [[ -z "${trigger}" && "${phase}" == "Succeeded" ]]
  '
}

wait_for_tls_config_hash() {
  local previous_hash="$1"
  wait_for "TLS configuration hash to advance" 900 bash -ec '
    current="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.observedTLSConfigurationHash}")"
    [[ -n "${current}" && "${current}" != "'"${previous_hash}"'" ]]
  '
}

wait_for_tls_rollout_observed() {
  wait_for "TLS rollout to start" 300 bash -ec '
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    [[ "${trigger}" == "TLSDrift" ]]
  '
}

require_command kind
require_command kubectl
require_command helm

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
kubectl -n "${SYSTEM_NAMESPACE}" rollout status deployment/nifi-fabric-controller-manager --timeout=5m
run_make helm-install-managed
run_make apply-managed
run_make kind-health

log_step "exercising restart-required TLS drift handling only"
tls_config_hash_before="$(cluster_jsonpath '{.status.observedTLSConfigurationHash}')"
tls_restart_uids_before="$(pod_uid_snapshot)"
run_make kind-tls-config-drift
wait_for_tls_rollout_observed || fail "TLS restart-required rollout was not observed"
wait_for_rollout_clear || fail "TLS restart-required rollout did not finish"
wait_for_tls_config_hash "${tls_config_hash_before}" || fail "TLS configuration hash did not advance after rollout completion"
run_make kind-health
assert_all_pods_changed "${tls_restart_uids_before}" "TLS restart-required rollout"

echo
echo "PASS: TLS restart-required workflow completed successfully at +$(elapsed)s"
