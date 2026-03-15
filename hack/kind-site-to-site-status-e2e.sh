#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-site-to-site-status}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
RECEIVER_NAMESPACE="${RECEIVER_NAMESPACE:-site-to-site-receiver}"
RECEIVER_RELEASE="${RECEIVER_RELEASE:-site-to-site-receiver}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_STATUS_VALUES_FILE="${PLATFORM_STATUS_VALUES_FILE:-examples/platform-managed-site-to-site-status-values.yaml}"
PLATFORM_KIND_VALUES_FILE="${PLATFORM_KIND_VALUES_FILE:-examples/platform-managed-site-to-site-status-kind-values.yaml}"
SITE_TO_SITE_AUTHORIZED_IDENTITY="${SITE_TO_SITE_AUTHORIZED_IDENTITY:-O=NiFi-Fabric, CN=nifi-site-to-site-status-client}"
SENDER_CLIENT_SECRET_NAME="${SENDER_CLIENT_SECRET_NAME:-nifi-site-to-site-receiver-status-client}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
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
  echo "==> site-to-site status diagnostics after failure at +$(elapsed)s"
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,configmap,secret || true
  kubectl -n "${RECEIVER_NAMESPACE}" get statefulset,pod,service,secret,configmap || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}" || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${RECEIVER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --tail=300 || true
  kubectl -n "${RECEIVER_NAMESPACE}" logs "${RECEIVER_RELEASE}-0" -c nifi --tail=300 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
  -f "${ROOT_DIR}/${PLATFORM_STATUS_VALUES_FILE}"
  -f "${ROOT_DIR}/${PLATFORM_KIND_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
check_prereqs

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for Site-to-Site status runtime proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for Site-to-Site status runtime proof"
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

phase "Installing focused Site-to-Site receiver proof harness"
bash "${ROOT_DIR}/hack/bootstrap-site-to-site-receiver.sh" \
  --sender-namespace "${NAMESPACE}" \
  --sender-release "${HELM_RELEASE}" \
  --sender-client-secret "${SENDER_CLIENT_SECRET_NAME}" \
  --receiver-namespace "${RECEIVER_NAMESPACE}" \
  --receiver-release "${RECEIVER_RELEASE}" \
  --authorized-identity "${SITE_TO_SITE_AUTHORIZED_IDENTITY}" \
  --input-port "nifi-status"

phase "Installing product chart managed release${profile_label}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

phase "Verifying platform resources and controller rollout"
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

phase "Verifying cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available

phase "Proving typed Site-to-Site status bootstrap"
bash "${ROOT_DIR}/hack/prove-site-to-site-status-delivery.sh" \
  --sender-namespace "${NAMESPACE}" \
  --sender-release "${HELM_RELEASE}" \
  --sender-auth-secret nifi-auth \
  --sender-expected-destination-url "https://${RECEIVER_RELEASE}.${RECEIVER_NAMESPACE}.svc.cluster.local:8443/nifi" \
  --sender-expected-input-port "nifi-status" \
  --sender-expected-transport "HTTP" \
  --sender-expected-platform "nifi" \
  --sender-expected-auth-type "secretRef" \
  --sender-expected-authorized-identity "${SITE_TO_SITE_AUTHORIZED_IDENTITY}" \
  --sender-expected-auth-secret-ref-name "${SENDER_CLIENT_SECRET_NAME}" \
  --receiver-namespace "${RECEIVER_NAMESPACE}" \
  --receiver-release "${RECEIVER_RELEASE}" \
  --receiver-auth-secret site-to-site-receiver-auth \
  --receiver-expected-authorized-identity "${SITE_TO_SITE_AUTHORIZED_IDENTITY}" \
  --receiver-input-port "nifi-status"

print_success_footer "typed Site-to-Site status delivery proof completed" \
  "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
  "helm -n ${RECEIVER_NAMESPACE} status ${RECEIVER_RELEASE}" \
  "kubectl -n ${NAMESPACE} logs ${HELM_RELEASE}-0 -c nifi --tail=200" \
  "kubectl -n ${RECEIVER_NAMESPACE} logs ${RECEIVER_RELEASE}-0 -c nifi --tail=200" \
  "bash hack/prove-site-to-site-status-delivery.sh --sender-namespace ${NAMESPACE} --sender-release ${HELM_RELEASE} --sender-expected-destination-url https://${RECEIVER_RELEASE}.${RECEIVER_NAMESPACE}.svc.cluster.local:8443/nifi --sender-expected-input-port nifi-status --sender-expected-auth-type secretRef --sender-expected-authorized-identity '${SITE_TO_SITE_AUTHORIZED_IDENTITY}' --sender-expected-auth-secret-ref-name ${SENDER_CLIENT_SECRET_NAME} --receiver-namespace ${RECEIVER_NAMESPACE} --receiver-release ${RECEIVER_RELEASE} --receiver-expected-authorized-identity '${SITE_TO_SITE_AUTHORIZED_IDENTITY}' --receiver-input-port nifi-status"
