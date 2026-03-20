#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-flow-import-registry}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
NIFI_REGISTRY_IMAGE="${NIFI_REGISTRY_IMAGE:-apache/nifi-registry:1.28.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
FLOW_IMPORT_VALUES_FILE="${FLOW_IMPORT_VALUES_FILE:-examples/platform-managed-versioned-flow-import-nifi-registry-values.yaml}"
FLOW_IMPORT_KIND_VALUES_FILE="${FLOW_IMPORT_KIND_VALUES_FILE:-examples/platform-managed-versioned-flow-import-nifi-registry-kind-values.yaml}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"
FLOW_REGISTRY_CLIENT_NAME="${FLOW_REGISTRY_CLIENT_NAME:-nifi-registry-flows}"
SEED_FLOW_REGISTRY_CLIENT_NAME="${SEED_FLOW_REGISTRY_CLIENT_NAME:-nifi-registry-seed}"
SEED_FLOW_REGISTRY_CLIENT_URL="${SEED_FLOW_REGISTRY_CLIENT_URL:-http://nifi-registry.${NAMESPACE}.svc.cluster.local:18080}"

run_make() {
  make -C "${ROOT_DIR}" "$@"
}

retry_proof() {
  local description="$1"
  shift

  local deadline=$(( $(date +%s) + 240 ))
  while true; do
    if "$@"; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${description}" >&2
      return 1
    fi
    sleep 5
  done
}

status_file_matches_version() {
  local import_name="$1"
  local expected_version="$2"
  local status_json

  status_json="$(
    kubectl -n "${NAMESPACE}" exec -i "${HELM_RELEASE}-0" -c nifi -- \
      cat /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json
  )"

  python3 - "${import_name}" "${expected_version}" "${status_json}" <<'PY'
import json
import sys

import_name = sys.argv[1]
expected_version = sys.argv[2]
payload = json.loads(sys.argv[3])

if payload.get("status") != "ok":
    raise SystemExit(1)

entry = None
for candidate in payload.get("imports", []):
    if candidate.get("name") == import_name:
        entry = candidate
        break

if not entry or entry.get("status") != "ok":
    raise SystemExit(1)

actual_version = str(entry.get("actualVersion") or "")
resolved_version = str(entry.get("resolvedVersion") or "")
selected_version = str(entry.get("selectedVersion") or "")

if selected_version != "latest":
    raise SystemExit(1)

if actual_version != expected_version or resolved_version != expected_version:
    raise SystemExit(1)
PY
}

configure_kind_kubeconfig() {
  local kubeconfig_path="${TMPDIR:-/tmp}/${KIND_CLUSTER_NAME}.kubeconfig"
  kind get kubeconfig --name "${KIND_CLUSTER_NAME}" >"${kubeconfig_path}"
  export KUBECONFIG="${kubeconfig_path}"
}

mint_nifi_token() {
  local username password pod host base_url

  username="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.username}' | base64 -d)"
  password="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.password}' | base64 -d)"
  pod="${HELM_RELEASE}-0"
  host="nifi-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local"
  base_url="https://${host}:8443/nifi-api"

  kubectl -n "${NAMESPACE}" exec -i -c nifi "${pod}" -- env \
    NIFI_USERNAME="${username}" \
    NIFI_PASSWORD="${password}" \
    NIFI_BASE_URL="${base_url}" \
    sh -ec '
      curl --silent --show-error --fail \
        --cacert /opt/nifi/tls/ca.crt \
        -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
        --data-urlencode "username=${NIFI_USERNAME}" \
        --data-urlencode "password=${NIFI_PASSWORD}" \
        "${NIFI_BASE_URL}/access/token"
    '
}

nifi_request() {
  local token="$1"
  local method="$2"
  local path="$3"
  local body="${4:-}"
  local pod host base_url

  pod="${HELM_RELEASE}-0"
  host="nifi-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local"
  base_url="https://${host}:8443/nifi-api"

  kubectl -n "${NAMESPACE}" exec -i -c nifi "${pod}" -- env \
    NIFI_BASE_URL="${base_url}" \
    NIFI_TOKEN="${token}" \
    REQUEST_METHOD="${method}" \
    REQUEST_PATH="${path}" \
    REQUEST_BODY="${body}" \
    sh -ec '
      if [ -n "${REQUEST_BODY}" ]; then
        curl --silent --show-error --fail \
          --cacert /opt/nifi/tls/ca.crt \
          -H "Authorization: Bearer ${NIFI_TOKEN}" \
          -H "Content-Type: application/json" \
          -X "${REQUEST_METHOD}" \
          --data "${REQUEST_BODY}" \
          "${NIFI_BASE_URL}${REQUEST_PATH}"
      else
        curl --silent --show-error --fail \
          --cacert /opt/nifi/tls/ca.crt \
          -H "Authorization: Bearer ${NIFI_TOKEN}" \
          -X "${REQUEST_METHOD}" \
          "${NIFI_BASE_URL}${REQUEST_PATH}"
      fi
    '
}

nifi_request_retry() {
  local token="$1"
  local method="$2"
  local path="$3"
  local attempts="${4:-20}"
  local sleep_seconds="${5:-2}"
  local attempt

  for attempt in $(seq 1 "${attempts}"); do
    if nifi_request "${token}" "${method}" "${path}"; then
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  return 1
}

resolve_flow_versions() {
  local client_name="$1"
  local bucket_name="$2"
  local flow_name="$3"
  local token registries_json registry_id buckets_json bucket_id flows_json flow_id versions_json

  token="$(mint_nifi_token)"

  registries_json="$(nifi_request_retry "${token}" GET /flow/registries 20 2)"
  registry_id="$(
    python3 - "${client_name}" "${registries_json}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[2])
target = sys.argv[1]
for entry in payload.get("registries", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        print(entry.get("id") or component.get("id") or "")
        break
PY
  )"
  if [[ -z "${registry_id}" ]]; then
    echo "failed to resolve registry id for ${client_name}" >&2
    return 1
  fi

  buckets_json="$(nifi_request_retry "${token}" GET "/flow/registries/${registry_id}/buckets" 20 2)"
  bucket_id="$(
    python3 - "${bucket_name}" "${buckets_json}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[2])
target = sys.argv[1]
entries = payload.get("bucketResults") or payload.get("buckets") or []
for entry in entries:
    candidate = entry.get("bucket") or entry
    name = candidate.get("name") or ""
    identifier = candidate.get("identifier") or candidate.get("id") or ""
    if name == target and identifier:
        print(identifier)
        break
PY
  )"
  if [[ -z "${bucket_id}" ]]; then
    echo "failed to resolve bucket id for ${bucket_name}" >&2
    return 1
  fi

  flows_json="$(nifi_request_retry "${token}" GET "/flow/registries/${registry_id}/buckets/${bucket_id}/flows" 20 2)"
  flow_id="$(
    python3 - "${flow_name}" "${flows_json}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[2])
target = sys.argv[1]
if isinstance(payload, list):
    flows = payload
else:
    flows = payload.get("bucketFlowResults") or payload.get("versionedFlows") or payload.get("flows") or []
for entry in flows:
    candidate = entry.get("flow") or entry.get("versionedFlow") or entry
    if (candidate.get("name") or candidate.get("flowName") or "") == target:
        print(candidate.get("identifier") or candidate.get("flowId") or candidate.get("id") or "")
        break
PY
  )"
  if [[ -z "${flow_id}" ]]; then
    echo "failed to resolve flow id for ${flow_name}" >&2
    return 1
  fi

  versions_json="$(nifi_request_retry "${token}" GET "/flow/registries/${registry_id}/buckets/${bucket_id}/flows/${flow_id}/versions" 20 2)"
  python3 - "${versions_json}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
entries = []
sources = []
if isinstance(payload.get("versionedFlowSnapshotMetadataSet"), list):
    sources.append({"items": payload["versionedFlowSnapshotMetadataSet"]})
if isinstance(payload.get("versionedFlowSnapshotMetadataSet"), dict):
    sources.append(payload["versionedFlowSnapshotMetadataSet"])
if isinstance(payload.get("versionedFlowSnapshotMetadata"), list):
    sources.append({"items": payload["versionedFlowSnapshotMetadata"]})
if isinstance(payload.get("versionedFlowSnapshots"), list):
    sources.append({"items": payload["versionedFlowSnapshots"]})
if isinstance(payload.get("versionedFlowSnapshot"), dict):
    sources.append({"items": [payload["versionedFlowSnapshot"]]})

for source in sources:
    items = source.get("versionedFlowSnapshotMetadata") or source.get("items") or []
    for index, item in enumerate(items):
        candidate = item.get("versionedFlowSnapshotMetadata") or item.get("snapshotMetadata") or item
        version = candidate.get("version")
        if version is None:
            continue
        timestamp = candidate.get("timestamp")
        if not isinstance(timestamp, int):
            timestamp = -1
        entries.append((str(version), timestamp, index))

ordered = sorted({entry[0]: entry for entry in entries}.values(), key=lambda entry: (entry[1], entry[2], entry[0]))
if not ordered:
    raise SystemExit("no flow versions discovered")
print(ordered[0][0])
print(ordered[-1][0])
PY
}

dump_diagnostics() {
  set +e
  echo
  echo "==> versioned flow import NiFi Registry diagnostics after failure"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,service,configmap,secret || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 120 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --tail=400 || true
  kubectl -n "${NAMESPACE}" logs deployment/nifi-registry --tail=200 || true
  kubectl -n "${NAMESPACE}" exec -i "${HELM_RELEASE}-0" -c nifi -- cat /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
)

if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
fi

helm_values_args+=(
  -f "${ROOT_DIR}/${FLOW_IMPORT_VALUES_FILE}"
  -f "${ROOT_DIR}/${FLOW_IMPORT_KIND_VALUES_FILE}"
)

phase "Checking prerequisites"
check_prereqs
require_command curl
require_command jq
require_command python3
require_command docker

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for runtime-managed NiFi Registry import proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for runtime-managed NiFi Registry import proof"
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
  configure_kind_kubeconfig

  phase "Loading NiFi image into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Loading NiFi Registry image into kind"
  docker pull "${NIFI_REGISTRY_IMAGE}"
  bash "${ROOT_DIR}/hack/load-kind-image.sh" "${KIND_CLUSTER_NAME}" "${NIFI_REGISTRY_IMAGE}"

  phase "Creating TLS and auth Secrets"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
fi

phase "Deploying Apache NiFi Registry compatibility service"
NAMESPACE="${NAMESPACE}" IMAGE="${NIFI_REGISTRY_IMAGE}" bash "${ROOT_DIR}/hack/deploy-nifi-registry.sh"

phase "Installing product chart managed release with bounded NiFi Registry import mounted from the start at explicit version 1"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}" \
  --set-string "nifi.versionedFlowImports.imports[0].version=1"

phase "Verifying platform resources and controller rollout"
kubectl get crd nificlusters.platform.nifi.io >/dev/null
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

phase "Verifying initial cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

phase "Verifying bounded import bundle is mounted on pod ${HELM_RELEASE}-0"
kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- test -f /opt/nifi/fabric/versioned-flow-imports/config.json
kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- test -f /opt/nifi/fabric/versioned-flow-imports/bootstrap.py

phase "Proving runtime-managed Parameter Context prerequisite"
bash "${ROOT_DIR}/hack/prove-parameter-contexts-runtime.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --auth-secret nifi-auth \
  --context-name payments-runtime \
  --expected-inline-parameter payments.api.baseUrl \
  --expected-inline-value https://payments.internal.example.com \
  --expected-sensitive-parameter "" \
  --expected-action created,unchanged,updated

phase "Creating a seed-only live NiFi Registry client and seeding the first selected versioned flow"
bash "${ROOT_DIR}/hack/prove-nifi-registry-workflow.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --auth-secret nifi-auth \
  --client-name "${SEED_FLOW_REGISTRY_CLIENT_NAME}" \
  --registry-url "${SEED_FLOW_REGISTRY_CLIENT_URL}" \
  --workflow-bucket team-a \
  --workflow-flow-name payments-api \
  --workflow-process-group-name "flow-import-seed-$(date +%s)"

phase "Saving a second version of the selected flow to NiFi Registry"
bash "${ROOT_DIR}/hack/prove-nifi-registry-workflow.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --auth-secret nifi-auth \
  --client-name "${SEED_FLOW_REGISTRY_CLIENT_NAME}" \
  --registry-url "${SEED_FLOW_REGISTRY_CLIENT_URL}" \
  --workflow-bucket team-a \
  --workflow-flow-name payments-api \
  --workflow-process-group-name "flow-import-seed-update-$(date +%s)"

phase "Resolving the seeded explicit and latest flow versions"
mapfile -t flow_versions < <(resolve_flow_versions "${SEED_FLOW_REGISTRY_CLIENT_NAME}" team-a payments-api)
initial_flow_version="${flow_versions[0]:-}"
latest_flow_version="${flow_versions[1]:-}"
if [[ -z "${initial_flow_version}" || -z "${latest_flow_version}" ]]; then
  echo "failed to resolve seeded flow versions for payments-api" >&2
  exit 1
fi

phase "Proving the declared bounded NiFi Registry Flow Registry Client is created and resolvable by the product-owned path"
retry_proof "bounded NiFi Registry Flow Registry Client creation" \
  bash "${ROOT_DIR}/hack/prove-nifi-registry-flow-registry-client.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --client-name "${FLOW_REGISTRY_CLIENT_NAME}" \
    --expect-bucket team-a

phase "Proving runtime-managed bounded NiFi Registry flow import at explicit version ${initial_flow_version}"
retry_proof "runtime-managed bounded NiFi Registry flow import creation" \
  bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --import-name payments-api \
    --expected-action created,updated,unchanged

phase "Updating the declared versioned flow import back to latest (${latest_flow_version}) without replacing pod ${HELM_RELEASE}-0"
previous_pod_uid="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.metadata.uid}')"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

current_pod_uid="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.metadata.uid}')"
if [[ "${current_pod_uid}" != "${previous_pod_uid}" ]]; then
  echo "expected live versioned flow import update to reconcile without replacing pod ${HELM_RELEASE}-0" >&2
  exit 1
fi

phase "Proving live bounded version update to latest ${latest_flow_version}"
retry_proof "runtime-managed bounded NiFi Registry flow import live update" \
  bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --import-name payments-api \
    --expected-action updated,unchanged

phase "Waiting for bounded latest-version convergence to version ${latest_flow_version}"
retry_proof "runtime-managed bounded NiFi Registry flow import latest version convergence" \
  status_file_matches_version payments-api "${latest_flow_version}"

print_success_footer "platform chart runtime-managed NiFi Registry import proof completed" \
  "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
  "kubectl -n ${NAMESPACE} exec -i ${HELM_RELEASE}-0 -c nifi -- cat /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get pods,svc,configmap,secret"
