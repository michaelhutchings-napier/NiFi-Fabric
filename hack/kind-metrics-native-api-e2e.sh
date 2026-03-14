#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-metrics-native-api}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_METRICS_VALUES_FILE="${PLATFORM_METRICS_VALUES_FILE:-examples/platform-managed-metrics-native-values.yaml}"
PLATFORM_TRUST_MANAGER_VALUES_FILE="${PLATFORM_TRUST_MANAGER_VALUES_FILE:-examples/platform-managed-trust-manager-values.yaml}"
PLATFORM_TRUST_MANAGER_METRICS_VALUES_FILE="${PLATFORM_TRUST_MANAGER_METRICS_VALUES_FILE:-examples/platform-managed-metrics-native-trust-manager-values.yaml}"
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
TRUST_MANAGER_PKCS12_KEY="${TRUST_MANAGER_PKCS12_KEY:-truststore.p12}"
PROBE_POD_NAME="${PROBE_POD_NAME:-metrics-probe}"
PROBE_IMAGE="${PROBE_IMAGE:-curlimages/curl:8.12.1}"
SOURCE_AUTH_SECRET="${SOURCE_AUTH_SECRET:-nifi-auth}"
CURRENT_PHASE="${CURRENT_PHASE:-bootstrap}"
FAILURE_CATEGORY="${FAILURE_CATEGORY:-unknown}"
FAILURE_ENDPOINT="${FAILURE_ENDPOINT:-}"
START_EPOCH="$(date +%s)"

TMPDIR_METRICS=""
RESTORE_TLS_CA_B64=""

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

read_bundle_hash() {
  read_bundle_data 2>/dev/null | sha256sum | awk '{print $1}'
}

read_probe_ca_hash() {
  kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec \
    "sha256sum /etc/nifi-metrics-ca/$(bundle_ca_key) | cut -d' ' -f1" 2>/dev/null
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
    env:
    - name: METRICS_TOKEN
      valueFrom:
        secretKeyRef:
          name: ${METRICS_AUTH_SECRET}
          key: token
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
  local ca_name

  token_preview="$(kubectl -n "${NAMESPACE}" get secret "${METRICS_AUTH_SECRET}" -o jsonpath='{.data.token}' | base64 --decode | cut -c1-18)"
  ca_name="$(bundle_ca_name)"

  if [[ -z "${token_preview}" || "${token_preview}" == "bootstrap-pending" ]]; then
    echo "metrics auth Secret ${METRICS_AUTH_SECRET} was not updated with a live token" >&2
    return 1
  fi
  ca_present="$(read_bundle_data | tr -d '\n')"
  if [[ -z "${ca_present}" ]]; then
    echo "metrics CA bundle ${ca_name} does not contain $(bundle_ca_key)" >&2
    return 1
  fi
}

verify_metrics_secret_references() {
  local name="$1"
  local expected_interval="$2"
  local authorization_secret
  local authorization_type
  local ca_resource
  local server_name
  local path
  local interval

  authorization_secret="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].authorization.credentials.name}')"
  authorization_type="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].authorization.type}')"
  server_name="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].tlsConfig.serverName}')"
  path="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].path}')"
  interval="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].interval}')"

  assert_equals "${authorization_secret}" "${METRICS_AUTH_SECRET}" "${name} auth Secret mismatch"
  assert_equals "${authorization_type}" "Bearer" "${name} authorization type mismatch"
  if [[ "$(bundle_ca_type)" == "secret" ]]; then
    ca_resource="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].tlsConfig.ca.secret.name}')"
  else
    ca_resource="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].tlsConfig.ca.configMap.name}')"
  fi
  assert_equals "${ca_resource}" "$(bundle_ca_name)" "${name} CA bundle reference mismatch"
  assert_equals "${server_name}" "${HELM_RELEASE}.${NAMESPACE}.svc.cluster.local" "${name} serverName mismatch"
  assert_equals "${path}" "/nifi-api/flow/metrics/prometheus" "${name} path mismatch"
  assert_equals "${interval}" "${expected_interval}" "${name} interval mismatch"
}

verify_metrics_service_contract() {
  local service_port
  local target_port
  local component_label

  service_port="$(kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o jsonpath='{.spec.ports[0].port}')"
  target_port="$(kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o jsonpath='{.spec.ports[0].targetPort}')"
  component_label="$(kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/component}')"

  assert_equals "${service_port}" "8443" "metrics Service port mismatch"
  assert_equals "${target_port}" "https" "metrics Service targetPort mismatch"
  assert_equals "${component_label}" "metrics" "metrics Service component label mismatch"
}

verify_metrics_service_monitor_selector() {
  local name="$1"
  local component_label

  component_label="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.selector.matchLabels.app\.kubernetes\.io/component}')"
  assert_equals "${component_label}" "metrics" "${name} selector component label mismatch"
}

verify_trust_manager_bundle_contract() {
  kubectl get bundle "${TRUST_BUNDLE_NAME}" >/dev/null
  kubectl -n "${TRUST_MANAGER_NAMESPACE}" get secret "${TRUST_SOURCE_SECRET_NAME}" >/dev/null
  wait_for_bundle_resource
  if [[ "$(bundle_ca_type)" == "secret" ]]; then
    kubectl -n "${NAMESPACE}" get secret "${TRUST_BUNDLE_NAME}" -o json | jq -r --arg key "${TRUST_MANAGER_PKCS12_KEY}" '.data[$key] // empty' | grep -q .
  fi
}

scrape_flow_metrics() {
  local metrics_service_ip
  local attempt
  local max_attempts=12
  local http_code
  CURRENT_PHASE="scrape-native-flow-metrics"
  FAILURE_CATEGORY="scrape"
  FAILURE_ENDPOINT="https://${HELM_RELEASE}.${NAMESPACE}.svc.cluster.local:8443/nifi-api/flow/metrics/prometheus"
  metrics_service_ip="$(kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" -o jsonpath='{.spec.clusterIP}')"
  [[ -n "${metrics_service_ip}" ]]

  for attempt in $(seq 1 "${max_attempts}"); do
    http_code="$(
      kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec "
        curl --silent --show-error \
          --output /tmp/flow-metrics.prom \
          --write-out '%{http_code}' \
          --cacert /etc/nifi-metrics-ca/ca.crt \
          -H \"Authorization: Bearer \${METRICS_TOKEN}\" \
          --resolve \"${HELM_RELEASE}.${NAMESPACE}.svc.cluster.local:8443:${metrics_service_ip}\" \
          \"https://${HELM_RELEASE}.${NAMESPACE}.svc.cluster.local:8443/nifi-api/flow/metrics/prometheus\"
      "
    )"
    if [[ "${http_code}" == "200" ]] && kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec "
      grep -q '^# HELP ' /tmp/flow-metrics.prom
      grep -q '^# TYPE ' /tmp/flow-metrics.prom
    " >/dev/null; then
      return 0
    fi
    if (( attempt == max_attempts )); then
      echo "nativeApi flow scrape never succeeded: final HTTP status ${http_code:-<empty>}" >&2
      return 1
    fi
    sleep 5
  done
}

wait_for_probe_ca_hash_change() {
  local previous_hash="$1"
  local attempts="${2:-24}"
  local sleep_seconds="${3:-5}"
  local current_hash
  local attempt

  for attempt in $(seq 1 "${attempts}"); do
    current_hash="$(read_probe_ca_hash || true)"
    if [[ -n "${current_hash}" && "${current_hash}" != "${previous_hash}" ]]; then
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  echo "probe pod trust bundle file did not update" >&2
  return 1
}

verify_trust_manager_update_propagation() {
  local original_source_b64
  local original_bundle_hash
  local original_probe_hash
  local temp_dir
  local original_pem_file
  local extra_key
  local extra_crt
  local merged_pem
  local merged_b64

  CURRENT_PHASE="verify-trust-manager-update-propagation"
  FAILURE_CATEGORY="scrape"
  FAILURE_ENDPOINT="workload TLS Secret -> mirrored source Secret -> trust-manager bundle -> nativeApi CA consumer"

  original_source_b64="$(kubectl -n "${NAMESPACE}" get secret nifi-tls -o jsonpath='{.data.ca\.crt}')"
  RESTORE_TLS_CA_B64="${original_source_b64}"
  original_bundle_hash="$(read_bundle_hash)"
  original_probe_hash="$(read_probe_ca_hash)"

  temp_dir="$(mktemp -d)"
  original_pem_file="${temp_dir}/source.pem"
  extra_key="${temp_dir}/extra.key"
  extra_crt="${temp_dir}/extra.crt"
  merged_pem="${temp_dir}/merged.pem"

  printf '%s' "${original_source_b64}" | base64 -d >"${original_pem_file}"
  openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
    -subj "/CN=nifi-fabric-trust-manager-extra-ca" \
    -keyout "${extra_key}" \
    -out "${extra_crt}" >/dev/null 2>&1
  cat "${original_pem_file}" "${extra_crt}" >"${merged_pem}"
  merged_b64="$(base64 <"${merged_pem}" | tr -d '\n')"

  kubectl -n "${NAMESPACE}" patch secret nifi-tls --type merge -p "{\"data\":{\"ca.crt\":\"${merged_b64}\"}}" >/dev/null

  local attempt
  local current_hash
  for attempt in $(seq 1 24); do
    current_hash="$(read_bundle_hash)"
    if [[ -n "${current_hash}" && "${current_hash}" != "${original_bundle_hash}" ]]; then
      break
    fi
    sleep 5
  done
  if [[ -z "${current_hash}" || "${current_hash}" == "${original_bundle_hash}" ]]; then
    echo "trust-manager target bundle did not change after source TLS CA update" >&2
    return 1
  fi

  wait_for_probe_ca_hash_change "${original_probe_hash}"
  scrape_flow_metrics
  rm -rf "${temp_dir}"
}

dump_diagnostics() {
  set +e
  echo
  echo "==> metrics nativeApi diagnostics after failure at +$(elapsed)s"
  echo "  mode: nativeApi"
  echo "  failed phase: ${CURRENT_PHASE}"
  echo "  failure category: ${FAILURE_CATEGORY}"
  echo "  endpoint or contract: ${FAILURE_ENDPOINT:-n/a}"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl get crd servicemonitors.monitoring.coreos.com -o name || true
  kubectl -n "${PROM_CRDS_NAMESPACE}" get secret >/dev/null 2>&1 || true
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    kubectl get crd bundles.trust.cert-manager.io -o name || true
    helm -n "${TRUST_MANAGER_NAMESPACE}" status "${TRUST_MANAGER_RELEASE}" || true
    kubectl -n "${TRUST_MANAGER_NAMESPACE}" get deployment,cronjob,job,secret || true
    kubectl get bundle "${TRUST_BUNDLE_NAME}" -o yaml || true
  fi
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,service,servicemonitor,pod,secret,configmap || true
  kubectl -n "${NAMESPACE}" describe service "${HELM_RELEASE}-metrics" || true
  kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-flow" -o yaml || true
  kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-flow-fast" -o yaml || true
  kubectl -n "${NAMESPACE}" get secret "${METRICS_AUTH_SECRET}" -o yaml || true
  if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
    if [[ "$(bundle_ca_type)" == "secret" ]]; then
      kubectl -n "${NAMESPACE}" get secret "$(bundle_ca_name)" -o yaml || true
    else
      kubectl -n "${NAMESPACE}" get configmap "$(bundle_ca_name)" -o yaml || true
    fi
  else
    kubectl -n "${NAMESPACE}" get secret "${METRICS_CA_SECRET}" -o yaml || true
  fi
  kubectl -n "${NAMESPACE}" logs "${PROBE_POD_NAME}" || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
}

cleanup() {
  if [[ -n "${RESTORE_TLS_CA_B64}" ]]; then
    kubectl -n "${NAMESPACE}" patch secret nifi-tls --type merge -p "{\"data\":{\"ca.crt\":\"${RESTORE_TLS_CA_B64}\"}}" >/dev/null 2>&1 || true
  fi
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

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for nativeApi metrics runtime proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for nativeApi metrics runtime proof"
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
FAILURE_ENDPOINT="metrics auth material"
phase "Creating operator-provided metrics Secrets"
create_metrics_secrets

CURRENT_PHASE="install-platform-chart"
FAILURE_CATEGORY="rendering"
FAILURE_ENDPOINT="nativeApi metrics Service and ServiceMonitor resources"
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

CURRENT_PHASE="verify-nativeapi-contract"
FAILURE_CATEGORY="rendering"
FAILURE_ENDPOINT="Service/nifi-metrics and ServiceMonitor/nifi-flow,nifi-flow-fast"
phase "Verifying metrics Service and ServiceMonitor objects"
kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" >/dev/null
kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-flow" >/dev/null
kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-flow-fast" >/dev/null
if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
  verify_trust_manager_bundle_contract
fi
verify_metrics_service_contract
verify_metrics_secret_contract
verify_metrics_secret_references "${HELM_RELEASE}-flow" "30s"
verify_metrics_secret_references "${HELM_RELEASE}-flow-fast" "15s"
verify_metrics_service_monitor_selector "${HELM_RELEASE}-flow"
verify_metrics_service_monitor_selector "${HELM_RELEASE}-flow-fast"

CURRENT_PHASE="probe-nativeapi-flow"
FAILURE_CATEGORY="scrape"
FAILURE_ENDPOINT="/nifi-api/flow/metrics/prometheus"
phase "Probing the secured native flow metrics endpoint with machine auth"
install_probe_pod
scrape_flow_metrics

if [[ "${TRUST_MANAGER_ENABLED}" == "true" ]]; then
  phase "Verifying trust-manager-backed trust updates reach the native metrics consumer path"
  verify_trust_manager_update_propagation
fi

print_success_footer "nativeApi metrics runtime proof completed" \
  "make kind-metrics-native-api-fast-e2e-reuse" \
  "kubectl -n ${NAMESPACE} get service ${HELM_RELEASE}-metrics" \
  "kubectl -n ${NAMESPACE} get servicemonitor ${HELM_RELEASE}-flow -o yaml" \
  "kubectl -n ${NAMESPACE} exec ${PROBE_POD_NAME} -- sh -ec 'curl --silent --show-error --fail --cacert /etc/nifi-metrics-ca/$(bundle_ca_key) -H \"Authorization: Bearer \${METRICS_TOKEN}\" https://${HELM_RELEASE}.${NAMESPACE}.svc.cluster.local:8443/nifi-api/flow/metrics/prometheus | head'" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml"
