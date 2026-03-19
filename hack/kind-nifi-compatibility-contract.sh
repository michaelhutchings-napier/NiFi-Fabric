#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-compatibility}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
NIFI_IMAGE_REPOSITORY="${NIFI_IMAGE_REPOSITORY:-apache/nifi}"
NIFI_IMAGE_TAG="${NIFI_IMAGE_TAG:-2.0.0}"
VERSION_LABEL="${VERSION_LABEL:-2.0.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
PLATFORM_METRICS_VALUES_FILE="${PLATFORM_METRICS_VALUES_FILE:-examples/platform-managed-metrics-native-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-true}"
PROM_CRDS_NAMESPACE="${PROM_CRDS_NAMESPACE:-monitoring}"
PROM_CRDS_RELEASE="${PROM_CRDS_RELEASE:-prometheus-operator-crds}"
METRICS_AUTH_SECRET="${METRICS_AUTH_SECRET:-nifi-metrics-auth}"
METRICS_CA_SECRET="${METRICS_CA_SECRET:-nifi-metrics-ca}"
PROBE_POD_NAME="${PROBE_POD_NAME:-compatibility-probe}"
PROBE_IMAGE="${PROBE_IMAGE:-curlimages/curl:8.12.1}"
AUTOSCALING_TARGET_REPLICAS="${AUTOSCALING_TARGET_REPLICAS:-3}"
CURRENT_PHASE="${CURRENT_PHASE:-bootstrap}"
FAILURE_ENDPOINT="${FAILURE_ENDPOINT:-}"
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

assert_equals() {
  local actual="$1"
  local expected="$2"
  local message="$3"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "${message}: expected ${expected}, got ${actual:-<empty>}" >&2
    return 1
  fi
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

wait_for_nonempty() {
  local description="$1"
  local timeout_seconds="$2"
  shift 2
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local actual=""

  while true; do
    actual="$("$@" | tr -d '\n')"
    if [[ -n "${actual}" ]]; then
      printf '%s\n' "${actual}"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${description}" >&2
      return 1
    fi
    sleep 5
  done
}

cluster_jsonpath() {
  local jsonpath="$1"
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

sts_jsonpath() {
  local jsonpath="$1"
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

wait_for_condition_true() {
  local condition_type="$1"
  local timeout_seconds="${2:-300}"
  wait_for_output "condition ${condition_type}=True" "True" "${timeout_seconds}" \
    cluster_jsonpath "{.status.conditions[?(@.type==\"${condition_type}\")].status}"
}

wait_for_sts_replicas() {
  local replicas="$1"
  local timeout_seconds="${2:-300}"
  wait_for_output "StatefulSet replicas ${replicas}" "${replicas}" "${timeout_seconds}" \
    sts_jsonpath '{.spec.replicas}'
}

wait_for_cluster_reason() {
  local reason="$1"
  local timeout_seconds="${2:-300}"
  wait_for_output "autoscaling reason ${reason}" "${reason}" "${timeout_seconds}" \
    cluster_jsonpath '{.status.autoscaling.reason}'
}

wait_for_cluster_last_scale_up_time() {
  wait_for_nonempty "lastScaleUpTime" "${1:-300}" cluster_jsonpath '{.status.autoscaling.lastScaleUpTime}'
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

create_metrics_secrets() {
  bash "${ROOT_DIR}/hack/bootstrap-metrics-machine-auth.sh" \
    --namespace "${NAMESPACE}" \
    --metrics-auth-secret "${METRICS_AUTH_SECRET}" \
    --metrics-ca-secret "${METRICS_CA_SECRET}" \
    --auth-mode authorizationHeader \
    --token bootstrap-pending >/dev/null
}

mint_metrics_token() {
  bash "${ROOT_DIR}/hack/bootstrap-metrics-machine-auth.sh" \
    --namespace "${NAMESPACE}" \
    --metrics-auth-secret "${METRICS_AUTH_SECRET}" \
    --metrics-ca-secret "${METRICS_CA_SECRET}" \
    --auth-mode authorizationHeader \
    --source-auth-secret nifi-auth \
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
    secret:
      secretName: ${METRICS_CA_SECRET}
      items:
      - key: ca.crt
        path: ca.crt
EOF
  wait_for_probe_ready
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

verify_metrics_secret_references() {
  local name="$1"
  local expected_interval="$2"
  local authorization_secret
  local authorization_type
  local ca_secret
  local server_name
  local path
  local interval

  authorization_secret="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].authorization.credentials.name}')"
  authorization_type="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].authorization.type}')"
  ca_secret="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].tlsConfig.ca.secret.name}')"
  server_name="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].tlsConfig.serverName}')"
  path="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].path}')"
  interval="$(kubectl -n "${NAMESPACE}" get servicemonitor "${name}" -o jsonpath='{.spec.endpoints[0].interval}')"

  assert_equals "${authorization_secret}" "${METRICS_AUTH_SECRET}" "${name} auth Secret mismatch"
  assert_equals "${authorization_type}" "Bearer" "${name} authorization type mismatch"
  assert_equals "${ca_secret}" "${METRICS_CA_SECRET}" "${name} CA Secret mismatch"
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

scrape_flow_metrics() {
  local metrics_service_ip
  local attempt
  local max_attempts=12
  local http_code

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

patch_main_cluster() {
  local patch="$1"
  kubectl -n "${NAMESPACE}" patch nificluster "${HELM_RELEASE}" --type merge -p "${patch}" >/dev/null
}

configure_bounded_scale_up() {
  patch_main_cluster "$(cat <<EOF
{"spec":{"autoscaling":{"mode":"Enforced","scaleUp":{"enabled":true,"cooldown":"5m"},"scaleDown":{"enabled":false},"minReplicas":${AUTOSCALING_TARGET_REPLICAS},"maxReplicas":${AUTOSCALING_TARGET_REPLICAS},"signals":["QueuePressure","CPU"]}}}
EOF
)"
}

configure_disabled_autoscaling() {
  patch_main_cluster '{"spec":{"autoscaling":{"mode":"Disabled","scaleUp":{"enabled":false,"cooldown":"5m"},"scaleDown":{"enabled":false},"minReplicas":0,"maxReplicas":0,"signals":[]}}}'
}

dump_diagnostics() {
  set +e
  echo
  echo "==> NiFi ${VERSION_LABEL} compatibility diagnostics after failure at +$(elapsed)s"
  echo "  failed phase: ${CURRENT_PHASE}"
  echo "  endpoint or contract: ${FAILURE_ENDPOINT:-n/a}"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl get crd servicemonitors.monitoring.coreos.com -o name || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,service,servicemonitor,pod,secret || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-flow" -o yaml || true
  kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-flow-fast" -o yaml || true
  kubectl -n "${NAMESPACE}" get secret "${METRICS_AUTH_SECRET}" -o yaml || true
  kubectl -n "${NAMESPACE}" get secret "${METRICS_CA_SECRET}" -o yaml || true
  kubectl -n "${NAMESPACE}" logs "${PROBE_POD_NAME}" || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
  -f "${ROOT_DIR}/${PLATFORM_METRICS_VALUES_FILE}"
  --set-string "nifi.image.repository=${NIFI_IMAGE_REPOSITORY}"
  --set-string "nifi.image.tag=${NIFI_IMAGE_TAG}"
  --set-string "nifi.image.pullPolicy=IfNotPresent"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

CURRENT_PHASE="check-prerequisites"
phase "Checking prerequisites for NiFi ${VERSION_LABEL} compatibility contract"
check_prereqs

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  CURRENT_PHASE="reuse-kind"
  phase "Reusing existing kind cluster for NiFi ${VERSION_LABEL} compatibility contract"
  configure_kind_kubeconfig
else
  CURRENT_PHASE="bootstrap-kind"
  phase "Creating fresh kind cluster for NiFi ${VERSION_LABEL} compatibility contract"
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
  configure_kind_kubeconfig

  CURRENT_PHASE="load-nifi-image"
  FAILURE_ENDPOINT="${NIFI_IMAGE}"
  phase "Loading NiFi image ${NIFI_IMAGE} into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  CURRENT_PHASE="create-core-secrets"
  FAILURE_ENDPOINT="Secret/nifi-tls and Secret/nifi-auth"
  phase "Creating TLS and auth Secrets"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}" NIFI_IMAGE="${NIFI_IMAGE}"

  CURRENT_PHASE="build-load-controller"
  FAILURE_ENDPOINT="controller image"
  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
fi

CURRENT_PHASE="install-prometheus-crds"
FAILURE_ENDPOINT="ServiceMonitor CRD"
phase "Installing Prometheus Operator CRDs"
install_prometheus_operator_crds

CURRENT_PHASE="create-metrics-secrets"
FAILURE_ENDPOINT="Secret/${METRICS_AUTH_SECRET} and Secret/${METRICS_CA_SECRET}"
phase "Creating operator-provided metrics Secrets"
create_metrics_secrets

CURRENT_PHASE="install-platform-chart"
FAILURE_ENDPOINT="charts/nifi-platform"
phase "Installing product chart managed release for NiFi ${VERSION_LABEL}${profile_label}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

CURRENT_PHASE="verify-platform-install"
FAILURE_ENDPOINT="platform resources and controller rollout"
phase "Verifying platform resources and controller rollout"
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

CURRENT_PHASE="verify-health"
FAILURE_ENDPOINT="secured NiFi API readiness"
phase "Verifying secured cluster health and readiness"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved 300
wait_for_condition_true Available 300
wait_for_sts_replicas 2 300

CURRENT_PHASE="mint-metrics-token"
FAILURE_ENDPOINT="NiFi access token bootstrap"
phase "Minting a bearer token for native metrics access"
mint_metrics_token

CURRENT_PHASE="verify-metrics-contract"
FAILURE_ENDPOINT="Service/${HELM_RELEASE}-metrics and ServiceMonitor/${HELM_RELEASE}-flow*"
phase "Verifying shared native metrics contract"
kubectl -n "${NAMESPACE}" get service "${HELM_RELEASE}-metrics" >/dev/null
kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-flow" >/dev/null
kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-flow-fast" >/dev/null
verify_metrics_service_contract
verify_metrics_secret_contract
verify_metrics_secret_references "${HELM_RELEASE}-flow" "30s"
verify_metrics_secret_references "${HELM_RELEASE}-flow-fast" "15s"
verify_metrics_service_monitor_selector "${HELM_RELEASE}-flow"
verify_metrics_service_monitor_selector "${HELM_RELEASE}-flow-fast"

CURRENT_PHASE="probe-native-metrics"
FAILURE_ENDPOINT="/nifi-api/flow/metrics/prometheus"
phase "Probing the secured native metrics endpoint"
install_probe_pod
scrape_flow_metrics

CURRENT_PHASE="autoscaling-scale-up"
FAILURE_ENDPOINT="bounded controller-owned scale-up from 2 to ${AUTOSCALING_TARGET_REPLICAS}"
phase "Proving bounded controller-owned autoscaling scale-up"
configure_bounded_scale_up
wait_for_event_reason AutoscalingScaleUp 180
wait_for_sts_replicas "${AUTOSCALING_TARGET_REPLICAS}" 600
wait_for_cluster_last_scale_up_time 300 >/dev/null
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"
wait_for_condition_true TargetResolved 600
wait_for_condition_true Available 600

CURRENT_PHASE="restore-autoscaling-baseline"
FAILURE_ENDPOINT="Disabled autoscaling baseline"
phase "Restoring disabled autoscaling baseline"
configure_disabled_autoscaling
wait_for_cluster_reason "${autoscalingReasonDisabled:-Disabled}" 120

print_success_footer "NiFi ${VERSION_LABEL} compatibility contract completed" \
  "make kind-health KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get servicemonitor ${HELM_RELEASE}-flow -o yaml" \
  "kubectl -n ${NAMESPACE} exec ${PROBE_POD_NAME} -- sh -ec 'curl --silent --show-error --fail --cacert /etc/nifi-metrics-ca/ca.crt -H \"Authorization: Bearer \${METRICS_TOKEN}\" https://${HELM_RELEASE}.${NAMESPACE}.svc.cluster.local:8443/nifi-api/flow/metrics/prometheus | head'"
