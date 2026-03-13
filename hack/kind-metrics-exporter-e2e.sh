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

install_prometheus_operator_crds() {
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
  helm repo update prometheus-community >/dev/null
  helm upgrade --install "${PROM_CRDS_RELEASE}" prometheus-community/prometheus-operator-crds \
    --namespace "${PROM_CRDS_NAMESPACE}" \
    --create-namespace >/dev/null
  kubectl get crd servicemonitors.monitoring.coreos.com >/dev/null
}

create_metrics_secrets() {
  local ca_file=""

  TMPDIR_METRICS="$(mktemp -d)"
  ca_file="${TMPDIR_METRICS}/ca.crt"

  kubectl -n "${NAMESPACE}" get secret nifi-tls -o jsonpath='{.data.ca\.crt}' | base64 --decode >"${ca_file}"

  kubectl -n "${NAMESPACE}" create secret generic "${METRICS_AUTH_SECRET}" \
    --from-literal=token=bootstrap-pending \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  kubectl -n "${NAMESPACE}" create secret generic "${METRICS_CA_SECRET}" \
    --from-file=ca.crt="${ca_file}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

mint_metrics_token() {
  local username
  local password
  local token
  local host

  username="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.username}' | base64 --decode)"
  password="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.password}' | base64 --decode)"
  host="${HELM_RELEASE}-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local"

  token="$(
    kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- \
      env NIFI_HOST="${host}" NIFI_USERNAME="${username}" NIFI_PASSWORD="${password}" TLS_CA_PATH="/opt/nifi/tls/ca.crt" sh -ec '
        curl --silent --show-error --fail \
          --cacert "${TLS_CA_PATH}" \
          -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
          --data-urlencode "username=${NIFI_USERNAME}" \
          --data-urlencode "password=${NIFI_PASSWORD}" \
          "https://${NIFI_HOST}:8443/nifi-api/access/token"
      '
  )"

  [[ -n "${token}" ]]

  kubectl -n "${NAMESPACE}" create secret generic "${METRICS_AUTH_SECRET}" \
    --from-literal=token="${token}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
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

verify_exporter_resources() {
  local auth_secret
  local ca_secret
  local auth_type
  local auth_header_type
  local source_host
  local source_path
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
  service_monitor_path="$(kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o jsonpath='{.spec.endpoints[0].path}')"
  service_monitor_scheme="$(kubectl -n "${NAMESPACE}" get servicemonitor "${HELM_RELEASE}-exporter" -o jsonpath='{.spec.endpoints[0].scheme}')"

  [[ "${auth_secret}" == "${METRICS_AUTH_SECRET}" ]]
  [[ "${ca_secret}" == "${METRICS_CA_SECRET}" ]]
  [[ "${auth_type}" == "authorizationHeader" ]]
  [[ "${auth_header_type}" == "Bearer" ]]
  [[ "${source_host}" == "${HELM_RELEASE}.${NAMESPACE}.svc.cluster.local" ]]
  [[ "${source_path}" == "/nifi-api/flow/metrics/prometheus" ]]
  [[ "${service_monitor_path}" == "/metrics" ]]
  [[ "${service_monitor_scheme}" == "http" ]]
}

verify_exporter_mounts() {
  local exporter_pod

  exporter_pod="$(kubectl -n "${NAMESPACE}" get pod -l app.kubernetes.io/component=metrics-exporter -o jsonpath='{.items[0].metadata.name}')"
  [[ -n "${exporter_pod}" ]]

  kubectl -n "${NAMESPACE}" exec "${exporter_pod}" -- /bin/sh -ec '
    test -f /exporter/exporter.py
    test -f /var/run/nifi-metrics-auth/token
    test -f /var/run/nifi-metrics-ca/ca.crt
  ' >/dev/null
}

scrape_exporter_metrics() {
  kubectl -n "${NAMESPACE}" exec "${PROBE_POD_NAME}" -- /bin/sh -ec "
    curl --silent --show-error --fail \
      http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090/metrics \
      | tee /tmp/exporter-metrics.prom >/dev/null
    grep -q '^# HELP ' /tmp/exporter-metrics.prom
    grep -q '^# TYPE ' /tmp/exporter-metrics.prom
  " >/dev/null
}

restart_exporter_deployment() {
  kubectl -n "${NAMESPACE}" rollout restart deployment/"${EXPORTER_DEPLOYMENT_NAME}" >/dev/null
  wait_for_exporter_ready
}

dump_diagnostics() {
  set +e
  echo
  echo "==> metrics exporter diagnostics after failure at +$(elapsed)s"
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

phase "Installing Prometheus Operator CRDs for ServiceMonitor acceptance"
install_prometheus_operator_crds

phase "Creating operator-provided metrics Secrets"
create_metrics_secrets

phase "Installing product chart managed release${profile_label}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

phase "Verifying platform resources and controller rollout"
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null
wait_for_exporter_ready

phase "Verifying secured NiFi cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

phase "Minting a bearer token for the operator-provided metrics Secret"
mint_metrics_token

phase "Restarting the exporter to pick up the fresh machine-auth token"
restart_exporter_deployment

phase "Verifying exporter deployment, Service, and ServiceMonitor wiring"
verify_exporter_resources
verify_exporter_mounts

phase "Probing the clean exporter /metrics endpoint"
install_probe_pod
scrape_exporter_metrics

print_success_footer "exporter metrics runtime proof completed" \
  "make kind-metrics-exporter-fast-e2e-reuse" \
  "kubectl -n ${NAMESPACE} get deployment ${EXPORTER_DEPLOYMENT_NAME} -o yaml" \
  "kubectl -n ${NAMESPACE} get service ${HELM_RELEASE}-metrics -o yaml" \
  "kubectl -n ${NAMESPACE} get servicemonitor ${HELM_RELEASE}-exporter -o yaml" \
  "kubectl -n ${NAMESPACE} exec ${PROBE_POD_NAME} -- sh -ec 'curl --silent --show-error --fail http://${HELM_RELEASE}-metrics.${NAMESPACE}.svc.cluster.local:9090/metrics | head'" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml"
