#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-nifidataflow}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
FLOW_IMPORT_VALUES_FILE="${FLOW_IMPORT_VALUES_FILE:-examples/platform-managed-versioned-flow-import-values.yaml}"
FLOW_IMPORT_KIND_VALUES_FILE="${FLOW_IMPORT_KIND_VALUES_FILE:-examples/platform-managed-versioned-flow-import-kind-values.yaml}"
NIFIDATAFLOW_VALUES_FILE="${NIFIDATAFLOW_VALUES_FILE:-examples/platform-managed-nifidataflow-values.yaml}"
FLOW_REGISTRY_SECRET_NAME="${FLOW_REGISTRY_SECRET_NAME:-github-flow-registry}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"

PRIMARY_DATAFLOW_NAME="${PRIMARY_DATAFLOW_NAME:-payments-api-primary}"
PRIMARY_TARGET_NAME="${PRIMARY_TARGET_NAME:-payments-api-primary-root}"
RETAINED_DATAFLOW_NAME="${RETAINED_DATAFLOW_NAME:-payments-api-retained}"
RETAINED_TARGET_NAME="${RETAINED_TARGET_NAME:-payments-api-retained-root}"
MANUAL_DATAFLOW_NAME="${MANUAL_DATAFLOW_NAME:-payments-api-manual}"
MANUAL_TARGET_NAME="${MANUAL_TARGET_NAME:-payments-api-manual-root}"
REGISTRY_CLIENT_NAME="${REGISTRY_CLIENT_NAME:-github-flows}"
PARAMETER_CONTEXT_NAME="${PARAMETER_CONTEXT_NAME:-payments-runtime}"
WORKFLOW_BUCKET_NAME="${WORKFLOW_BUCKET_NAME:-team-a}"
WORKFLOW_FLOW_NAME="${WORKFLOW_FLOW_NAME:-payments-api}"
BRIDGE_IMPORTS_CONFIGMAP_NAME="${BRIDGE_IMPORTS_CONFIGMAP_NAME:-${HELM_RELEASE}-nifidataflows}"
BRIDGE_IMPORTS_MOUNT_PATH="${BRIDGE_IMPORTS_MOUNT_PATH:-/opt/nifi/fabric/nifidataflows/imports.json}"

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

run_make() {
  make -C "${ROOT_DIR}" "$@"
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

retry_proof() {
  local description="$1"
  shift

  local deadline=$(( $(date +%s) + 300 ))
  local attempt_output
  attempt_output="$(mktemp)"
  while true; do
    if "$@" >"${attempt_output}" 2>&1; then
      cat "${attempt_output}"
      rm -f "${attempt_output}"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      cat "${attempt_output}" >&2
      rm -f "${attempt_output}"
      echo "timed out waiting for ${description}" >&2
      return 1
    fi
    sleep 5
  done
}

configure_kind_kubeconfig() {
  local kubeconfig_path="${TMPDIR:-/tmp}/${KIND_CLUSTER_NAME}.kubeconfig"
  kind get kubeconfig --name "${KIND_CLUSTER_NAME}" >"${kubeconfig_path}"
  export KUBECONFIG="${kubeconfig_path}"
}

dump_diagnostics() {
  set +e
  echo
  echo "==> NiFiDataflow e2e diagnostics after failure"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,nifidataflow,statefulset,pod,secret,configmap || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 120 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --tail=400 || true
  kubectl -n "${NAMESPACE}" logs deployment/github-mock --tail=200 || true
  kubectl -n "${NAMESPACE}" get nifidataflow "${PRIMARY_DATAFLOW_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nifidataflow "${RETAINED_DATAFLOW_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nifidataflow "${MANUAL_DATAFLOW_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" exec -i "${HELM_RELEASE}-0" -c nifi -- cat /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json || true
  kubectl -n "${NAMESPACE}" get configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get configmap "${HELM_RELEASE}-nifidataflows-status" -o yaml || true

  capture_cmd current-context kubectl config current-context
  capture_cmd helm-status helm -n "${NAMESPACE}" status "${HELM_RELEASE}"
  capture_cmd controller-workloads kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod -o wide
  capture_cmd namespace-workloads kubectl -n "${NAMESPACE}" get nificluster,nifidataflow,statefulset,pod,secret,configmap -o wide
  capture_cmd namespace-events sh -ec "kubectl -n \"${NAMESPACE}\" get events --sort-by=.lastTimestamp | tail -n 200"
  capture_cmd controller-logs kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=500
  capture_cmd nifi-logs kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --tail=600
  capture_cmd github-mock-logs kubectl -n "${NAMESPACE}" logs deployment/github-mock --tail=300
  capture_cmd primary-dataflow kubectl -n "${NAMESPACE}" get nifidataflow "${PRIMARY_DATAFLOW_NAME}" -o yaml
  capture_cmd retained-dataflow kubectl -n "${NAMESPACE}" get nifidataflow "${RETAINED_DATAFLOW_NAME}" -o yaml
  capture_cmd manual-dataflow kubectl -n "${NAMESPACE}" get nifidataflow "${MANUAL_DATAFLOW_NAME}" -o yaml
  capture_cmd runtime-status-file kubectl -n "${NAMESPACE}" exec -i "${HELM_RELEASE}-0" -c nifi -- cat /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json
  capture_cmd bridge-imports-configmap kubectl -n "${NAMESPACE}" get configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" -o yaml
  capture_cmd bridge-status-configmap kubectl -n "${NAMESPACE}" get configmap "${HELM_RELEASE}-nifidataflows-status" -o yaml
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

is_transient_nifi_exec_error() {
  local message="$1"
  [[ "${message}" == *'container not found ("nifi")'* ]] \
    || [[ "${message}" == *"unable to upgrade connection"* ]] \
    || [[ "${message}" == *"connection refused"* ]] \
    || [[ "${message}" == *"Connection refused"* ]] \
    || [[ "${message}" == *"Couldn't connect to server"* ]] \
    || [[ "${message}" == *"Failed to connect to "* ]] \
    || [[ "${message}" == *"Flow Controller is initializing the Data Flow."* ]] \
    || [[ "${message}" == *"the server doesn't have a resource type"* ]]
}

is_transient_nifi_api_error() {
  local status="$1"
  local body="$2"
  [[ "${body}" == *"Flow Controller is initializing the Data Flow."* ]] \
    || [[ "${body}" == *"no nodes are connected"* ]] \
    || [[ "${status}" == "409" && "${body}" == *"initializing"* ]] \
    || [[ "${status}" == "409" && "${body}" == *"unable to fulfill this request due to: Unexpected Response Code 500"* ]]
}

wait_for_nifi_exec_ready() {
  local pod="${HELM_RELEASE}-0"
  local deadline=$(( $(date +%s) + 300 ))

  kubectl -n "${NAMESPACE}" wait --for=condition=Ready "pod/${pod}" --timeout=10m >/dev/null

  while true; do
    if kubectl -n "${NAMESPACE}" get pod "${pod}" -o json >"${tmpdir}/nifi-pod.json" 2>/dev/null && \
      python3 - "${tmpdir}/nifi-pod.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload.get("metadata", {}).get("deletionTimestamp"):
    raise SystemExit(1)

for status in payload.get("status", {}).get("containerStatuses") or []:
    if status.get("name") != "nifi":
        continue
    state = status.get("state") or {}
    if status.get("ready") and isinstance(state.get("running"), dict):
        raise SystemExit(0)
    raise SystemExit(1)

raise SystemExit(1)
PY
    then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for ${pod} nifi container to become exec-ready" >&2
      return 1
    fi
    sleep 3
  done
}

nifi_exec() {
  local output=""
  local attempt

  for attempt in $(seq 1 20); do
    wait_for_nifi_exec_ready || return 1
    if output="$(kubectl -n "${NAMESPACE}" exec -i "${HELM_RELEASE}-0" -c nifi -- "$@" 2>&1)"; then
      printf '%s' "${output}"
      return 0
    fi
    if is_transient_nifi_exec_error "${output}"; then
      sleep 3
      continue
    fi
    printf '%s\n' "${output}" >&2
    return 1
  done

  printf '%s\n' "${output}" >&2
  echo "timed out waiting for a stable exec path into ${HELM_RELEASE}-0/nifi" >&2
  return 1
}

mint_nifi_token() {
  local username password pod host base_url
  local token=""
  local attempt
  username="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.username}' | base64 -d)"
  password="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.password}' | base64 -d)"
  pod="${HELM_RELEASE}-0"
  host="nifi-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local"
  base_url="https://${host}:8443/nifi-api"

  for attempt in $(seq 1 20); do
    if token="$(
      nifi_exec env \
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
    2>&1)"; then
      printf '%s' "${token}"
      return 0
    fi
    if is_transient_nifi_exec_error "${token}"; then
      sleep 3
      continue
    fi
    printf '%s\n' "${token}" >&2
    return 1
  done

  printf '%s\n' "${token}" >&2
  echo "timed out waiting for NiFi API token minting through direct pod HTTPS" >&2
  return 1
}

nifi_request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local token pod host base_url response status
  local attempt

  token="$(mint_nifi_token)"
  pod="${HELM_RELEASE}-0"
  host="nifi-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local"
  base_url="https://${host}:8443/nifi-api"

  for attempt in $(seq 1 20); do
    response="$(
      nifi_exec env \
        NIFI_BASE_URL="${base_url}" \
        NIFI_TOKEN="${token}" \
        REQUEST_METHOD="${method}" \
        REQUEST_PATH="${path}" \
        REQUEST_BODY="${body}" \
        sh -ec '
          if [ -n "${REQUEST_BODY}" ]; then
            curl --silent --show-error \
              --cacert /opt/nifi/tls/ca.crt \
              -H "Authorization: Bearer ${NIFI_TOKEN}" \
              -H "Content-Type: application/json" \
              -X "${REQUEST_METHOD}" \
              --data "${REQUEST_BODY}" \
              --write-out "\n%{http_code}" \
              "${NIFI_BASE_URL}${REQUEST_PATH}"
          else
            curl --silent --show-error \
              --cacert /opt/nifi/tls/ca.crt \
              -H "Authorization: Bearer ${NIFI_TOKEN}" \
              -X "${REQUEST_METHOD}" \
              --write-out "\n%{http_code}" \
              "${NIFI_BASE_URL}${REQUEST_PATH}"
          fi
        '
    )"

    status="${response##*$'\n'}"
    LAST_HTTP_BODY="${response%$'\n'*}"
    LAST_HTTP_STATUS="${status}"

    if [[ "${status}" =~ ^2 ]]; then
      printf '%s' "${LAST_HTTP_BODY}"
      return 0
    fi
    if is_transient_nifi_api_error "${status}" "${LAST_HTTP_BODY}"; then
      sleep 3
      continue
    fi
    echo "NiFi API ${method} ${path} returned HTTP ${status}" >&2
    printf '%s\n' "${LAST_HTTP_BODY}" >&2
    return 1
  done

  echo "NiFi API ${method} ${path} kept returning transient errors after repeated retries" >&2
  printf '%s\n' "${LAST_HTTP_BODY}" >&2
  return 1
}

create_root_process_group() {
  local target_name="$1"
  nifi_request GET /flow/process-groups/root >"${tmpdir}/root-flow-before-create.json"
  if python3 - "${tmpdir}/root-flow-before-create.json" "${target_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
for entry in payload.get("processGroupFlow", {}).get("flow", {}).get("processGroups", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        raise SystemExit(0)
raise SystemExit(1)
PY
  then
    return 0
  fi

  python3 - "${target_name}" >"${tmpdir}/create-process-group.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "nifidataflow-e2e",
        "version": 0,
    },
    "component": {
        "name": sys.argv[1],
        "position": {"x": 320.0, "y": 320.0},
    },
}
json.dump(payload, sys.stdout)
PY

  nifi_request POST /process-groups/root/process-groups "$(tr -d '\n' < "${tmpdir}/create-process-group.json")" >/dev/null
}

root_process_group_exists() {
  local target_name="$1"
  nifi_request GET /flow/process-groups/root >"${tmpdir}/root-flow-check.json"
  python3 - "${tmpdir}/root-flow-check.json" "${target_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
for entry in payload.get("processGroupFlow", {}).get("flow", {}).get("processGroups", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        raise SystemExit(0)
raise SystemExit(1)
PY
}

resolve_root_process_group_id() {
  local target_name="$1"
  nifi_request GET /flow/process-groups/root >"${tmpdir}/root-flow-resolve.json"
  python3 - "${tmpdir}/root-flow-resolve.json" "${target_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
for entry in payload.get("processGroupFlow", {}).get("flow", {}).get("processGroups", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        identifier = entry.get("id") or component.get("id") or ""
        if identifier:
            print(identifier)
            raise SystemExit(0)
raise SystemExit(1)
PY
}

set_root_process_group_state() {
  local target_name="$1"
  local state="$2"
  local process_group_id

  process_group_id="$(resolve_root_process_group_id "${target_name}")"
  python3 - "${process_group_id}" "${state}" >"${tmpdir}/set-process-group-state.json" <<'PY'
import json
import sys

payload = {
    "id": sys.argv[1],
    "state": sys.argv[2],
    "disconnectedNodeAcknowledged": True,
}
json.dump(payload, sys.stdout)
PY

  nifi_request PUT "/flow/process-groups/${process_group_id}" "$(tr -d '\n' < "${tmpdir}/set-process-group-state.json")" >/dev/null
}

root_process_group_running() {
  local target_name="$1"
  local process_group_id
  process_group_id="$(resolve_root_process_group_id "${target_name}")"

  nifi_request GET "/process-groups/${process_group_id}" >"${tmpdir}/process-group-running.json"
  nifi_request GET "/flow/process-groups/${process_group_id}" >"${tmpdir}/process-group-flow-running.json"
  python3 - "${tmpdir}/process-group-running.json" "${tmpdir}/process-group-flow-running.json" <<'PY'
import json
import sys

component_payload = json.load(open(sys.argv[1], encoding="utf-8"))
flow_payload = json.load(open(sys.argv[2], encoding="utf-8"))

direct_candidates = [
    component_payload,
    component_payload.get("component", {}) if isinstance(component_payload.get("component"), dict) else {},
]
flow = flow_payload.get("processGroupFlow", {}).get("flow", {}) if isinstance(flow_payload, dict) else {}
for entry in flow.get("processors", []) or []:
    if not isinstance(entry, dict):
        continue
    component = entry.get("component", {}) if isinstance(entry.get("component"), dict) else {}
    status = entry.get("status", {}).get("aggregateSnapshot", {}) if isinstance(entry.get("status"), dict) else {}
    direct_candidates.extend((component, status))

running = 0
for source in direct_candidates:
    if not isinstance(source, dict):
        continue
    value = source.get("runningCount")
    if value is not None:
        try:
            running = max(running, int(value))
        except Exception:
            pass
    for key in ("state", "runStatus", "scheduledState", "physicalState"):
        if str(source.get(key) or "").upper() == "RUNNING":
            running = max(running, 1)

raise SystemExit(0 if running > 0 else 1)
PY
}

start_root_process_group_and_wait() {
  local target_name="$1"
  set_root_process_group_state "${target_name}" RUNNING
  retry_proof "root process group ${target_name} reports running descendants on process-group endpoints" \
    root_process_group_running "${target_name}"
  retry_proof "root process group ${target_name} remains running on a second poll" \
    root_process_group_running "${target_name}"
}

resolve_flow_registry_id() {
  nifi_request GET /flow/registries >"${tmpdir}/flow-registries.json"
  python3 - "${tmpdir}/flow-registries.json" "${REGISTRY_CLIENT_NAME}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
for entry in payload.get("registries", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        identifier = entry.get("id") or component.get("id") or ""
        if identifier:
            print(identifier)
            raise SystemExit(0)
raise SystemExit(1)
PY
}

resolve_flow_registry_flow_id() {
  local bucket_name="$1"
  local flow_name="$2"
  local registry_id

  registry_id="$(resolve_flow_registry_id)"
  nifi_request GET "/flow/registries/${registry_id}/buckets/${bucket_name}/flows" >"${tmpdir}/bucket-flows.json"
  python3 - "${tmpdir}/bucket-flows.json" "${flow_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
flows = payload.get("bucketFlowResults") or payload.get("versionedFlows") or payload.get("flows") or []
for entry in flows:
    candidate = entry.get("flow") or entry.get("versionedFlow") or entry
    name = candidate.get("name") or candidate.get("flowName") or ""
    identifier = candidate.get("identifier") or candidate.get("flowId") or candidate.get("id") or ""
    if name == target and identifier:
        print(identifier)
        raise SystemExit(0)
raise SystemExit(1)
PY
}

save_mock_flow_version() {
  local bucket_name="$1"
  local flow_name="$2"
  local flow_id

  flow_id="$(resolve_flow_registry_flow_id "${bucket_name}" "${flow_name}")"

  kubectl -n "${NAMESPACE}" exec -i deployment/github-mock -c mock -- python3 - "${bucket_name}" "${flow_id}" <<'PY'
import base64
import json
import time
import sys
import urllib.parse
import urllib.request

bucket = sys.argv[1]
flow_id = sys.argv[2]
path = f"flows/{bucket}/{flow_id}.json"
base = f"http://127.0.0.1:8080/repos/example-org/nifi-flows/contents/{path}"
headers = {"Authorization": "Bearer dummytoken"}

get_request = urllib.request.Request(
    base + "?ref=" + urllib.parse.quote("refs/heads/main", safe=""),
    headers=headers,
    method="GET",
)
with urllib.request.urlopen(get_request, timeout=20) as response:
    existing = json.loads(response.read().decode("utf-8"))

content = base64.b64decode(existing["content"]).decode("utf-8")
payload_json = json.loads(content)
suffix = str(int(time.time()))

labels = ((payload_json.get("flowContents") or {}).get("labels")) or []
if labels:
    first_label = labels[0]
    base_label = str(first_label.get("label") or "payments import seed").split(" @ update ", 1)[0]
    first_label["label"] = f"{base_label} @ update {suffix}"
else:
    (payload_json.setdefault("flowContents", {})).setdefault("labels", []).append({
        "componentType": "LABEL",
        "groupIdentifier": payload_json.get("flowContents", {}).get("identifier", "flow-contents-group"),
        "height": 40.0,
        "identifier": f"generated-label-{suffix}",
        "label": f"payments import seed @ update {suffix}",
        "position": {"x": 180.0, "y": 180.0},
        "style": {"background-color": "#fff4bf"},
        "width": 320.0,
        "zIndex": 0,
    })

flow = payload_json.setdefault("flow", {})
flow["lastModifiedTimestamp"] = int(time.time() * 1000)
snapshot_metadata = payload_json.setdefault("snapshotMetadata", {})
snapshot_metadata["timestamp"] = int(time.time() * 1000)

updated_content = json.dumps(payload_json, indent=2, sort_keys=True)
payload = {
    "message": f"Update {path} for NiFiDataflow e2e",
    "content": base64.b64encode(updated_content.encode("utf-8")).decode("utf-8"),
    "sha": existing["sha"],
    "branch": "main",
}
put_request = urllib.request.Request(
    base,
    headers={**headers, "Content-Type": "application/json"},
    data=json.dumps(payload).encode("utf-8"),
    method="PUT",
)
with urllib.request.urlopen(put_request, timeout=20) as response:
    updated = json.loads(response.read().decode("utf-8"))

print(json.dumps({"path": path, "commit": updated.get("commit", {}).get("sha", "")}, indent=2, sort_keys=True))
PY
}

mock_flow_exists() {
  local bucket_name="$1"
  local flow_name="$2"
  local flow_id

  flow_id="$(resolve_flow_registry_flow_id "${bucket_name}" "${flow_name}")"

  kubectl -n "${NAMESPACE}" exec -i deployment/github-mock -c mock -- python3 - "${bucket_name}" "${flow_id}" <<'PY'
import json
import sys
import urllib.request

bucket = sys.argv[1]
flow_id = sys.argv[2]
with urllib.request.urlopen("http://127.0.0.1:8080/debug/state", timeout=20) as response:
    state = json.loads(response.read().decode("utf-8"))

path = f"flows/{bucket}/{flow_id}.json"
raise SystemExit(0 if path in (state.get("files") or {}) else 1)
PY
}

flow_versions_diverged() {
  local bucket_name="$1"
  local flow_name="$2"
  local flow_version_output
  local -a flow_versions=()

  flow_version_output="$(resolve_flow_versions "${bucket_name}" "${flow_name}")"
  mapfile -t flow_versions <<<"${flow_version_output}"

  [[ -n "${flow_versions[0]:-}" ]] || return 1
  [[ -n "${flow_versions[1]:-}" ]] || return 1
  [[ "${flow_versions[0]}" != "${flow_versions[1]}" ]]
}

runtime_imports_do_not_report_missing_registry_client() {
  local status_configmap_name="${HELM_RELEASE}-nifidataflows-status"

  kubectl -n "${NAMESPACE}" get configmap "${status_configmap_name}" -o jsonpath='{.data.status\.json}' >"${tmpdir}/bridge-status.json"
  python3 - "${tmpdir}/bridge-status.json" "${REGISTRY_CLIENT_NAME}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
client_name = sys.argv[2]
imports = payload.get("imports")
if not isinstance(imports, list) or not imports:
    raise SystemExit(1)
for entry in payload.get("imports", []):
    reason = str(entry.get("reason") or "")
    if client_name in reason and "does not exist in NiFi" in reason:
        raise SystemExit(1)
raise SystemExit(0)
PY
}

runtime_import_status_matches() {
  local import_name="$1"
  local expected_selected_version="$2"
  local status_configmap_name="${HELM_RELEASE}-nifidataflows-status"

  kubectl -n "${NAMESPACE}" get configmap "${status_configmap_name}" -o jsonpath='{.data.status\.json}' >"${tmpdir}/bridge-status.json"
  python3 - "${tmpdir}/bridge-status.json" "${import_name}" "${expected_selected_version}" "${REGISTRY_CLIENT_NAME}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
expected_selected_version = sys.argv[3]
registry_client_name = sys.argv[4]

imports = payload.get("imports")
if not isinstance(imports, list) or not imports:
    raise SystemExit(1)

entry = None
for candidate in imports:
    if str(candidate.get("name") or "") == target_name:
        entry = candidate
        break

if entry is None:
    raise SystemExit(1)

reason = str(entry.get("reason") or "")
if registry_client_name in reason and "does not exist in NiFi" in reason:
    raise SystemExit(1)

if expected_selected_version and str(entry.get("selectedVersion") or "") != expected_selected_version:
    raise SystemExit(1)

raise SystemExit(0)
PY
}

runtime_import_prereqs_ready() {
  local import_name="$1"
  local expected_selected_version="$2"

  ensure_live_flow_registry_client
  trigger_versioned_flow_import_runtime >/dev/null
  runtime_imports_do_not_report_missing_registry_client
  runtime_import_status_matches "${import_name}" "${expected_selected_version}"
}

apply_dataflow() {
  local name="$1"
  local version="$2"
  local target_name="$3"
  local suspend="${4:-false}"
  local parameter_context_name="${5:-${PARAMETER_CONTEXT_NAME}}"
  local sync_mode="${6:-OnChange}"
  local rollout_strategy="${7:-DrainAndReplace}"
  local rollout_timeout="${8:-20m}"

  cat >"${tmpdir}/${name}.yaml" <<EOF
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiDataflow
metadata:
  name: ${name}
  namespace: ${NAMESPACE}
spec:
  clusterRef:
    name: ${HELM_RELEASE}
  source:
    registryClient:
      name: ${REGISTRY_CLIENT_NAME}
    bucket: ${WORKFLOW_BUCKET_NAME}
    flow: ${WORKFLOW_FLOW_NAME}
    version: "${version}"
  target:
    rootChildProcessGroupName: ${target_name}
    parameterContextRef:
      name: ${parameter_context_name}
  rollout:
    strategy: ${rollout_strategy}
    timeout: ${rollout_timeout}
  syncPolicy:
    mode: ${sync_mode}
  suspend: ${suspend}
EOF

  kubectl apply -f "${tmpdir}/${name}.yaml"
}

manually_update_imported_process_group_version() {
  local target_name="$1"
  local selected_version="$2"

  nifi_exec env \
    TARGET_ROOT_PROCESS_GROUP_NAME="${target_name}" \
    TARGET_SELECTED_VERSION="${selected_version}" \
    TARGET_REGISTRY_CLIENT_NAME="${REGISTRY_CLIENT_NAME}" \
    TARGET_BUCKET_NAME="${WORKFLOW_BUCKET_NAME}" \
    TARGET_FLOW_NAME="${WORKFLOW_FLOW_NAME}" \
    python3 - <<'PY'
import importlib.util
import json
import os

spec = importlib.util.spec_from_file_location(
    "versioned_flow_imports_bootstrap",
    "/opt/nifi/fabric/versioned-flow-imports/bootstrap.py",
)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

config = module.load_config()
base_url = f"https://{os.environ['NIFI_POD_FQDN']}:8443/nifi-api"
proxied_identity = module.resolve_proxied_identity(config)
opener = module.build_http_client(config)
module.wait_for_cluster_summary(opener, base_url, proxied_identity)

target_name = os.environ["TARGET_ROOT_PROCESS_GROUP_NAME"]
selected_version = os.environ["TARGET_SELECTED_VERSION"]
registry_name = os.environ["TARGET_REGISTRY_CLIENT_NAME"]
bucket_name = os.environ["TARGET_BUCKET_NAME"]
flow_name = os.environ["TARGET_FLOW_NAME"]

root_children = module.root_child_process_groups(opener, base_url, proxied_identity)
matches = [entry for entry in root_children if entry.get("name") == target_name]
if len(matches) != 1:
    raise SystemExit(f"expected exactly one root child process group named {target_name!r}, got {len(matches)}")

process_group_id = matches[0]["id"]
registry_id = module.flow_registry_ids_by_name(opener, base_url, proxied_identity).get(registry_name, "")
if not registry_id:
    raise SystemExit(f"failed to resolve live Flow Registry Client {registry_name!r}")

bucket = module.resolve_bucket(config, opener, base_url, proxied_identity, registry_id, bucket_name)
bucket_id = bucket["id"]
flow_id = module.resolve_flow_id(opener, base_url, proxied_identity, registry_id, bucket_id, flow_name)
version_entry = module.resolve_version_entry(opener, base_url, proxied_identity, registry_id, bucket_id, flow_id, selected_version)
snapshot = module.selected_snapshot(
    config,
    registry_name,
    registry_id,
    bucket_name,
    bucket_id,
    flow_name,
    flow_id,
    version_entry,
    selected_version,
)
_, component, revision, _, _, _ = module.refresh_process_group_state(opener, base_url, proxied_identity, process_group_id)
preserved_comments = component.get("comments", "") or ""
module.update_imported_process_group_version(
    opener,
    base_url,
    proxied_identity,
    "nifidataflow-e2e-manual-drift",
    process_group_id,
    revision.get("version", 0),
    registry_id,
    snapshot,
)
_, component, revision, _, _, version_control = module.refresh_process_group_state(opener, base_url, proxied_identity, process_group_id)
if (component.get("comments", "") or "") != preserved_comments:
    module.update_process_group_comments(
        opener,
        base_url,
        proxied_identity,
        config,
        process_group_id,
        revision.get("version", 0),
        component.get("name", target_name),
        preserved_comments,
    )
    _, component, _, _, _, version_control = module.refresh_process_group_state(opener, base_url, proxied_identity, process_group_id)
print(
    json.dumps(
        {
            "actualVersion": "" if version_control.get("version") is None else str(version_control.get("version")),
            "processGroupId": process_group_id,
            "targetRootProcessGroupName": component.get("name", ""),
        },
        indent=2,
        sort_keys=True,
    )
)
PY
}

prove_primary_once_drift_skip() {
  local drifted_version="$1"

  trigger_versioned_flow_import_runtime >/dev/null
  bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --imports-configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" \
    --import-name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-resolved-version "${latest_flow_version}" \
    --expected-actual-version "${drifted_version}" \
    --expected-version-drift-reconcile-skipped true
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${drifted_version}" \
    --expect-no-retained-owned-imports
}

mounted_bridge_import_sync_mode_matches() {
  local import_name="$1"
  local expected_mode="$2"

  nifi_exec cat "${BRIDGE_IMPORTS_MOUNT_PATH}" >"${tmpdir}/mounted-bridge-imports.json"
  python3 - "${tmpdir}/mounted-bridge-imports.json" "${import_name}" "${expected_mode}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
expected_mode = sys.argv[3]

imports = payload.get("imports", [])
for entry in imports:
    if str(entry.get("name") or "") != target_name:
        continue
    sync_policy = entry.get("syncPolicy") or {}
    mode = str(sync_policy.get("mode") or "")
    raise SystemExit(0 if mode == expected_mode else 1)

raise SystemExit(1)
PY
}

runtime_import_observed_hash_matches_bridge() {
  local import_name="$1"
  local status_configmap_name="${HELM_RELEASE}-nifidataflows-status"

  nifi_exec cat "${BRIDGE_IMPORTS_MOUNT_PATH}" >"${tmpdir}/mounted-bridge-imports.json"
  kubectl -n "${NAMESPACE}" get configmap "${status_configmap_name}" -o jsonpath='{.data.status\.json}' >"${tmpdir}/bridge-status.json"
  python3 - "${tmpdir}/mounted-bridge-imports.json" "${tmpdir}/bridge-status.json" "${import_name}" <<'PY'
import hashlib
import json
import sys

catalog = json.load(open(sys.argv[1], encoding="utf-8"))
status_payload = json.load(open(sys.argv[2], encoding="utf-8"))
target_name = sys.argv[3]

imports = catalog.get("imports", [])
selected = None
for entry in imports:
    if str(entry.get("name") or "") == target_name:
        selected = entry
        break
if selected is None:
    raise SystemExit(1)

parameter_context_refs = []
for ref in selected.get("parameterContextRefs", []) or []:
    if isinstance(ref, dict) and ref.get("name"):
        parameter_context_refs.append(ref.get("name", ""))

payload = {
    "name": selected["name"],
    "registryClientName": selected["registryClientRef"]["name"],
    "bucket": selected["source"]["bucket"],
    "flowName": selected["source"]["flowName"],
    "version": selected["source"]["version"],
    "targetRootProcessGroupName": selected["target"]["rootProcessGroupName"],
    "syncPolicyMode": ((selected.get("syncPolicy") or {}).get("mode") or "OnChange"),
    "rolloutStrategy": ((selected.get("rollout") or {}).get("strategy") or "DrainAndReplace"),
    "rolloutTimeout": ((selected.get("rollout") or {}).get("timeout") or ""),
    "parameterContextRefs": parameter_context_refs,
}
expected_hash = hashlib.sha256(json.dumps(payload, sort_keys=True).encode("utf-8")).hexdigest()

for entry in status_payload.get("imports", []) or []:
    if str(entry.get("name") or "") != target_name:
        continue
    raise SystemExit(0 if str(entry.get("observedHash") or "") == expected_hash else 1)

raise SystemExit(1)
PY
}

delete_dataflow() {
  local name="$1"
  kubectl -n "${NAMESPACE}" delete nifidataflow "${name}" --wait=true
}

delete_dataflow_if_exists() {
  local name="$1"
  kubectl -n "${NAMESPACE}" delete nifidataflow "${name}" --ignore-not-found --wait=true >/dev/null
}

delete_root_process_group() {
  local target_name="$1"
  local process_group_json process_group_id process_group_version
  local attempt

  for attempt in $(seq 1 5); do
    nifi_request GET /flow/process-groups/root >"${tmpdir}/root-flow-before-delete.json"
    process_group_json="$(
      python3 - "${tmpdir}/root-flow-before-delete.json" "${target_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
for entry in payload.get("processGroupFlow", {}).get("flow", {}).get("processGroups", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        print(json.dumps({
            "id": entry.get("id") or component.get("id") or "",
            "version": (entry.get("revision") or {}).get("version", 0),
        }))
        raise SystemExit(0)
raise SystemExit(1)
PY
    )" || return 0

    process_group_id="$(
      python3 - "${process_group_json}" <<'PY'
import json
import sys
payload = json.loads(sys.argv[1])
print(payload.get("id", ""))
PY
    )"
    process_group_version="$(
      python3 - "${process_group_json}" <<'PY'
import json
import sys
payload = json.loads(sys.argv[1])
print(payload.get("version", 0))
PY
    )"

    if [[ -z "${process_group_id}" ]]; then
      return 0
    fi

    if nifi_request DELETE "/process-groups/${process_group_id}?version=${process_group_version}&clientId=nifidataflow-e2e" >/dev/null; then
      return 0
    fi

    sleep 2
  done

  echo "failed to delete root process group ${target_name} after repeated revision refresh attempts" >&2
  return 1
}

cleanup_e2e_artifacts() {
  delete_dataflow_if_exists "${PRIMARY_DATAFLOW_NAME}"
  delete_dataflow_if_exists "${RETAINED_DATAFLOW_NAME}"
  delete_dataflow_if_exists "${MANUAL_DATAFLOW_NAME}"

  delete_root_process_group "${PRIMARY_TARGET_NAME}"
  delete_root_process_group "${RETAINED_TARGET_NAME}"
  delete_root_process_group "${MANUAL_TARGET_NAME}"
}

reset_runtime_status_artifacts() {
  nifi_exec \
    sh -ec 'rm -f /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json'
}

ensure_live_flow_registry_client() {
  local github_token
  local ensure_output
  local attempt
  github_token="$(kubectl -n "${NAMESPACE}" get secret "${FLOW_REGISTRY_SECRET_NAME}" -o jsonpath='{.data.token}' | base64 -d)"

  for attempt in $(seq 1 20); do
    if ensure_output="$(
      nifi_exec env \
        FLOW_REGISTRY_CLIENT_NAME="${REGISTRY_CLIENT_NAME}" \
        FLOW_REGISTRY_GITHUB_PAT="${github_token}" \
        EXPECTED_BUCKETS="${WORKFLOW_BUCKET_NAME},team-b" \
        python3 - <<'PY'
import json
import os
import ssl
import subprocess
import tempfile
import urllib.error
import urllib.request


def ensure_client_files():
    keystore = "/opt/nifi/tls/keystore.p12"
    password = os.environ.get("KEYSTORE_PASSWORD", "")
    if not password:
        raise SystemExit("KEYSTORE_PASSWORD is required to prove proxied-auth Flow Registry Client visibility")
    workdir = tempfile.mkdtemp(prefix="nifidataflow-runtime-client-")
    cert_path = os.path.join(workdir, "client.crt.pem")
    key_path = os.path.join(workdir, "client.key.pem")
    subprocess.run(
        ["openssl", "pkcs12", "-in", keystore, "-clcerts", "-nokeys", "-passin", f"pass:{password}", "-out", cert_path],
        check=True,
        capture_output=True,
        text=True,
    )
    subprocess.run(
        ["openssl", "pkcs12", "-in", keystore, "-nocerts", "-nodes", "-passin", f"pass:{password}", "-out", key_path],
        check=True,
        capture_output=True,
        text=True,
    )
    return cert_path, key_path


def build_opener():
    cert_path, key_path = ensure_client_files()
    ctx = ssl.create_default_context(cafile="/opt/nifi/tls/ca.crt")
    ctx.load_cert_chain(certfile=cert_path, keyfile=key_path)
    return urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx))


def request(opener, method, path, payload=None):
    headers = {
        "X-ProxiedEntitiesChain": f"<{os.environ.get('SINGLE_USER_CREDENTIALS_USERNAME', '').strip()}>",
    }
    body = None
    if payload is not None:
        body = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(
        "https://nifi-0.nifi-headless.nifi.svc.cluster.local:8443/nifi-api" + path,
        headers=headers,
        data=body,
        method=method,
    )
    try:
        with opener.open(req, timeout=20) as response:
            raw = response.read().decode("utf-8")
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", "ignore")
        raise SystemExit(f"{method} {path} failed with status {exc.code}: {raw[:4096]}")


def client_catalog_entry():
    catalog = json.load(open("/opt/nifi/fabric/flow-registry-clients/clients.json", encoding="utf-8"))
    target = os.environ["FLOW_REGISTRY_CLIENT_NAME"]
    for entry in catalog.get("clients", []):
        if entry.get("name") == target:
            return entry
    raise SystemExit(f"prepared Flow Registry Client {target!r} not found in mounted catalog")


def registry_id_by_name(payload, target):
    for entry in payload.get("registries", []):
        component = entry.get("component", {})
        if component.get("name") == target:
            return entry.get("id") or component.get("id") or ""
    return ""


opener = build_opener()
client = client_catalog_entry()
client_name = client["name"]

_, registries = request(opener, "GET", "/flow/registries")
registry_id = registry_id_by_name(registries, client_name)

if not registry_id:
    _, types_payload = request(opener, "GET", "/controller/registry-types")
    bundle = None
    for entry in types_payload.get("flowRegistryClientTypes", []):
        if entry.get("type") == client.get("implementationClass"):
            bundle = entry.get("bundle")
            break
    if not bundle:
        raise SystemExit(f"NiFi runtime does not expose registry type {client.get('implementationClass')!r}")

    properties = dict(client.get("properties", {}))
    properties["Personal Access Token"] = os.environ["FLOW_REGISTRY_GITHUB_PAT"]
    payload = {
        "revision": {"version": 0},
        "component": {
            "name": client_name,
            "type": client["implementationClass"],
            "bundle": bundle,
            "properties": properties,
        },
    }
    try:
        request(opener, "POST", "/controller/registry-clients", payload)
    except SystemExit as exc:
        if "status 409" not in str(exc):
            raise

    _, registries = request(opener, "GET", "/flow/registries")
    registry_id = registry_id_by_name(registries, client_name)

if not registry_id:
    raise SystemExit(f"Flow Registry Client {client_name!r} is still not visible to proxied-auth runtime requests")

_, buckets_payload = request(opener, "GET", f"/flow/registries/{registry_id}/buckets")
buckets = []
for entry in buckets_payload.get("bucketResults") or buckets_payload.get("buckets") or []:
    candidate = entry.get("bucket") if isinstance(entry, dict) else {}
    if not isinstance(candidate, dict):
        candidate = entry if isinstance(entry, dict) else {}
    name = candidate.get("name") or candidate.get("id") or ""
    if name:
        buckets.append(name)

expected = [item for item in os.environ.get("EXPECTED_BUCKETS", "").split(",") if item]
missing = [item for item in expected if item not in buckets]
if missing:
    raise SystemExit(f"Flow Registry Client {client_name!r} is visible to proxied-auth runtime requests but missing buckets {missing!r}; got {buckets!r}")

print(json.dumps({"buckets": buckets, "client": client_name, "registryId": registry_id}, indent=2, sort_keys=True))
PY
    2>&1)"; then
      printf '%s\n' "${ensure_output}"
      return 0
    fi
    if is_transient_nifi_exec_error "${ensure_output}"; then
      sleep 5
      continue
    fi
    printf '%s\n' "${ensure_output}" >&2
    return 1
  done

  printf '%s\n' "${ensure_output}" >&2
  echo "timed out waiting for proxied Flow Registry Client visibility in NiFi" >&2
  return 1
}

trigger_versioned_flow_import_runtime() {
  local output=""
  local attempt

  for attempt in $(seq 1 20); do
    if output="$(nifi_exec python3 /opt/nifi/fabric/versioned-flow-imports/bootstrap.py --once 2>&1)"; then
      return 0
    fi
    if is_transient_nifi_exec_error "${output}"; then
      sleep 5
      continue
    fi
    printf '%s\n' "${output}" >&2
    return 1
  done

  printf '%s\n' "${output}" >&2
  echo "timed out waiting for bounded versioned flow import runtime to run successfully" >&2
  return 1
}

ensure_runtime_import_prereqs() {
  local import_name="$1"
  local expected_selected_version="$2"

  retry_proof "bounded runtime can resolve live Flow Registry Client ${REGISTRY_CLIENT_NAME} for ${import_name}=${expected_selected_version}" \
    runtime_import_prereqs_ready "${import_name}" "${expected_selected_version}"
}

flow_registry_client_listed() {
  nifi_request GET /flow/registries >"${tmpdir}/flow-registries-check.json"
  python3 - "${tmpdir}/flow-registries-check.json" "${REGISTRY_CLIENT_NAME}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
for entry in payload.get("registries", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        raise SystemExit(0)
raise SystemExit(1)
PY
}

resolve_flow_versions() {
  local bucket_name="$1"
  local flow_name="$2"
  local username password pod host base_url token registries_json registry_id flows_json flow_id versions_json

  username="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.username}' | base64 -d)"
  password="$(kubectl -n "${NAMESPACE}" get secret nifi-auth -o jsonpath='{.data.password}' | base64 -d)"
  pod="${HELM_RELEASE}-0"
  host="nifi-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local"
  base_url="https://${host}:8443/nifi-api"

  token="$(
    nifi_exec env \
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
  )"

  nifi_request_with_token() {
    local method="$1"
    local path="$2"
    nifi_exec env \
      NIFI_BASE_URL="${base_url}" \
      NIFI_TOKEN="${token}" \
      REQUEST_METHOD="${method}" \
      REQUEST_PATH="${path}" \
      sh -ec '
        curl --silent --show-error --fail \
          --cacert /opt/nifi/tls/ca.crt \
          -H "Authorization: Bearer ${NIFI_TOKEN}" \
          -X "${REQUEST_METHOD}" \
          "${NIFI_BASE_URL}${REQUEST_PATH}"
      '
  }

  nifi_request_with_token_status() {
    local method="$1"
    local path="$2"
    local response

    response="$(
      nifi_exec env \
        NIFI_BASE_URL="${base_url}" \
        NIFI_TOKEN="${token}" \
        REQUEST_METHOD="${method}" \
        REQUEST_PATH="${path}" \
        sh -ec '
          curl --silent --show-error \
            --cacert /opt/nifi/tls/ca.crt \
            -H "Authorization: Bearer ${NIFI_TOKEN}" \
            -X "${REQUEST_METHOD}" \
            --write-out "\n%{http_code}" \
            "${NIFI_BASE_URL}${REQUEST_PATH}"
        '
    )"

    STATUS_HTTP_CODE="${response##*$'\n'}"
    STATUS_HTTP_BODY="${response%$'\n'*}"
  }

  registries_json="$(nifi_request_with_token GET /flow/registries)"
  registry_id="$(
    python3 - "${bucket_name}" "${flow_name}" "${registries_json}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[3])
for entry in payload.get("registries", []):
    component = entry.get("component", {})
    if component.get("name") == "github-flows":
        print(entry.get("id") or component.get("id") or "")
        break
PY
  )"
  if [[ -z "${registry_id}" ]]; then
    echo "failed to resolve registry id for github-flows" >&2
    return 1
  fi

  nifi_request_with_token_status GET "/flow/registries/${registry_id}/buckets/${bucket_name}/flows"
  if [[ "${STATUS_HTTP_CODE}" =~ ^2 ]]; then
    flows_json="${STATUS_HTTP_BODY}"
  elif [[ "${STATUS_HTTP_CODE}" == "404" || "${STATUS_HTTP_CODE}" == "409" ]]; then
    flows_json='{"bucketFlowResults":[]}'
  else
    echo "NiFi API GET /flow/registries/${registry_id}/buckets/${bucket_name}/flows returned HTTP ${STATUS_HTTP_CODE}" >&2
    printf '%s\n' "${STATUS_HTTP_BODY}" >&2
    return 1
  fi
  flow_id="$(
    python3 - "${flow_name}" "${flows_json}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[2])
target = sys.argv[1]
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

  versions_json="$(nifi_request_with_token GET "/flow/registries/${registry_id}/buckets/${bucket_name}/flows/${flow_id}/versions")"
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

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
)

if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
fi

helm_values_args+=(
  -f "${ROOT_DIR}/${FLOW_IMPORT_VALUES_FILE}"
  -f "${ROOT_DIR}/${FLOW_IMPORT_KIND_VALUES_FILE}"
  -f "${ROOT_DIR}/${NIFIDATAFLOW_VALUES_FILE}"
)

phase "Checking prerequisites"
check_prereqs
require_command curl
require_command jq
require_command python3

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for NiFiDataflow proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for NiFiDataflow proof"
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

phase "Creating GitHub Flow Registry token Secret"
kubectl -n "${NAMESPACE}" create secret generic "${FLOW_REGISTRY_SECRET_NAME}" \
  --from-literal=token=dummytoken \
  --dry-run=client -o yaml | kubectl apply -f -

phase "Deploying GitHub-compatible evaluator service"
NAMESPACE="${NAMESPACE}" NIFI_IMAGE="${NIFI_IMAGE}" bash "${ROOT_DIR}/hack/deploy-github-flow-registry-mock.sh"

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Quiescing NiFi runtime writers before reuse"
  kubectl -n "${NAMESPACE}" scale statefulset "${HELM_RELEASE}" --replicas=0
  kubectl -n "${NAMESPACE}" wait --for=delete pod/"${HELM_RELEASE}-0" --timeout=10m || true

  phase "Resetting NiFiDataflow bridge ConfigMaps for reuse"
  kubectl -n "${NAMESPACE}" delete configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" --ignore-not-found
  kubectl -n "${NAMESPACE}" delete configmap "${HELM_RELEASE}-nifidataflows-status" --ignore-not-found
fi

phase "Installing product chart managed release with NiFiDataflow bridge enabled"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Refreshing NiFi pod mounts for controller-bridge reuse"
  kubectl -n "${NAMESPACE}" delete pod "${HELM_RELEASE}-0" --wait=false
  kubectl -n "${NAMESPACE}" wait --for=condition=Ready pod/"${HELM_RELEASE}-0" --timeout=10m
fi

phase "Verifying platform resources and controller rollout"
kubectl get crd nificlusters.platform.nifi.io >/dev/null
kubectl get crd nifidataflows.platform.nifi.io >/dev/null
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

phase "Verifying initial cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

phase "Proving runtime-managed Parameter Context prerequisite"
bash "${ROOT_DIR}/hack/prove-parameter-contexts-runtime.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --auth-secret nifi-auth \
  --context-name "${PARAMETER_CONTEXT_NAME}" \
  --expected-inline-parameter payments.api.baseUrl \
  --expected-inline-value https://payments.internal.example.com \
  --expected-sensitive-parameter "" \
  --expected-action created,unchanged,updated

phase "Cleaning prior NiFiDataflow e2e artifacts"
cleanup_e2e_artifacts
reset_runtime_status_artifacts

phase "Ensuring the live Flow Registry Client exists before flow-version discovery"
ensure_live_flow_registry_client

phase "Ensuring the selected flow is visible through the live GitHub registry client"
if flow_version_output="$(resolve_flow_versions "${WORKFLOW_BUCKET_NAME}" "${WORKFLOW_FLOW_NAME}")"; then
  mapfile -t flow_versions <<<"${flow_version_output}"
  info "seed flow ${WORKFLOW_BUCKET_NAME}/${WORKFLOW_FLOW_NAME} is already visible through NiFi; reusing it"
else
  bash "${ROOT_DIR}/hack/prove-github-flow-registry-workflow.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --client-name "${REGISTRY_CLIENT_NAME}" \
    --workflow-bucket "${WORKFLOW_BUCKET_NAME}" \
    --workflow-flow-name "${WORKFLOW_FLOW_NAME}" \
    --workflow-process-group-name "nifidataflow-seed-$(date +%s)"
fi

phase "Saving a second GitHub-backed version of the selected flow"
save_mock_flow_version "${WORKFLOW_BUCKET_NAME}" "${WORKFLOW_FLOW_NAME}"
retry_proof "seeded flow versions diverge after mock update" \
  flow_versions_diverged "${WORKFLOW_BUCKET_NAME}" "${WORKFLOW_FLOW_NAME}"

phase "Resolving the seeded explicit and latest flow versions"
flow_version_output="$(resolve_flow_versions "${WORKFLOW_BUCKET_NAME}" "${WORKFLOW_FLOW_NAME}")"
mapfile -t flow_versions <<<"${flow_version_output}"
initial_flow_version="${flow_versions[0]:-}"
latest_flow_version="${flow_versions[1]:-}"
if [[ -z "${initial_flow_version}" || -z "${latest_flow_version}" ]]; then
  echo "failed to resolve seeded flow versions for ${WORKFLOW_FLOW_NAME}" >&2
  exit 1
fi

phase "Ensuring the live Flow Registry Client exists before NiFiDataflow reconcile"
ensure_live_flow_registry_client

phase "Creating primary NiFiDataflow at explicit version ${initial_flow_version}"
apply_dataflow "${PRIMARY_DATAFLOW_NAME}" "${initial_flow_version}" "${PRIMARY_TARGET_NAME}" false
ensure_runtime_import_prereqs "${PRIMARY_DATAFLOW_NAME}" "${initial_flow_version}"

retry_proof "primary NiFiDataflow ready status" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${initial_flow_version}" \
    --expect-no-retained-owned-imports

retry_proof "primary NiFiDataflow live import creation" \
  bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --imports-configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" \
    --import-name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-action created,updated,unchanged

phase "Starting primary imported target to prove bounded DrainAndReplace"
start_root_process_group_and_wait "${PRIMARY_TARGET_NAME}"

phase "Updating primary NiFiDataflow to latest (${latest_flow_version})"
previous_pod_uid="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.metadata.uid}')"
apply_dataflow "${PRIMARY_DATAFLOW_NAME}" latest "${PRIMARY_TARGET_NAME}" false
ensure_runtime_import_prereqs "${PRIMARY_DATAFLOW_NAME}" latest

retry_proof "primary NiFiDataflow latest version status" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${latest_flow_version}" \
    --expect-no-retained-owned-imports

retry_proof "primary NiFiDataflow live import update" \
  bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --imports-configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" \
    --import-name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-action updated,unchanged \
    --expected-rollout-strategy DrainAndReplace \
    --expected-drained-before-version-update true \
    --expected-resumed-after-version-update true

current_pod_uid="$(kubectl -n "${NAMESPACE}" get pod "${HELM_RELEASE}-0" -o jsonpath='{.metadata.uid}')"
if [[ "${current_pod_uid}" != "${previous_pod_uid}" ]]; then
  echo "expected NiFiDataflow live version update to reconcile without replacing pod ${HELM_RELEASE}-0" >&2
  exit 1
fi

phase "Switching primary NiFiDataflow to syncPolicy Once at the latest declaration"
apply_dataflow "${PRIMARY_DATAFLOW_NAME}" latest "${PRIMARY_TARGET_NAME}" false "${PARAMETER_CONTEXT_NAME}" Once
ensure_runtime_import_prereqs "${PRIMARY_DATAFLOW_NAME}" latest

retry_proof "mounted NiFiDataflow bridge reflects syncPolicy Once for the primary import" \
  mounted_bridge_import_sync_mode_matches "${PRIMARY_DATAFLOW_NAME}" Once
retry_proof "runtime status reflects the Once declaration hash for the primary import" \
  runtime_import_observed_hash_matches_bridge "${PRIMARY_DATAFLOW_NAME}"

retry_proof "primary NiFiDataflow ready status under syncPolicy Once" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${latest_flow_version}" \
    --expected-last-successful-version "${latest_flow_version}" \
    --expect-no-retained-owned-imports

phase "Manually drifting the primary imported process group back to explicit version ${initial_flow_version}"
manually_update_imported_process_group_version "${PRIMARY_TARGET_NAME}" "${initial_flow_version}"

retry_proof "primary NiFiDataflow leaves version drift unhealed under syncPolicy Once" \
  prove_primary_once_drift_skip "${initial_flow_version}" "${latest_flow_version}"

phase "Changing the primary NiFiDataflow spec to explicit version ${latest_flow_version}"
apply_dataflow "${PRIMARY_DATAFLOW_NAME}" "${latest_flow_version}" "${PRIMARY_TARGET_NAME}" false "${PARAMETER_CONTEXT_NAME}" Once
ensure_runtime_import_prereqs "${PRIMARY_DATAFLOW_NAME}" "${latest_flow_version}"

retry_proof "primary NiFiDataflow reconciles drift again after spec change" \
  bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --imports-configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" \
    --import-name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-action updated,unchanged \
    --expected-resolved-version "${latest_flow_version}" \
    --expected-actual-version "${latest_flow_version}" \
    --expected-version-drift-reconcile-skipped false

retry_proof "primary NiFiDataflow ready status after spec-change reconcile" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${latest_flow_version}" \
    --expected-last-successful-version "${latest_flow_version}" \
    --expect-no-retained-owned-imports

phase "Creating operator-owned conflicting root process group"
create_root_process_group "${MANUAL_TARGET_NAME}"

phase "Creating conflicting NiFiDataflow to prove AdoptionRefused"
apply_dataflow "${MANUAL_DATAFLOW_NAME}" "${initial_flow_version}" "${MANUAL_TARGET_NAME}" false
ensure_runtime_import_prereqs "${MANUAL_DATAFLOW_NAME}" "${initial_flow_version}"

retry_proof "adoption refused status" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${MANUAL_DATAFLOW_NAME}" \
    --expected-phase Blocked \
    --expected-ownership-state AdoptionRefused \
    --expect-condition TargetResolved=False:AdoptionRefused \
    --expected-last-operation-substring "not adopted automatically"

phase "Creating retained-target NiFiDataflow"
apply_dataflow "${RETAINED_DATAFLOW_NAME}" "${initial_flow_version}" "${RETAINED_TARGET_NAME}" false "${PARAMETER_CONTEXT_NAME}" OnChange Replace
ensure_runtime_import_prereqs "${RETAINED_DATAFLOW_NAME}" "${initial_flow_version}"

retry_proof "retained-target dataflow ready status" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${RETAINED_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${initial_flow_version}"

retry_proof "retained-target live import creation" \
  bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --imports-configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" \
    --import-name "${RETAINED_DATAFLOW_NAME}" \
    --expected-action created,updated,unchanged

phase "Starting retained imported target to prove Replace skips bounded drain"
start_root_process_group_and_wait "${RETAINED_TARGET_NAME}"

phase "Updating retained-target NiFiDataflow with rollout strategy Replace"
apply_dataflow "${RETAINED_DATAFLOW_NAME}" "${latest_flow_version}" "${RETAINED_TARGET_NAME}" false "${PARAMETER_CONTEXT_NAME}" OnChange Replace
ensure_runtime_import_prereqs "${RETAINED_DATAFLOW_NAME}" "${latest_flow_version}"

retry_proof "retained-target dataflow ready status after Replace rollout" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${RETAINED_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${latest_flow_version}"

retry_proof "retained-target live import update uses Replace without bounded drain" \
  bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
    --namespace "${NAMESPACE}" \
    --release "${HELM_RELEASE}" \
    --auth-secret nifi-auth \
    --imports-configmap "${BRIDGE_IMPORTS_CONFIGMAP_NAME}" \
    --import-name "${RETAINED_DATAFLOW_NAME}" \
    --expected-action updated,unchanged \
    --expected-rollout-strategy Replace \
    --expected-drained-before-version-update false \
    --expected-resumed-after-version-update false

phase "Suspending the primary NiFiDataflow"
apply_dataflow "${PRIMARY_DATAFLOW_NAME}" "${latest_flow_version}" "${PRIMARY_TARGET_NAME}" true "${PARAMETER_CONTEXT_NAME}" Once
ensure_runtime_import_prereqs "${PRIMARY_DATAFLOW_NAME}" "${latest_flow_version}"

retry_proof "primary NiFiDataflow suspended status" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-phase Pending \
    --expect-condition Progressing=False:Suspended \
    --expected-last-operation-substring "spec.suspend=true"

retry_proof "primary imported process group still exists while suspended" root_process_group_exists "${PRIMARY_TARGET_NAME}"

phase "Resuming the primary NiFiDataflow"
apply_dataflow "${PRIMARY_DATAFLOW_NAME}" "${latest_flow_version}" "${PRIMARY_TARGET_NAME}" false "${PARAMETER_CONTEXT_NAME}" Once
ensure_runtime_import_prereqs "${PRIMARY_DATAFLOW_NAME}" "${latest_flow_version}"

retry_proof "primary NiFiDataflow resumed ready status" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${latest_flow_version}" \
    --expected-last-successful-version "${latest_flow_version}"

phase "Deleting retained-target NiFiDataflow without deleting the live process group"
delete_dataflow "${RETAINED_DATAFLOW_NAME}"
ensure_runtime_import_prereqs "${PRIMARY_DATAFLOW_NAME}" "${latest_flow_version}"

retry_proof "retained target process group remains after NiFiDataflow deletion" root_process_group_exists "${RETAINED_TARGET_NAME}"

retry_proof "primary NiFiDataflow surfaces retained owned import warning" \
  bash "${ROOT_DIR}/hack/prove-nifidataflow-status.sh" \
    --namespace "${NAMESPACE}" \
    --name "${PRIMARY_DATAFLOW_NAME}" \
    --expected-phase Ready \
    --expected-ownership-state Managed \
    --expected-observed-version "${latest_flow_version}" \
    --expected-last-successful-version "${latest_flow_version}" \
    --expected-retained-owned-import "${RETAINED_DATAFLOW_NAME}" \
    --expect-condition Degraded=True:RetainedOwnedImportsPresent

print_success_footer "NiFiDataflow bounded e2e proof completed" \
  "kubectl -n ${NAMESPACE} get nifidataflow" \
  "kubectl -n ${NAMESPACE} get nifidataflow ${PRIMARY_DATAFLOW_NAME} -o yaml" \
  "kubectl -n ${NAMESPACE} exec -i ${HELM_RELEASE}-0 -c nifi -- cat /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json" \
  "kubectl -n ${NAMESPACE} get configmap ${BRIDGE_IMPORTS_CONFIGMAP_NAME} -o yaml"
