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
EXPORTER_IMAGE_REPOSITORY="${EXPORTER_IMAGE_REPOSITORY:-${NIFI_IMAGE%:*}}"
EXPORTER_IMAGE_TAG="${EXPORTER_IMAGE_TAG:-${NIFI_IMAGE##*:}}"
EXPORTER_SOURCE_HOST_OVERRIDE="${EXPORTER_SOURCE_HOST_OVERRIDE:-${HELM_RELEASE}-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_METRICS_VALUES_FILE="${PLATFORM_METRICS_VALUES_FILE:-examples/platform-managed-metrics-exporter-values.yaml}"
PLATFORM_TRUST_MANAGER_VALUES_FILE="${PLATFORM_TRUST_MANAGER_VALUES_FILE:-examples/platform-managed-trust-manager-values.yaml}"
PLATFORM_TRUST_MANAGER_METRICS_VALUES_FILE="${PLATFORM_TRUST_MANAGER_METRICS_VALUES_FILE:-examples/platform-managed-metrics-exporter-trust-manager-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
PROM_CRDS_NAMESPACE="${PROM_CRDS_NAMESPACE:-monitoring}"
PROM_CRDS_RELEASE="${PROM_CRDS_RELEASE:-prometheus-operator-crds}"
METRICS_AUTH_SECRET="${METRICS_AUTH_SECRET:-nifi-metrics-auth}"
METRICS_CA_SECRET="${METRICS_CA_SECRET:-nifi-metrics-ca}"
TRUST_MANAGER_ENABLED="${TRUST_MANAGER_ENABLED:-false}"
TRUST_MANAGER_NAMESPACE="${TRUST_MANAGER_NAMESPACE:-cert-manager}"
TRUST_MANAGER_RELEASE="${TRUST_MANAGER_RELEASE:-trust-manager}"
TRUST_MANAGER_DEPLOYMENT="${TRUST_MANAGER_DEPLOYMENT:-trust-manager}"
TRUST_BUNDLE_NAME="${TRUST_BUNDLE_NAME:-${HELM_RELEASE}-trust-bundle}"
TRUST_SOURCE_SECRET_NAME="${TRUST_SOURCE_SECRET_NAME:-${HELM_RELEASE}-tls-ca-source}"
TRUST_MANAGER_BUNDLE_TARGET_TYPE="${TRUST_MANAGER_BUNDLE_TARGET_TYPE:-secret}"
TRUST_MANAGER_BUNDLE_KEY="${TRUST_MANAGER_BUNDLE_KEY:-ca.crt}"
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

install_trust_manager() {
  run_make kind-bootstrap-cert-manager KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
  helm repo add jetstack https://charts.jetstack.io >/dev/null 2>&1 || true
  helm repo update jetstack >/dev/null
  helm upgrade --install "${TRUST_MANAGER_RELEASE}" jetstack/trust-manager \
    --namespace "${TRUST_MANAGER_NAMESPACE}" \
    --create-namespace \
    --wait \
    --timeout 5m \
    --set secretTargets.enabled=true \
    --set-json "secretTargets.authorizedSecrets=[\"${TRUST_BUNDLE_NAME}\"]" >/dev/null
  kubectl -n "${TRUST_MANAGER_NAMESPACE}" rollout status deployment/"${TRUST_MANAGER_DEPLOYMENT}" --timeout=5m >/dev/null
}

bundle_ca_name() {
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    printf '%s' "${TRUST_BUNDLE_NAME}"
  else
    printf '%s' "${METRICS_CA_SECRET}"
  fi
}

bundle_ca_key() {
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    printf '%s' "${TRUST_MANAGER_BUNDLE_KEY}"
  else
    printf 'ca.crt'
  fi
}

bundle_ca_type() {
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    printf '%s' "${TRUST_MANAGER_BUNDLE_TARGET_TYPE}"
  else
    printf 'secret'
  fi
}

read_bundle_data() {
  local name
  name="$(bundle_ca_name)"
  local key
  key="$(bundle_ca_key)"

  if [[ "$(bundle_ca_type)" == "secret" ]]; then
    kubectl -n "${NAMESPACE}" get secret "${name}" -o json | jq -r --arg key "${key}" '.data[$key] // empty'
  else
    kubectl -n "${NAMESPACE}" get configmap "${name}" -o json | jq -r --arg key "${key}" '.data[$key] // empty'
  fi
}

wait_for_bundle_resource() {
  local deadline=$(( $(date +%s) + 240 ))

  while true; do
    if read_bundle_data 2>/dev/null | grep -q .; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "expected trust bundle $(bundle_ca_name) key $(bundle_ca_key) to be populated" >&2
      return 1
    fi
    sleep 5
  done
}

create_metrics_secrets() {
  CURRENT_PHASE="bootstrap-metrics-secrets"
  FAILURE_CATEGORY="auth-material"
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET}"
    bash "${ROOT_DIR}/hack/bootstrap-metrics-machine-auth.sh" \
      --namespace "${NAMESPACE}" \
      --metrics-auth-secret "${METRICS_AUTH_SECRET}" \
      --auth-mode authorizationHeader \
      --token bootstrap-pending \
      --no-ca-secret >/dev/null
  else
    FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET},Secret/${METRICS_CA_SECRET}"
    bash "${ROOT_DIR}/hack/bootstrap-metrics-machine-auth.sh" \
      --namespace "${NAMESPACE}" \
      --metrics-auth-secret "${METRICS_AUTH_SECRET}" \
      --metrics-ca-secret "${METRICS_CA_SECRET}" \
      --auth-mode authorizationHeader \
      --token bootstrap-pending >/dev/null
  fi
}

mint_metrics_token() {
  CURRENT_PHASE="mint-metrics-machine-auth"
  FAILURE_CATEGORY="auth-material"
  FAILURE_ENDPOINT="https://${HELM_RELEASE}-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local:8443/nifi-api/access/token"
  local mint_args=(
    --namespace "${NAMESPACE}"
    --metrics-auth-secret "${METRICS_AUTH_SECRET}"
    --auth-mode authorizationHeader
    --source-auth-secret "${SOURCE_AUTH_SECRET}"
    --mint-token
    --statefulset "${HELM_RELEASE}"
    --container nifi
  )
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    mint_args+=(--no-ca-secret)
  else
    mint_args+=(--metrics-ca-secret "${METRICS_CA_SECRET}")
  fi
  bash "${ROOT_DIR}/hack/bootstrap-metrics-machine-auth.sh" "${mint_args[@]}" >/dev/null
}

install_probe_pod() {
  kubectl -n "${NAMESPACE}" delete pod "${PROBE_POD_NAME}" --ignore-not-found >/dev/null 2>&1 || true
  local ca_name
  ca_name="$(bundle_ca_name)"
  local ca_key
  ca_key="$(bundle_ca_key)"
  local volume_kind
  volume_kind="$(bundle_ca_type)"
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
    volumeMounts:
    - name: metrics-ca
      mountPath: /etc/nifi-metrics-ca
      readOnly: true
  volumes:
  - name: metrics-ca
$(if [[ "${volume_kind}" == "secret" ]]; then cat <<INNER
    secret:
      secretName: ${ca_name}
      items:
      - key: ${ca_key}
        path: ${ca_key}
INNER
else cat <<INNER
    configMap:
      name: ${ca_name}
      items:
      - key: ${ca_key}
        path: ${ca_key}
INNER
fi)
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
  ca_present="$(read_bundle_data | tr -d '\n')"

  if [[ -z "${token_preview}" || "${token_preview}" == "bootstrap-pending" ]]; then
    echo "metrics auth Secret ${METRICS_AUTH_SECRET} was not updated with a live token" >&2
    return 1
  fi
  if [[ -z "${ca_present}" ]]; then
    echo "metrics CA bundle $(bundle_ca_name) does not contain $(bundle_ca_key)" >&2
    return 1
  fi
}

verify_trust_manager_bundle_contract() {
  kubectl get bundle "${TRUST_BUNDLE_NAME}" >/dev/null
  kubectl -n "${TRUST_MANAGER_NAMESPACE}" get secret "${TRUST_SOURCE_SECRET_NAME}" >/dev/null
  wait_for_bundle_resource
}

verify_exporter_resources() {
  local auth_secret
  local ca_secret
  local ca_configmap
  local ca_mount_type
  local auth_type
  local auth_header_type
  local source_host
  local source_scheme
  local source_port
  local source_path
  local flow_status_enabled
  local flow_status_path
  local service_port
  local service_target_port
  local service_selector_component
  local service_component_label
  local service_monitor_path
  local service_monitor_port
  local service_monitor_scheme
  local service_monitor_selector_component
  local endpoints_ready

  kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" >/dev/null
  kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" >/dev/null
  kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" >/dev/null

  auth_secret="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.volumes[?(@.name=="exporter-auth")].secret.secretName}')"
  ca_secret="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.volumes[?(@.name=="exporter-ca")].secret.secretName}')"
  ca_configmap="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.volumes[?(@.name=="exporter-ca")].configMap.name}')"
  auth_type="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_AUTH_TYPE")].value}')"
  auth_header_type="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_AUTH_HEADER_TYPE")].value}')"
  source_host="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_SOURCE_HOST")].value}')"
  source_scheme="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_SOURCE_SCHEME")].value}')"
  source_port="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_SOURCE_PORT")].value}')"
  source_path="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_SOURCE_PATH")].value}')"
  flow_status_enabled="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_FLOW_STATUS_ENABLED")].value}')"
  flow_status_path="$(kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="EXPORTER_FLOW_STATUS_PATH")].value}')"
  service_port="$(kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o jsonpath='{.spec.ports[0].port}')"
  service_target_port="$(kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o jsonpath='{.spec.ports[0].targetPort}')"
  service_selector_component="$(kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o jsonpath='{.spec.selector.app\.kubernetes\.io/component}')"
  service_component_label="$(kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/component}')"
  service_monitor_path="$(kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o jsonpath='{.spec.endpoints[0].path}')"
  service_monitor_port="$(kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o jsonpath='{.spec.endpoints[0].port}')"
  service_monitor_scheme="$(kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o jsonpath='{.spec.endpoints[0].scheme}')"
  service_monitor_selector_component="$(kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o jsonpath='{.spec.selector.matchLabels.app\.kubernetes\.io/component}')"
  endpoints_ready="$(
    kubectl -n "${NAMESPACE}" get endpointslice -l kubernetes.io/service-name="${HELM_RELEASE}-metrics" -o json \
      | jq -r 'if ([.items[]?.endpoints[]?.conditions.ready] | any(. == true)) then "true" else "false" end'
  )"

  assert_equals "${auth_secret}" "${METRICS_AUTH_SECRET}" "exporter auth Secret mismatch"
  if [[ "$(bundle_ca_type)" == "secret" ]]; then
    ca_mount_type="secret"
    assert_equals "${ca_secret}" "$(bundle_ca_name)" "exporter CA Secret mismatch"
    assert_equals "${ca_configmap}" "" "exporter CA ConfigMap should not be set"
  else
    ca_mount_type="configMap"
    assert_equals "${ca_secret}" "" "exporter CA Secret should not be set"
    assert_equals "${ca_configmap}" "$(bundle_ca_name)" "exporter CA ConfigMap mismatch"
  fi
  assert_equals "${auth_type}" "authorizationHeader" "exporter auth type mismatch"
  assert_equals "${auth_header_type}" "Bearer" "exporter header type mismatch"
  assert_equals "${source_scheme}" "https" "exporter source scheme mismatch"
  assert_equals "${source_host}" "${EXPORTER_SOURCE_HOST_OVERRIDE}" "exporter source host mismatch"
  assert_equals "${source_port}" "8443" "exporter source port mismatch"
  assert_equals "${source_path}" "/nifi-api/flow/metrics/prometheus" "exporter source path mismatch"
  assert_equals "${flow_status_enabled}" "true" "exporter flow-status supplement flag mismatch"
  assert_equals "${flow_status_path}" "/nifi-api/flow/status" "exporter flow-status path mismatch"
  assert_equals "${service_port}" "9090" "exporter Service port mismatch"
  assert_equals "${service_target_port}" "metrics" "exporter Service targetPort mismatch"
  assert_equals "${service_selector_component}" "metrics-exporter" "exporter Service selector component mismatch"
  assert_equals "${service_component_label}" "metrics" "exporter Service component label mismatch"
  assert_equals "${service_monitor_path}" "/metrics" "exporter ServiceMonitor path mismatch"
  assert_equals "${service_monitor_port}" "metrics" "exporter ServiceMonitor port mismatch"
  assert_equals "${service_monitor_scheme}" "http" "exporter ServiceMonitor scheme mismatch"
  assert_equals "${service_monitor_selector_component}" "metrics-exporter" "exporter ServiceMonitor selector component mismatch"
  assert_equals "${endpoints_ready}" "true" "exporter Service EndpointSlice readiness mismatch"
  [[ -n "${ca_mount_type}" ]]
}

verify_exporter_mounts() {
  local exporter_pod

  exporter_pod="$(exporter_pod_name)"
  [[ -n "${exporter_pod}" ]]

  kubectl -n "${NAMESPACE}" exec "${exporter_pod}" -- /bin/sh -ec '
    test -f /exporter/exporter.py
    test -f /var/run/nifi-metrics-auth/token
    test -f /var/run/nifi-metrics-ca/'"$(bundle_ca_key)"'
  ' >/dev/null
}

scrape_upstream_from_exporter() {
  local path="$1"
  local accept_header="$2"
  local output_file="$3"
  local label="$4"
  local attempts="${5:-12}"
  local sleep_seconds="${6:-5}"
  local exporter_pod
  local attempt

  exporter_pod="$(exporter_pod_name)"
  [[ -n "${exporter_pod}" ]]

  for attempt in $(seq 1 "${attempts}"); do
    if kubectl -n "${NAMESPACE}" exec "${exporter_pod}" -- /bin/sh -ec "
      REQUEST_PATH='${path}' REQUEST_ACCEPT='${accept_header}' python3 - <<'PY'
import os
import ssl
import sys
import urllib.request

scheme = os.environ['EXPORTER_SOURCE_SCHEME']
host = os.environ['EXPORTER_SOURCE_HOST']
port = os.environ['EXPORTER_SOURCE_PORT']
request_path = os.environ['REQUEST_PATH']
timeout_seconds = float(os.environ.get('EXPORTER_SOURCE_TIMEOUT_SECONDS', '10'))
auth_type = os.environ['EXPORTER_AUTH_TYPE']
auth_header_type = os.environ.get('EXPORTER_AUTH_HEADER_TYPE', 'Bearer')
credentials_file = os.environ['EXPORTER_AUTH_CREDENTIALS_FILE']
ca_file = os.environ.get('EXPORTER_TLS_CA_FILE', '')
insecure_skip_verify = os.environ.get('EXPORTER_TLS_INSECURE_SKIP_VERIFY', 'false').lower() == 'true'
accept_header = os.environ.get('REQUEST_ACCEPT', '')

with open(credentials_file, 'r', encoding='utf-8') as handle:
    credentials = handle.read().strip()

if not credentials:
    raise RuntimeError('metrics exporter auth credentials are empty')

headers = {}
if auth_type == 'bearerToken':
    headers['Authorization'] = f'Bearer {credentials}'
elif auth_type == 'authorizationHeader':
    headers['Authorization'] = f'{auth_header_type} {credentials}'
else:
    raise RuntimeError(f'unsupported auth type: {auth_type}')

if accept_header:
    headers['Accept'] = accept_header

context = None
if scheme == 'https':
    if insecure_skip_verify:
        context = ssl._create_unverified_context()
    elif ca_file:
        context = ssl.create_default_context(cafile=ca_file)
    else:
        context = ssl.create_default_context()

request = urllib.request.Request(f'{scheme}://{host}:{port}{request_path}', headers=headers)
with urllib.request.urlopen(request, timeout=timeout_seconds, context=context) as response:
    sys.stdout.buffer.write(response.read())
PY
    " >"${output_file}.tmp"; then
      mv "${output_file}.tmp" "${output_file}"
      return 0
    fi

    if (( attempt == attempts )); then
      rm -f "${output_file}.tmp"
      echo "exporter upstream scrape for ${label} never succeeded" >&2
      return 1
    fi

    sleep "${sleep_seconds}"
  done
}

verify_exporter_source_reachability() {
  local flow_metrics_file="${TMPDIR_METRICS}/flow-prometheus.prom"
  local flow_status_file="${TMPDIR_METRICS}/flow-status.json"

  CURRENT_PHASE="probe-exporter-upstream-sources"
  FAILURE_CATEGORY="scrape"
  FAILURE_ENDPOINT="https://${EXPORTER_SOURCE_HOST_OVERRIDE}:8443/nifi-api/flow/metrics/prometheus and /nifi-api/flow/status"

  scrape_upstream_from_exporter "/nifi-api/flow/metrics/prometheus" "" "${flow_metrics_file}" "flow_prometheus"
  if ! grep -q '^# HELP ' "${flow_metrics_file}" ||
    ! grep -q '^# TYPE ' "${flow_metrics_file}" ||
    ! awk '!/^#/ && NF > 1 { found = 1; exit } END { exit found ? 0 : 1 }' "${flow_metrics_file}"; then
    echo "secured upstream flow metrics payload did not look like Prometheus text" >&2
    return 1
  fi

  scrape_upstream_from_exporter "/nifi-api/flow/status" "application/json" "${flow_status_file}" "flow_status"
  if ! jq -e '.controllerStatus | type == "object"' "${flow_status_file}" >/dev/null ||
    ! jq -e '.controllerStatus.activeThreadCount != null' "${flow_status_file}" >/dev/null ||
    ! jq -e '.controllerStatus.bytesQueued != null' "${flow_status_file}" >/dev/null; then
    echo "secured upstream flow status payload did not contain the expected controllerStatus fields" >&2
    return 1
  fi
}

assert_exporter_contains_upstream_metrics() {
  local exporter_metrics_file="$1"
  local upstream_metrics_file="$2"
  local metric_family
  local found_count=0

  while IFS= read -r metric_family; do
    [[ -n "${metric_family}" ]] || continue
    if ! grep -Eq "^${metric_family}(\\{| )" "${exporter_metrics_file}"; then
      echo "exporter payload did not contain upstream metric family ${metric_family}" >&2
      return 1
    fi
    found_count=$((found_count + 1))
  done < <(
    awk '
      !/^#/ && NF > 1 {
        metric = $1
        sub(/\{.*/, "", metric)
        if (!(metric in seen)) {
          print metric
          seen[metric] = 1
          count++
          if (count == 3) {
            exit
          }
        }
      }
    ' "${upstream_metrics_file}"
  )

  if (( found_count < 1 )); then
    echo "no upstream metric families were discovered for exporter comparison" >&2
    return 1
  fi
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
  local exporter_metrics_file="${TMPDIR_METRICS}/exporter-metrics.prom"
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
      printf '%s\n' "${body}" >"${exporter_metrics_file}"
      if printf '%s\n' "${body}" | grep -q '^# HELP ' &&
        printf '%s\n' "${body}" | grep -q '^# TYPE ' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_exporter_source_up{source=\"flow_prometheus\"} 1$' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_exporter_source_up{source=\"flow_status\"} 1$' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_exporter_source_scrape_duration_seconds{source=\"flow_prometheus\"} ' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_exporter_source_scrape_duration_seconds{source=\"flow_status\"} ' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_flow_status_controller_active_thread_count ' &&
        printf '%s\n' "${body}" | grep -q '^nifi_fabric_flow_status_controller_bytes_queued ' &&
        assert_exporter_contains_upstream_metrics "${exporter_metrics_file}" "${TMPDIR_METRICS}/flow-prometheus.prom"; then
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

reuse_command() {
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    printf '%s' "make kind-metrics-exporter-trust-manager-fast-e2e-reuse"
  else
    printf '%s' "make kind-metrics-exporter-fast-e2e-reuse"
  fi
}

dump_diagnostics() {
  local nifi_pod
  set +e
  nifi_pod="$(kubectl -n "${NAMESPACE}" get pod -l app.kubernetes.io/instance="${HELM_RELEASE}",app.kubernetes.io/name=nifi -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
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
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    kubectl get crd bundles.trust.cert-manager.io -o name || true
    helm -n "${TRUST_MANAGER_NAMESPACE}" status "${TRUST_MANAGER_RELEASE}" || true
    kubectl -n "${TRUST_MANAGER_NAMESPACE}" get deployment,cronjob,job,secret || true
    kubectl -n "${TRUST_MANAGER_NAMESPACE}" logs deployment/"${TRUST_MANAGER_DEPLOYMENT}" --tail=200 || true
    kubectl get bundle "${TRUST_BUNDLE_NAME}" -o yaml || true
    kubectl -n "${TRUST_MANAGER_NAMESPACE}" get secret "${TRUST_SOURCE_SECRET_NAME}" -o yaml || true
  fi
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,deployment,service,servicemonitor,pod,secret,configmap || true
  kubectl -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}" || true
  kubectl -n "${NAMESPACE}" describe statefulset "${HELM_RELEASE}" || true
  kubectl -n "${NAMESPACE}" describe deployment "${EXPORTER_DEPLOYMENT_NAME}" || true
  kubectl -n "${NAMESPACE}" describe service "${HELM_RELEASE}-metrics" || true
  kubectl -n "${NAMESPACE}" describe servicemonitor "${HELM_RELEASE}-exporter" || true
  kubectl -n "${NAMESPACE}" describe secret "${METRICS_AUTH_SECRET}" || true
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    if [[ "$(bundle_ca_type)" == "secret" ]]; then
      kubectl -n "${NAMESPACE}" describe secret "$(bundle_ca_name)" || true
    else
      kubectl -n "${NAMESPACE}" describe configmap "$(bundle_ca_name)" || true
    fi
  else
    kubectl -n "${NAMESPACE}" describe secret "${METRICS_CA_SECRET}" || true
  fi
  kubectl -n "${NAMESPACE}" get deployment "${EXPORTER_DEPLOYMENT_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o yaml || true
  kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o yaml || true
  kubectl -n "${NAMESPACE}" get endpointslice -l kubernetes.io/service-name="${HELM_RELEASE}-metrics" -o yaml || true
  kubectl -n "${NAMESPACE}" get configmap "${EXPORTER_DEPLOYMENT_NAME}-config" -o yaml || true
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    if [[ "$(bundle_ca_type)" == "secret" ]]; then
      kubectl -n "${NAMESPACE}" get secret "$(bundle_ca_name)" -o yaml || true
    else
      kubectl -n "${NAMESPACE}" get configmap "$(bundle_ca_name)" -o yaml || true
    fi
  fi
  kubectl -n "${NAMESPACE}" logs deployment/"${EXPORTER_DEPLOYMENT_NAME}" --tail=200 || true
  if [[ -n "${nifi_pod}" ]]; then
    kubectl -n "${NAMESPACE}" describe pod "${nifi_pod}" || true
    kubectl -n "${NAMESPACE}" logs "${nifi_pod}" -c nifi --tail=200 || true
  fi
  exporter_pod_name | xargs -r -I{} kubectl -n "${NAMESPACE}" exec {} -- /bin/sh -ec '
    echo "== exporter auth mount =="
    ls -l /var/run/nifi-metrics-auth
    echo "== exporter ca mount =="
    ls -l /var/run/nifi-metrics-ca
    echo "== exporter ca bundle preview =="
    head -n 5 /var/run/nifi-metrics-ca/'"$(bundle_ca_key)"' || true
  ' || true
  kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec '
    echo "== probe ca mount =="
    ls -l /etc/nifi-metrics-ca
  ' || true
  kubectl -n "${NAMESPACE}" logs "${PROBE_POD_NAME}" || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  if [[ -n "${TMPDIR_METRICS}" && -d "${TMPDIR_METRICS}" ]]; then
    find "${TMPDIR_METRICS}" -maxdepth 1 -type f -print -exec sh -ec 'echo "--- ${1}"; sed -n "1,80p" "${1}"' _ {} \; || true
  fi
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
  --set-string "nifi.observability.metrics.exporter.image.repository=${EXPORTER_IMAGE_REPOSITORY}"
  --set-string "nifi.observability.metrics.exporter.image.tag=${EXPORTER_IMAGE_TAG}"
  --set-string "nifi.observability.metrics.exporter.source.host=${EXPORTER_SOURCE_HOST_OVERRIDE}"
)

if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
  helm_values_args+=(
    -f "${ROOT_DIR}/${PLATFORM_TRUST_MANAGER_VALUES_FILE}"
    -f "${ROOT_DIR}/${PLATFORM_TRUST_MANAGER_METRICS_VALUES_FILE}"
  )
fi

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
check_prereqs

TMPDIR_METRICS="$(mktemp -d)"

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

if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
  CURRENT_PHASE="install-trust-manager"
  FAILURE_CATEGORY="rendering"
  FAILURE_ENDPOINT="trust-manager Bundle and mirror prerequisites"
  phase "Installing cert-manager and trust-manager prerequisites"
  install_trust_manager
fi

CURRENT_PHASE="create-bootstrap-metrics-secrets"
FAILURE_CATEGORY="auth-material"
if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
  FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET}"
else
  FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET},Secret/${METRICS_CA_SECRET}"
fi
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
if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
  verify_trust_manager_bundle_contract
fi
verify_metrics_secret_contract
verify_exporter_resources
verify_exporter_mounts

phase "Proving the exporter pod can reach the documented secured NiFi metrics source endpoints"
verify_exporter_source_reachability

CURRENT_PHASE="probe-exporter-metrics"
FAILURE_CATEGORY="scrape"
FAILURE_ENDPOINT="/metrics"
phase "Probing the clean exporter /metrics endpoint"
install_probe_pod
probe_exporter_endpoint "/healthz" "200"
probe_exporter_endpoint "/readyz" "200"
scrape_exporter_metrics

CURRENT_PHASE="verify-exporter-secret-rotation"
FAILURE_CATEGORY="auth-material"
FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET} mounted into ${EXPORTER_DEPLOYMENT_NAME}"
phase "Proving exporter recovery after machine-auth Secret rotation without restarting the pod"
verify_exporter_secret_reload_without_restart

print_success_footer "exporter metrics runtime proof completed" \
  "$(reuse_command)" \
  "kubectl -n ${NAMESPACE} get deployment ${EXPORTER_DEPLOYMENT_NAME} -o yaml" \
  "kubectl -n ${NAMESPACE} get service ${HELM_RELEASE}-metrics -o yaml" \
  "kubectl -n ${NAMESPACE} get servicemonitor ${HELM_RELEASE}-exporter -o yaml" \
  "kubectl -n ${NAMESPACE} exec ${PROBE_POD_NAME} -- sh -ec 'curl --silent --show-error --fail http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090/metrics | head'" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml"
