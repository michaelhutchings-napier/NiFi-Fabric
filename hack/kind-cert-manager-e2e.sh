#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi2-platform-cert-manager}"
NAMESPACE="${NAMESPACE:-nifi}"
SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-nifi-system}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
TLS_SECRET_NAME="${TLS_SECRET_NAME:-nifi-tls}"
TLS_PARAMS_SECRET_NAME="${TLS_PARAMS_SECRET_NAME:-nifi-tls-params}"
CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE:-cert-manager}"
CERTIFICATE_NAME="${CERTIFICATE_NAME:-nifi}"
CONTROLLER_IMAGE="${CONTROLLER_IMAGE:-nifi2-platform-controller:dev}"
LOCALBIN="${LOCALBIN:-${ROOT_DIR}/bin}"
CMCTL_BIN="${CMCTL_BIN:-${LOCALBIN}/cmctl}"
CMCTL_DOWNLOAD_URL="${CMCTL_DOWNLOAD_URL:-}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
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

fail() {
  echo "FAIL: $*" >&2
  dump_diagnostics
  exit 1
}

trap 'fail "cert-manager e2e aborted"' ERR

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
      fail "${description} timed out"
    fi
    sleep 5
  done
}

run_make() {
  (cd "${ROOT_DIR}" && KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" make "$@")
}

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

cluster_jsonpath() {
  local path="$1"
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o "jsonpath=${path}"
}

pod_uid_snapshot() {
  kubectl -n "${NAMESPACE}" get pods \
    -o jsonpath='{range .items[*]}{.metadata.name}={.metadata.uid}{"\n"}{end}' | sort
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

assert_all_pods_changed() {
  local before="$1"
  local description="$2"
  local after
  after="$(pod_uid_snapshot)"
  while IFS='=' read -r pod uid_before; do
    [[ -z "${pod}" ]] && continue
    local uid_after
    uid_after="$(printf '%s\n' "${after}" | awk -F= -v pod="${pod}" '$1 == pod { print $2 }')"
    if [[ -z "${uid_after}" ]]; then
      fail "${description} is missing pod ${pod}"
    fi
    if [[ "${uid_before}" == "${uid_after}" ]]; then
      fail "${description} did not recreate pod ${pod}"
    fi
  done <<< "${before}"
}

ensure_cmctl() {
  if command -v cmctl >/dev/null 2>&1; then
    CMCTL_BIN="$(command -v cmctl)"
    return 0
  fi

  mkdir -p "${LOCALBIN}"
  local os arch url
  os="$(go env GOOS)"
  arch="$(go env GOARCH)"
  url="${CMCTL_DOWNLOAD_URL:-https://github.com/cert-manager/cmctl/releases/latest/download/cmctl_${os}_${arch}}"
  curl -fsSL -o "${CMCTL_BIN}" "${url}"
  chmod +x "${CMCTL_BIN}"
}

wait_for_certificate_secret() {
  wait_for "Certificate ${CERTIFICATE_NAME} to become ready" 600 \
    kubectl wait -n "${NAMESPACE}" certificate/"${CERTIFICATE_NAME}" --for=condition=Ready=True --timeout=5s

  wait_for "TLS Secret ${TLS_SECRET_NAME} to contain PKCS12 data" 600 bash -ec '
    secret_json="$(kubectl -n "'"${NAMESPACE}"'" get secret "'"${TLS_SECRET_NAME}"'" -o json)"
    jq -e ".data[\"keystore.p12\"] and .data[\"truststore.p12\"] and .data[\"ca.crt\"]" <<<"${secret_json}" >/dev/null
  '
}

wait_for_secret_hash_change() {
  local previous_hash="$1"
  wait_for "TLS Secret material to change" 600 bash -ec '
    current_hash="$(kubectl -n "'"${NAMESPACE}"'" get secret "'"${TLS_SECRET_NAME}"'" -o json | jq -r ".data[\"keystore.p12\"] + \":\" + .data[\"truststore.p12\"]" | sha256sum | awk "{print \$1}")"
    [[ "${current_hash}" != "'"${previous_hash}"'" ]]
  '
}

wait_for_tls_observed_hash_change() {
  local previous_hash="$1"
  wait_for "controller TLS observation to settle without rollout" 900 bash -ec '
    current_hash="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.observedCertificateHash}")"
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    reason="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.conditions[?(@.type==\"Progressing\")].reason}")"
    [[ -n "${current_hash}" && "${current_hash}" != "'"${previous_hash}"'" && -z "${trigger}" && "${reason}" != "TLSAutoreloadObserving" ]]
  '
}

wait_for_rollout_clear() {
  wait_for "TLS rollout to settle" 1200 bash -ec '
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    phase="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.lastOperation.phase}")"
    [[ -z "${trigger}" && "${phase}" == "Succeeded" ]]
  '
}

wait_for_tls_rollout_observed() {
  wait_for "TLS rollout to be observed" 300 bash -ec '
    trigger="$(kubectl -n "'"${NAMESPACE}"'" get nificluster "'"${HELM_RELEASE}"'" -o jsonpath="{.status.rollout.trigger}")"
    [[ "${trigger}" == "TLSDrift" ]]
  '
}

dump_diagnostics() {
  set +e
  echo
  echo "==> cert-manager diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl get ns || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" get deployment,pod,issuer,certificate,certificaterequest,clusterissuer || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,secret,certificate,certificaterequest || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" describe issuer nifi-selfsigned-bootstrap || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" describe certificate nifi-root-ca || true
  kubectl describe clusterissuer nifi-ca || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}" || true
  kubectl -n "${NAMESPACE}" get certificate "${CERTIFICATE_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" describe certificate "${CERTIFICATE_NAME}" || true
  kubectl -n "${NAMESPACE}" get secret "${TLS_SECRET_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}{"\n"}{.status.readyReplicas}{"\n"}{.status.currentRevision}{"\n"}{.status.updateRevision}{"\n"}' || true
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CERT_MANAGER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi2-platform-controller-manager --tail=300 || true

  capture_cmd cert-manager-workloads kubectl -n "${CERT_MANAGER_NAMESPACE}" get deployment,pod,issuer,certificate,certificaterequest,clusterissuer -o wide
  capture_cmd nificluster-yaml kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml
  capture_cmd nificluster-status kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
  capture_cmd certificate-yaml kubectl -n "${NAMESPACE}" get certificate "${CERTIFICATE_NAME}" -o yaml
  capture_cmd tls-secret kubectl -n "${NAMESPACE}" get secret "${TLS_SECRET_NAME}" -o yaml
  capture_cmd statefulset-status kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}{"\n"}{.status.readyReplicas}{"\n"}{.status.currentRevision}{"\n"}{.status.updateRevision}{"\n"}'
  capture_cmd pods-summary kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp
  capture_cmd nifi-events bash -lc "kubectl -n '${NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 200"
  capture_cmd cert-manager-events bash -lc "kubectl -n '${CERT_MANAGER_NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 200"
  capture_cmd controller-logs kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi2-platform-controller-manager --tail=500
  capture_cmd controller-metrics bash -lc "kubectl -n '${SYSTEM_NAMESPACE}' port-forward deployment/nifi2-platform-controller-manager 18080:8080 >/tmp/nifi-cert-manager-metrics.log 2>&1 & pf=\$!; sleep 5; curl --silent --show-error --fail http://127.0.0.1:18080/metrics || true; kill \$pf >/dev/null 2>&1 || true; wait \$pf >/dev/null 2>&1 || true"
}

log_step "creating a fresh kind cluster for cert-manager evaluation"
require_command kind
require_command kubectl
require_command helm
require_command docker
require_command curl
require_command jq
require_command go
run_make kind-down || true
run_make kind-up

log_step "preloading the NiFi runtime image into kind"
run_make kind-load-nifi-image

log_step "installing cert-manager"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" \
CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE}" \
bash "${ROOT_DIR}/hack/kind-bootstrap-cert-manager.sh"

log_step "creating auth and TLS parameter Secrets"
bash "${ROOT_DIR}/hack/create-kind-cert-manager-secrets.sh" "${NAMESPACE}" nifi-auth "${TLS_PARAMS_SECRET_NAME}"

log_step "installing the controller and CRD"
run_make install-crd
run_make docker-build-controller
run_make kind-load-controller
run_make deploy-controller
kubectl -n "${SYSTEM_NAMESPACE}" rollout status deployment/nifi2-platform-controller-manager --timeout=5m

log_step "installing the managed chart with cert-manager TLS"
(cd "${ROOT_DIR}" && helm upgrade --install "${HELM_RELEASE}" charts/nifi \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  -f examples/managed/values.yaml \
  -f examples/cert-manager-values.yaml)
kubectl apply -f "${ROOT_DIR}/examples/managed/nificluster.yaml"

log_step "verifying Certificate readiness and TLS Secret contents"
wait_for_certificate_secret

log_step "verifying NiFi health in cert-manager mode"
(cd "${ROOT_DIR}" && KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" make kind-health)

log_step "exercising cert-manager renewal with unchanged TLS wiring"
ensure_cmctl
tls_secret_hash_before="$(kubectl -n "${NAMESPACE}" get secret "${TLS_SECRET_NAME}" -o json | jq -r '.data["keystore.p12"] + ":" + .data["truststore.p12"]' | sha256sum | awk '{print $1}')"
observed_certificate_hash_before="$(cluster_jsonpath '{.status.observedCertificateHash}')"
pod_uids_before_renewal="$(pod_uid_snapshot)"
"${CMCTL_BIN}" renew --namespace "${NAMESPACE}" "${CERTIFICATE_NAME}"
wait_for_secret_hash_change "${tls_secret_hash_before}"
wait_for_tls_observed_hash_change "${observed_certificate_hash_before}"
assert_pods_unchanged "${pod_uids_before_renewal}" "cert-manager renewal"
(cd "${ROOT_DIR}" && KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" make kind-health)

log_step "exercising restart-required TLS config drift with cert-manager TLS"
pod_uids_before_rollout="$(pod_uid_snapshot)"
(cd "${ROOT_DIR}" && helm upgrade --install "${HELM_RELEASE}" charts/nifi \
  --namespace "${NAMESPACE}" \
  -f examples/managed/values.yaml \
  -f examples/cert-manager-values.yaml \
  --reuse-values \
  --set tls.mountPath=/opt/nifi/tls-alt)
wait_for_tls_rollout_observed
wait_for_rollout_clear
assert_all_pods_changed "${pod_uids_before_rollout}" "restart-required TLS rollout"
(cd "${ROOT_DIR}" && KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" make kind-health)

echo
echo "PASS: focused cert-manager workflow completed successfully in +$(elapsed)s"
echo "  cert-manager install"
echo "  managed chart with cert-manager Certificate"
echo "  Certificate and Secret readiness"
echo "  NiFi health"
echo "  content-only renewal without restart"
echo "  restart-required TLS rollout"
