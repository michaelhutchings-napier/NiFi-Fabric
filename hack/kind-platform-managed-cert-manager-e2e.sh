#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-platform-managed-cert-manager}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE:-cert-manager}"
TLS_SECRET_NAME="${TLS_SECRET_NAME:-nifi-tls}"
TLS_PARAMS_SECRET_NAME="${TLS_PARAMS_SECRET_NAME:-nifi-tls-params}"
CERTIFICATE_NAME="${CERTIFICATE_NAME:-nifi}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-cert-manager-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
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

wait_for_certificate_ready() {
  kubectl wait -n "${NAMESPACE}" certificate/"${CERTIFICATE_NAME}" --for=condition=Ready=True --timeout=10m
}

wait_for_secret_ready() {
  local deadline=$(( $(date +%s) + 600 ))
  while true; do
    if kubectl -n "${NAMESPACE}" get secret "${TLS_SECRET_NAME}" -o jsonpath='{.data.keystore\.p12}{.data.truststore\.p12}{.data.ca\.crt}' | grep -q .; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for TLS Secret ${TLS_SECRET_NAME} to contain PKCS12 data" >&2
      return 1
    fi
    sleep 5
  done
}

dump_diagnostics() {
  set +e
  echo
  echo "==> platform managed-cert-manager diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl get crd nificlusters.platform.nifi.io -o yaml || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" get deployment,pod,issuer,certificate,certificaterequest,clusterissuer || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,secret,certificate,certificaterequest || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}" || true
  kubectl -n "${NAMESPACE}" get certificate "${CERTIFICATE_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" describe certificate "${CERTIFICATE_NAME}" || true
  kubectl -n "${NAMESPACE}" get secret "${TLS_SECRET_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}{"\n"}{.status.readyReplicas}{"\n"}{.status.currentRevision}{"\n"}{.status.updateRevision}{"\n"}' || true
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" logs deployment/cert-manager --tail=200 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
check_prereqs

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for platform managed-cert-manager runtime proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for platform managed-cert-manager runtime proof"
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
  configure_kind_kubeconfig

  phase "Loading NiFi image into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Verifying the platform chart fails clearly before cert-manager is present"
  prerequisite_output="$(mktemp)"
  if helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
    --namespace "${NAMESPACE}" \
    --create-namespace \
    "${helm_values_args[@]}" >"${prerequisite_output}" 2>&1; then
    cat "${prerequisite_output}" >&2
    rm -f "${prerequisite_output}"
    echo "expected managed-cert-manager install to fail before cert-manager bootstrap" >&2
    exit 1
  fi
  if ! grep -Eq 'no matches for kind "Certificate"|cert-manager.io/v1|resource mapping not found' "${prerequisite_output}"; then
    cat "${prerequisite_output}" >&2
    rm -f "${prerequisite_output}"
    echo "managed-cert-manager prerequisite failure was not clear enough" >&2
    exit 1
  fi
  rm -f "${prerequisite_output}"

  phase "Bootstrapping cert-manager and issuer flow"
  KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE}" \
    bash "${ROOT_DIR}/hack/kind-bootstrap-cert-manager.sh"

  phase "Creating auth and TLS parameter Secrets"
  bash "${ROOT_DIR}/hack/create-kind-cert-manager-secrets.sh" "${NAMESPACE}" nifi-auth "${TLS_PARAMS_SECRET_NAME}"

  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
fi

phase "Installing product chart managed-cert-manager release${profile_label}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

phase "Verifying platform resources, cert-manager outputs, and controller rollout"
kubectl get crd nificlusters.platform.nifi.io >/dev/null
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null
wait_for_certificate_ready
wait_for_secret_ready

phase "Verifying cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved
wait_for_condition_true Available

print_success_footer "platform chart managed-cert-manager runtime proof completed" \
  "make kind-health KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}" \
  "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
  "kubectl -n ${NAMESPACE} get certificate,secret" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
