#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-metrics-exporter}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_METRICS_VALUES_FILE="${PLATFORM_METRICS_VALUES_FILE:-examples/platform-managed-metrics-exporter-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
PROM_CRDS_NAMESPACE="${PROM_CRDS_NAMESPACE:-monitoring}"
PROM_CRDS_RELEASE="${PROM_CRDS_RELEASE:-prometheus-operator-crds}"
METRICS_AUTH_SECRET="${METRICS_AUTH_SECRET:-nifi-metrics-auth}"
METRICS_CA_SECRET="${METRICS_CA_SECRET:-nifi-metrics-ca}"
PROBE_POD_NAME="${PROBE_POD_NAME:-metrics-exporter-probe}"
PROBE_IMAGE="${PROBE_IMAGE:-curlimages/curl:8.12.1}"
EXPORTER_DEPLOYMENT_NAME="${EXPORTER_DEPLOYMENT_NAME:-${HELM_RELEASE}-metrics-exporter}"
SOURCE_AUTH_SECRET="${SOURCE_AUTH_SECRET:-nifi-auth}"
CURRENT_PHASE="${CURRENT_PHASE:-bootstrap}"
FAILURE_CATEGORY="${FAILURE_CATEGORY:-unknown}"
FAILURE_ENDPOINT="${FAILURE_ENDPOINT:-}"
START_EPOCH="$(date +%s)"

TMPDIR_METRICS=""

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

wait_for_probe_ready() {
  kubectl -n "${NAMESPACE}" wait --for=condition=Ready pod/"${PROBE_POD_NAME}" --timeout=3m >/dev/null
}

wait_for_exporter_ready() {
  kubectl -n "${NAMESPACE}" rollout status deployment/"${EXPORTER_DEPLOYMENT_NAME}" --timeout=3m >/dev/null
}

exporter_pod_name() {
  kubectl -n "${NAMESPACE}" get pod -l app.kubernetes.io/component=metrics-exporter -o jsonpath='{.items[0].metadata.name}'
}

install_prometheus_operator_crds() {
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
  helm repo update prometheus-community >/dev/null
  helm upgrade --install "${PROM_CRDS_RELEASE}" prometheus-community/prometheus-operator-crds \
    --namespace "${PROM_CRDS_NAMESPACE}" \
    --create-namespace >/dev/null
  kubectl get crd servicemonitors.monitoring.coreos.com >/dev/null
}

create_metrics_secrets() {
  CURRENT_PHASE="bootstrap-metrics-secrets"
  FAILURE_CATEGORY="auth-material"
  FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET},Secret/${METRICS_CA_SECRET}"
  bash "${ROOT_DIR}/hack/bootstrap-metrics-machine-auth.sh" \
    --namespace "${NAMESPACE}" \
    --metrics-auth-secret "${METRICS_AUTH_SECRET}" \
    --metrics-ca-secret "${METRICS_CA_SECRET}" \
    --auth-mode authorizationHeader \
    --token bootstrap-pending >/dev/null
}

mint_metrics_token() {
  CURRENT_PHASE="mint-metrics-machine-auth"
  FAILURE_CATEGORY="auth-material"
  FAILURE_ENDPOINT="https://${HELM_RELEASE}-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local:8443/nifi-api/access/token"
  bash "${ROOT_DIR}/hack/bootstrap-metrics-machine-auth.sh" \
    --namespace "${NAMESPACE}" \
    --metrics-auth-secret "${METRICS_AUTH_SECRET}" \
    --metrics-ca-secret "${METRICS_CA_SECRET}" \
    --auth-mode authorizationHeader \
    --source-auth-secret "${SOURCE_AUTH_SECRET}" \
    --mint-token \
    --statefulset "${HELM_RELEASE}" \
    --container nifi >/dev/null
}

install_probe_pod() {
  kubectl -n "${NAMESPACE}" delete pod "${PROBE_POD_NAME}" --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n "${NAMESPACE}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${PROBE_POD_NAME}
spec:
  restartPolicy: Never
  containers:
  - name: curl
    image: ${PROBE_IMAGE}
    imagePullPolicy: IfNotPresent
    command:
    - /bin/sh
    - -ec
    - sleep 3600
EOF
  wait_for_probe_ready
}

assert_equals() {
  local actual="$1"
  local expected="$2"
  local message="$3"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "${message}: expected ${expected}, got ${actual:-<empty>}" >&2
    return 1
  fi
}

verify_metrics_secret_contract() {
  local token_preview
  local ca_present

  token_preview="$(kubectl -n "${NAMESPACE}" get secret "${METRICS_AUTH_SECRET}" -o jsonpath='{.data.token}' | base64 --decode | cut -c1-18)"
  ca_present="$(kubectl -n "${NAMESPACE}" get secret "${METRICS_CA_SECRET}" -o jsonpath='{.data.ca\.crt}' | tr -d '\n')"

  if [[ -z "${token_preview}" || "${token_preview}" == "bootstrap-pending" ]]; then
    echo "metrics auth Secret ${METRICS_AUTH_SECRET} was not updated with a live token" >&2
    return 1
  fi
  if [[ -z "${ca_present}" ]]; then
    echo "metrics CA Secret ${METRICS_CA_SECRET} does not contain ca.crt" >&2
    return 1
  fi
}

verify_exporter_resources() {
  local auth_secret
  local ca_secret
  local auth_type
  local auth_header_type
  local source_host
  local source_path
  local flow_status_enabled
  local flow_status_path
  local service_monitor_path
  local service_monitor_scheme

  kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" >/dev/null
  kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" >/dev/null
  kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" >/dev/null

  auth_secret="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.volumes[?(@.name=="exporter-auth")].secret.secretName}')"
  ca_secret="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.volumes[?(@.name=="exporter-ca")].secret.secretName}')"
  auth_type="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_AUTH_TYPE")].value}')"
  auth_header_type="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_AUTH_HEADER_TYPE")].value}')"
  source_host="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_SOURCE_HOST")].value}')"
  source_path="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_SOURCE_PATH")].value}')"
  flow_status_enabled="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_FLOW_STATUS_ENABLED")].value}')"
  flow_status_path="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_FLOW_STATUS_PATH")].value}')"
  service_monitor_path="$(kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o jsonpath='{.spec.endpoints[0].path}')"
  service_monitor_scheme="$(kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o jsonpath='{.spec.endpoints[0].scheme}')"

  assert_equals "${auth_secret}" "${METRICS_AUTH_SECRET}" "exporter auth Secret mismatch"
  assert_equals "${ca_secret}" "${METRICS_CA_SECRET}" "exporter CA Secret mismatch"
  assert_equals "${auth_type}" "authorizationHeader" "exporter auth type mismatch"
  assert_equals "${auth_header_type}" "Bearer" "exporter header type mismatch"
  assert_equals "${source_host}" "${HELM_RELEASE}.${NAMESPACE}.svc.cluster.local" "exporter source host mismatch"
  assert_equals "${source_path}" "/nifi-api/flow/metrics/prometheus" "exporter source path mismatch"
  assert_equals "${flow_status_enabled}" "true" "exporter flow-status supplement flag mismatch"
  assert_equals "${flow_status_path}" "/nifi-api/flow/status" "exporter flow-status path mismatch"
  assert_equals "${service_monitor_path}" "/metrics" "exporter ServiceMonitor path mismatch"
  assert_equals "${service_monitor_scheme}" "http" "exporter ServiceMonitor scheme mismatch"
}

verify_exporter_mounts() {
  local exporter_pod

  exporter_pod="$(exporter_pod_name)"
  [[ -n "${exporter_pod}" ]]

  kubectl -n "${NAMESPACE}" exec "${exporter_pod}" -- /bin/sh -ec '
    test -f /exporter/exporter.py
    test -f /var/run/nifi-metrics-auth/token
    test -f /var/run/nifi-metrics-ca/ca.crt
  ' >/dev/null
}

probe_exporter_endpoint() {
  local path="$1"
  local expected_status="$2"
  local attempts="${3:-12}"
  local sleep_seconds="${4:-5}"
  local http_code
  local attempt

  for attempt in $(seq 1 "${attempts}"); do
    http_code="$(
      kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec "
        curl --silent --show-error \
          --output /dev/null \
          --write-out '%{http_code}' \
          http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090${path}
      "
    )"
    if [[ "${http_code}" == "${expected_status}" ]]; then
      return 0
    fi
    if (( attempt == attempts )); then
      kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec "
        curl --silent --show-error http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090${path}
      " >&2 || true
      echo "exporter ${path} never returned HTTP ${expected_status}: final HTTP status ${http_code:-<empty>}" >&2
      return 1
    fi
    sleep "${sleep_seconds}"
  done
}

set_metrics_token_literal() {
  local token_value="$1"

  kubectl -n "${NAMESPACE}" create secret generic "${METRICS_AUTH_SECRET}" \
    --from-literal=token="${token_value}" \
    --dry-run=client \
    -o yaml | kubectl -n "${NAMESPACE}" apply -f - >/dev/null
}

wait_for_exporter_auth_file() {
  local comparison="$1"
  local expected_value="$2"
  local attempts="${3:-24}"
  local sleep_seconds="${4:-5}"
  local exporter_pod
  local current_value
  local attempt

  exporter_pod="$(exporter_pod_name)"
  [[ -n "${exporter_pod}" ]]

  for attempt in $(seq 1 "${attempts}"); do
    current_value="$(kubectl -n "${NAMESPACE}" exec "${exporter_pod}" -- /bin/sh -ec 'cat /var/run/nifi-metrics-auth/token' 2>/dev/null || true)"
    if [[ "${comparison}" == "equals" && "${current_value}" == "${expected_value}" ]]; then
      return 0
    fi
    if [[ "${comparison}" == "not-equals" && "${current_value}" != "${expected_value}" && -n "${current_value}" ]]; then
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  echo "exporter auth file never reached expected state (${comparison} ${expected_value})" >&2
  return 1
}

scrape_exporter_metrics() {
  local attempt
  local max_attempts=12
  local http_code
  local body
  CURRENT_PHASE="scrape-exporter-metrics"
  FAILURE_CATEGORY="scrape"
  FAILURE_ENDPOINT="http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090/metrics"
  for attempt in $(seq 1 "${max_attempts}"); do
    http_code="$(
      kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec "
        curl --silent --show-error \
          --output /dev/null \
          --write-out '%{http_code}' \
          http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090/metrics
      "
    )"
    if [[ "${http_code}" == "200" ]]; then
      body="$(
        kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec "
          curl --silent --show-error http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090/metrics
        "
      )"
      if printf '%s\n' "${body}" | grep -q '^# HELP ' &&
        printf '%s\n' "${body}" | grep -q '^# TYPE ' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_exporter_source_up{source=\"flow_prometheus\"} 1$' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_exporter_source_up{source=\"flow_status\"} 1$' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_flow_status_controller_active_thread_count ' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_flow_status_controller_bytes_queued '; then
        return 0
      fi
    fi
    if (( attempt == max_attempts )); then
      kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec "
        curl --silent --show-error http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090/metrics
      " >&2 || true
      echo "exporter scrape never succeeded: final HTTP status ${http_code:-<empty>}" >&2
      return 1
    fi
    sleep 5
  done
}

restart_exporter_deployment() {
  kubectl -n "${NAMESPACE}" rollout restart deployment/"${EXPORTER_DEPLOYMENT_NAME}" >/dev/null
  wait_for_exporter_ready
}

verify_exporter_secret_reload_without_restart() {
  local exporter_pod_before
  local exporter_uid_before
  local exporter_pod_after
  local exporter_uid_after
  local invalid_token="invalid-exporter-token"

  exporter_pod_before="$(exporter_pod_name)"
  exporter_uid_before="$(kubectl -n "${NAMESPACE}" get pod "${exporter_pod_before}" -o jsonpath='{.metadata.uid}')"

  set_metrics_token_literal "${invalid_token}"
  wait_for_exporter_auth_file "equals" "${invalid_token}"

  probe_exporter_endpoint "/readyz" "503" 18 5
  probe_exporter_endpoint "/metrics" "502" 18 5

  mint_metrics_token
  wait_for_exporter_auth_file "not-equals" "${invalid_token}"
  wait_for_exporter_ready
  probe_exporter_endpoint "/readyz" "200" 18 5
  scrape_exporter_metrics

  exporter_pod_after="$(exporter_pod_name)"
  exporter_uid_after="$(kubectl -n "${NAMESPACE}" get pod "${exporter_pod_after}" -o jsonpath='{.metadata.uid}')"
  assert_equals "${exporter_uid_after}" "${exporter_uid_before}" "exporter pod restarted during auth Secret rotation proof"
}

dump_diagnostics() {
  set +e
  echo
  echo "==> metrics exporter diagnostics after failure at +$(elapsed)s"
  echo "  mode: exporter"
  echo "  failed phase: ${CURRENT_PHASE}"
  echo "  failure category: ${FAILURE_CATEGORY}"
  echo "  endpoint or contract: ${FAILURE_ENDPOINT:-n/a}"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl get crd servicemonitors.monitoring.coreos.com -o name || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,deployment,service,servicemonitor,pod,secret,configmap || true
  kubectl -n "${NAMESPACE}" describe deployment "${EXPORTER_DEPLOYMENT_NAME}" || true
  kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o yaml || true
  kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o yaml || true
  kubectl -n "${NAMESPACE}" get configmap "${EXPORTER_DEPLOYMENT_NAME}-config" -o yaml || true
  kubectl -n "${NAMESPACE}" logs deployment/"${EXPORTER_DEPLOYMENT_NAME}" --tail=200 || true
  kubectl -n "${NAMESPACE}" logs "${PROBE_POD_NAME}" || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
}

cleanup() {
  if [[ -n "${TMPDIR_METRICS}" && -d "${TMPDIR_METRICS}" ]]; then
    rm -rf "${TMPDIR_METRICS}"
  fi
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR
trap cleanup EXIT

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
  -f "${ROOT_DIR}/${PLATFORM_METRICS_VALUES_FILE}"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
check_prereqs

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for exporter metrics runtime proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for exporter metrics runtime proof"
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

CURRENT_PHASE="install-prometheus-operator-crds"
FAILURE_CATEGORY="rendering"
FAILURE_ENDPOINT="ServiceMonitor CRD"
phase "Installing Prometheus Operator CRDs for ServiceMonitor acceptance"
install_prometheus_operator_crds

CURRENT_PHASE="create-bootstrap-metrics-secrets"
FAILURE_CATEGORY="auth-material"
FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET},Secret/${METRICS_CA_SECRET}"
phase "Creating operator-provided metrics Secrets"
create_metrics_secrets

CURRENT_PHASE="install-platform-chart"
FAILURE_CATEGORY="rendering"
FAILURE_ENDPOINT="exporter metrics Deployment, Service, and ServiceMonitor resources"
phase "Installing product chart managed release${profile_label}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

CURRENT_PHASE="verify-platform-install"
FAILURE_CATEGORY="rendering"
FAILURE_ENDPOINT="managed platform install"
phase "Verifying platform resources and controller rollout"
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

CURRENT_PHASE="verify-cluster-health"
FAILURE_CATEGORY="scrape"
FAILURE_ENDPOINT="secured NiFi API readiness"
phase "Verifying secured NiFi cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

CURRENT_PHASE="mint-machine-auth"
FAILURE_CATEGORY="auth-material"
FAILURE_ENDPOINT="NiFi access token bootstrap"
phase "Minting a bearer token for the operator-provided metrics Secret"
mint_metrics_token

CURRENT_PHASE="wait-for-exporter-readiness"
FAILURE_CATEGORY="scrape"
FAILURE_ENDPOINT="/readyz"
phase "Waiting for exporter readiness against the secured upstream NiFi metrics endpoint"
wait_for_exporter_ready

CURRENT_PHASE="verify-exporter-contract"
FAILURE_CATEGORY="rendering"
FAILURE_ENDPOINT="Deployment/${EXPORTER_DEPLOYMENT_NAME},Service/${HELM_RELEASE}-metrics,ServiceMonitor/${HELM_RELEASE}-exporter"
phase "Verifying exporter deployment, Service, and ServiceMonitor wiring"
verify_metrics_secret_contract
verify_exporter_resources
verify_exporter_mounts

CURRENT_PHASE="probe-exporter-metrics"
FAILURE_CATEGORY="scrape"
FAILURE_ENDPOINT="/metrics"
phase "Probing the clean exporter /metrics endpoint"
install_probe_pod
probe_exporter_endpoint "/readyz" "200"
scrape_exporter_metrics

CURRENT_PHASE="verify-exporter-secret-rotation"
FAILURE_CATEGORY="auth-material"
FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET} mounted into ${EXPORTER_DEPLOYMENT_NAME}"
phase "Proving exporter recovery after machine-auth Secret rotation without restarting the pod"
verify_exporter_secret_reload_without_restart

print_success_footer "exporter metrics runtime proof completed" \
  "make kind-metrics-exporter-fast-e2e-reuse" \
  "kubectl -n ${NAMESPACE} get deployment ${EXPORTER_DEPLOYMENT_NAME} -o yaml" \
  "kubectl -n ${NAMESPACE} get service ${HELM_RELEASE}-metrics -o yaml" \
  "kubectl -n ${NAMESPACE} get servicemonitor ${HELM_RELEASE}-exporter -o yaml" \
  "kubectl -n ${NAMESPACE} exec ${PROBE_POD_NAME} -- sh -ec 'curl --silent --show-error --fail http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090/metrics | head'" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml"
