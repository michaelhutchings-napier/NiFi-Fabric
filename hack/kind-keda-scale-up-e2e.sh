#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-keda-scale-up}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
VERSION_VALUES_FILE="${VERSION_VALUES_FILE:-examples/nifi-2.8.0-values.yaml}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_KEDA_VALUES_FILE="${PLATFORM_KEDA_VALUES_FILE:-examples/platform-managed-keda-values.yaml}"
KEDA_NAMESPACE="${KEDA_NAMESPACE:-keda}"
KEDA_RELEASE="${KEDA_RELEASE:-keda}"
KEDA_CHART_REPO="${KEDA_CHART_REPO:-https://kedacore.github.io/charts}"
KEDA_CHART_VERSION="${KEDA_CHART_VERSION:-}"
START_EPOCH="$(date +%s)"

TMP_VALUES_FILE=""

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
  local jsonpath="$1"
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

sts_jsonpath() {
  local jsonpath="$1"
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

scaled_object_jsonpath() {
  local jsonpath="$1"
  kubectl -n "${NAMESPACE}" get scaledobject "${HELM_RELEASE}-keda" -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

scale_subresource_replicas() {
  local field="$1"
  kubectl get --raw "/apis/platform.nifi.io/v1alpha1/namespaces/${NAMESPACE}/nificlusters/${HELM_RELEASE}/scale" 2>/dev/null | \
    python3 -c "import json,sys; payload=json.load(sys.stdin); print(payload.get('${field}', {}).get('replicas', ''))"
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

wait_for_changed_output() {
  local description="$1"
  local previous="$2"
  local timeout_seconds="$3"
  shift 3
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local actual=""

  while true; do
    actual="$("$@" | tr -d '\n')"
    if [[ -n "${actual}" && "${actual}" != "${previous}" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${description} to change from ${previous:-<empty>}: got ${actual:-<empty>}" >&2
      return 1
    fi
    sleep 5
  done
}

wait_for_condition_true() {
  local type="$1"
  wait_for_output "condition ${type}=True" "True" 600 \
    cluster_jsonpath "{.status.conditions[?(@.type==\"${type}\")].status}"
}

wait_for_scaledobject_hpa() {
  local timeout_seconds="${1:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local hpa_name=""

  while true; do
    hpa_name="$(scaled_object_jsonpath '{.status.hpaName}' | tr -d '\n')"
    if [[ -n "${hpa_name}" ]]; then
      printf '%s\n' "${hpa_name}"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ScaledObject to publish status.hpaName" >&2
      return 1
    fi
    sleep 5
  done
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

event_messages_for_reason() {
  local reason="$1"
  kubectl -n "${NAMESPACE}" get events \
    --field-selector "involvedObject.kind=NiFiCluster,involvedObject.name=${HELM_RELEASE},reason=${reason}" \
    -o jsonpath='{range .items[*]}{.message}{"\n"}{end}' 2>/dev/null || true
}

install_keda() {
  local helm_args=()
  if [[ -n "${KEDA_CHART_VERSION}" ]]; then
    helm_args+=(--version "${KEDA_CHART_VERSION}")
  fi

  helm repo add kedacore "${KEDA_CHART_REPO}" >/dev/null 2>&1 || true
  helm repo update kedacore >/dev/null
  helm upgrade --install "${KEDA_RELEASE}" kedacore/keda \
    --namespace "${KEDA_NAMESPACE}" \
    --create-namespace \
    --wait \
    --timeout 10m \
    "${helm_args[@]}"

  kubectl get crd scaledobjects.keda.sh >/dev/null
  kubectl -n "${KEDA_NAMESPACE}" wait --for=condition=Available deployment --all --timeout=10m
}

build_runtime_values() {
  TMP_VALUES_FILE="$(mktemp)"

  local future_epoch
  future_epoch="$(( $(date -u +%s) + 86400 ))"
  local end_epoch="$(( future_epoch + 600 ))"

  local start_cron
  start_cron="$(date -u -d "@${future_epoch}" '+%M %H %d %m *')"
  local end_cron
  end_cron="$(date -u -d "@${end_epoch}" '+%M %H %d %m *')"

  cat >"${TMP_VALUES_FILE}" <<EOF
keda:
  pollingInterval: 10
  cooldownPeriod: 30
  minReplicaCount: 2
  maxReplicaCount: 3
  triggers:
    - type: cron
      metadata:
        timezone: UTC
        start: "${start_cron}"
        end: "${end_cron}"
        desiredReplicas: "3"
EOF
}

arm_scaledobject_for_scale_up_window() {
  local next_minute_epoch
  next_minute_epoch="$(( ( $(date -u +%s) / 60 + 1 ) * 60 ))"
  local end_epoch="$(( next_minute_epoch + 600 ))"

  local start_cron
  start_cron="$(date -u -d "@${next_minute_epoch}" '+%M %H %d %m *')"
  local end_cron
  end_cron="$(date -u -d "@${end_epoch}" '+%M %H %d %m *')"

  kubectl -n "${NAMESPACE}" patch scaledobject "${HELM_RELEASE}-keda" --type merge -p "$(cat <<EOF
{"spec":{"triggers":[{"type":"cron","metadata":{"timezone":"UTC","start":"${start_cron}","end":"${end_cron}","desiredReplicas":"3"}}]}}
EOF
)" >/dev/null
}

install_platform_release() {
  local helm_values_args=(
    -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
    -f "${ROOT_DIR}/${VERSION_VALUES_FILE}"
    -f "${ROOT_DIR}/${PLATFORM_KEDA_VALUES_FILE}"
    -f "${TMP_VALUES_FILE}"
  )

  if [[ "${FAST_PROFILE}" == "true" ]]; then
    helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  fi

  helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
    --namespace "${NAMESPACE}" \
    --create-namespace \
    "${helm_values_args[@]}"
}

dump_diagnostics() {
  set +e
  echo
  echo "==> KEDA scale-up diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  echo
  echo "Platform release:"
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  echo
  echo "KEDA release:"
  helm -n "${KEDA_NAMESPACE}" status "${KEDA_RELEASE}" || true
  echo
  echo "NiFiCluster autoscaling summary:"
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.metadata.name}{" externalRequested="}{.spec.autoscaling.external.requestedReplicas}{" statusExternalRequested="}{.status.autoscaling.external.requestedReplicas}{" statusExternalObserved="}{.status.autoscaling.external.observed}{" statusExternalActionable="}{.status.autoscaling.external.actionable}{" statusExternalScaleDownIgnored="}{.status.autoscaling.external.scaleDownIgnored}{" reason="}{.status.autoscaling.reason}{" decision="}{.status.autoscaling.lastScalingDecision}{" executionPhase="}{.status.autoscaling.execution.phase}{" executionState="}{.status.autoscaling.execution.state}{" desired="}{.status.replicas.desired}{" ready="}{.status.replicas.ready}{"\n"}' 2>/dev/null || true
  echo
  echo "Scale subresource:"
  kubectl get --raw "/apis/platform.nifi.io/v1alpha1/namespaces/${NAMESPACE}/nificlusters/${HELM_RELEASE}/scale" || true
  echo
  echo "ScaledObject:"
  kubectl -n "${NAMESPACE}" get scaledobject "${HELM_RELEASE}-keda" -o yaml || true
  echo
  echo "HPA:"
  hpa_name="$(scaled_object_jsonpath '{.status.hpaName}' | tr -d '\n')"
  if [[ -n "${hpa_name}" ]]; then
    kubectl -n "${NAMESPACE}" get hpa "${hpa_name}" -o yaml || true
  else
    kubectl -n "${NAMESPACE}" get hpa || true
  fi
  echo
  echo "KEDA operator resources:"
  kubectl -n "${KEDA_NAMESPACE}" get deployment,pod,service,apiservice || true
  echo
  echo "Workload resources:"
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,scaledobject,hpa || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 200 || true
  kubectl -n "${KEDA_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 200 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 200 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=400 || true
  kubectl -n "${KEDA_NAMESPACE}" logs deployment/keda-operator --tail=400 || true
  kubectl -n "${KEDA_NAMESPACE}" logs deployment/keda-operator-metrics-apiserver --tail=200 || true
  kubectl -n "${KEDA_NAMESPACE}" logs deployment/keda-admission-webhooks --tail=200 || true
}

cleanup() {
  rm -f "${TMP_VALUES_FILE}" >/dev/null 2>&1 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR
trap cleanup EXIT

phase "Checking prerequisites"
check_prereqs
require_command python3

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for KEDA scale-up proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for KEDA scale-up proof"
  ensure_fresh_cluster
  configure_kind_kubeconfig

  phase "Loading NiFi image into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Creating TLS and auth Secrets"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
fi

phase "Installing KEDA"
install_keda

phase "Building KEDA runtime trigger values"
build_runtime_values

phase "Installing managed platform release with KEDA overlay"
install_platform_release

phase "Verifying baseline platform and cluster health"
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=10m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get scaledobject "${HELM_RELEASE}-keda" >/dev/null
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available
wait_for_output "baseline StatefulSet replicas" "2" 300 sts_jsonpath '{.spec.replicas}'

phase "Proving external scale-down intent is still ignored"
kubectl -n "${NAMESPACE}" patch nificluster "${HELM_RELEASE}" --type merge -p '{"spec":{"autoscaling":{"external":{"enabled":true,"source":"KEDA","requestedReplicas":1}}}}' >/dev/null
wait_for_output "external requested replicas 1" "1" 30 cluster_jsonpath '{.spec.autoscaling.external.requestedReplicas}'
wait_for_contains "ignored external scale-down event" "requested scale-down intent to 1 replicas, but external scale-down is ignored" 120 event_messages_for_reason AutoscalingRecommendationUpdated
wait_for_output "StatefulSet replicas remain unchanged after ignored scale-down" "2" 60 sts_jsonpath '{.spec.replicas}'

phase "Proving KEDA targets NiFiCluster rather than the StatefulSet"
wait_for_output "ScaledObject target kind" "NiFiCluster" 120 scaled_object_jsonpath '{.spec.scaleTargetRef.kind}'
wait_for_output "ScaledObject target apiVersion" "platform.nifi.io/v1alpha1" 120 scaled_object_jsonpath '{.spec.scaleTargetRef.apiVersion}'
hpa_name="$(wait_for_scaledobject_hpa 300)"
wait_for_output "HPA target kind" "NiFiCluster" 120 kubectl -n "${NAMESPACE}" get hpa "${hpa_name}" -o jsonpath='{.spec.scaleTargetRef.kind}'
wait_for_output "HPA target name" "${HELM_RELEASE}" 120 kubectl -n "${NAMESPACE}" get hpa "${hpa_name}" -o jsonpath='{.spec.scaleTargetRef.name}'

phase "Arming KEDA cron trigger for the live scale-up window"
previous_last_scale_up_time="$(cluster_jsonpath '{.status.autoscaling.lastScaleUpTime}' | tr -d '\n')"
arm_scaledobject_for_scale_up_window

phase "Proving KEDA writes scale intent through NiFiCluster /scale"
wait_for_output "scale subresource desired replicas from KEDA" "3" 300 scale_subresource_replicas spec
wait_for_output "external requested replicas from KEDA" "3" 300 cluster_jsonpath '{.spec.autoscaling.external.requestedReplicas}'
wait_for_output "status external requested replicas from KEDA" "3" 300 cluster_jsonpath '{.status.autoscaling.external.requestedReplicas}'
wait_for_output "status external intent observed" "true" 300 cluster_jsonpath '{.status.autoscaling.external.observed}'
wait_for_contains "external KEDA status message" "KEDA" 300 cluster_jsonpath '{.status.autoscaling.external.message}'

phase "Proving the controller remains the sole executor of safe scale-up"
wait_for_event_reason AutoscalingScaleUp 300
wait_for_changed_output "controller lastScaleUpTime" "${previous_last_scale_up_time}" 300 cluster_jsonpath '{.status.autoscaling.lastScaleUpTime}'
wait_for_output "StatefulSet replicas after controller scale-up" "3" 600 sts_jsonpath '{.spec.replicas}'
wait_for_output "scale subresource status replicas after controller scale-up" "3" 300 scale_subresource_replicas status
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true Available

print_success_footer "experimental KEDA scale-up runtime proof completed" \
  "make kind-keda-scale-up-fast-e2e-reuse" \
  "kubectl -n ${NAMESPACE} get scaledobject ${HELM_RELEASE}-keda -o yaml" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${KEDA_NAMESPACE} logs deployment/keda-operator --tail=200" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
