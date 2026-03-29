#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
imports_configmap=""
import_name=""
status_file="/opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json"
expected_action=""
expected_resolved_version=""
expected_actual_version=""
expected_version_drift_reconcile_skipped=""
expected_rollout_strategy=""
expected_drained_before_version_update=""
expected_resumed_after_version_update=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      namespace="$2"
      shift 2
      ;;
    --release)
      release="$2"
      shift 2
      ;;
    --auth-secret)
      auth_secret="$2"
      shift 2
      ;;
    --imports-configmap)
      imports_configmap="$2"
      shift 2
      ;;
    --import-name)
      import_name="$2"
      shift 2
      ;;
    --status-file)
      status_file="$2"
      shift 2
      ;;
    --expected-action)
      expected_action="$2"
      shift 2
      ;;
    --expected-resolved-version)
      expected_resolved_version="$2"
      shift 2
      ;;
    --expected-actual-version)
      expected_actual_version="$2"
      shift 2
      ;;
    --expected-version-drift-reconcile-skipped)
      expected_version_drift_reconcile_skipped="$2"
      shift 2
      ;;
    --expected-rollout-strategy)
      expected_rollout_strategy="$2"
      shift 2
      ;;
    --expected-drained-before-version-update)
      expected_drained_before_version_update="$2"
      shift 2
      ;;
    --expected-resumed-after-version-update)
      expected_resumed_after_version_update="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

imports_configmap="${imports_configmap:-${release}-versioned-flow-imports}"

for cmd in kubectl python3; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
done

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

is_transient_nifi_exec_error() {
  local message="$1"
  [[ "${message}" == *'container not found ("nifi")'* ]] \
    || [[ "${message}" == *"unable to upgrade connection"* ]] \
    || [[ "${message}" == *"connection refused"* ]] \
    || [[ "${message}" == *"Connection refused"* ]] \
    || [[ "${message}" == *"Couldn't connect to server"* ]] \
    || [[ "${message}" == *"Failed to connect to "* ]] \
    || [[ "${message}" == *"Flow Controller is initializing the Data Flow."* ]]
}

is_transient_nifi_api_error() {
  local status="$1"
  local body="$2"
  [[ "${body}" == *"Flow Controller is initializing the Data Flow."* ]] \
    || [[ "${body}" == *"no nodes are connected"* ]] \
    || [[ "${status}" == "409" && "${body}" == *"initializing"* ]]
}

wait_for_nifi_exec_ready() {
  local deadline=$(( $(date +%s) + 300 ))

  kubectl -n "${namespace}" wait --for=condition=Ready "pod/${pod}" --timeout=10m >/dev/null

  while true; do
    if kubectl -n "${namespace}" get pod "${pod}" -o json >"${tmpdir}/nifi-pod.json" 2>/dev/null && \
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
    if output="$(kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- "$@" 2>&1)"; then
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
  echo "timed out waiting for a stable exec path into ${pod}/nifi" >&2
  return 1
}

kubectl -n "${namespace}" get configmap "${imports_configmap}" -o jsonpath='{.data.imports\.json}' >"${tmpdir}/imports.json"
if [[ ! -s "${tmpdir}/imports.json" ]]; then
  echo "versioned flow import runtime bundle ${imports_configmap} is missing data.imports.json" >&2
  exit 1
fi

python3 - "${tmpdir}/imports.json" "${import_name}" >"${tmpdir}/selected-import.json" <<'PY'
import json
import sys

catalog = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
imports = catalog.get("imports", [])
if not imports:
    raise SystemExit("runtime bundle does not contain any imports")

selected = None
if target_name:
    for entry in imports:
        if entry.get("name") == target_name:
            selected = entry
            break
    if selected is None:
        raise SystemExit(f"import {target_name!r} not found in runtime bundle")
else:
    selected = imports[0]

json.dump(selected, sys.stdout)
PY

mapfile -t selected_meta < <(python3 - "${tmpdir}/selected-import.json" <<'PY'
import json
import sys

selected = json.load(open(sys.argv[1], encoding="utf-8"))
print(selected["name"])
print(selected["registryClientRef"]["name"])
print(selected["source"]["bucket"])
print(selected["source"]["flowName"])
print(selected["source"]["version"])
print(selected["target"]["rootProcessGroupName"])
refs = selected.get("parameterContextRefs", [])
print(refs[0]["name"] if refs else "")
PY
)

selected_import_name="${selected_meta[0]:-}"
registry_client_name="${selected_meta[1]:-}"
bucket_name="${selected_meta[2]:-}"
flow_name="${selected_meta[3]:-}"
selected_version="${selected_meta[4]:-}"
target_root_pg_name="${selected_meta[5]:-}"
parameter_context_name="${selected_meta[6]:-}"

username="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.username}' | base64 -d)"
password="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.password}' | base64 -d)"
pod="${release}-0"
host="nifi-0.${release}-headless.${namespace}.svc.cluster.local"
base_url="https://${host}:8443/nifi-api"
nifi_token=""

mint_nifi_token() {
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
}

for _ in $(seq 1 20); do
  if nifi_token="$(mint_nifi_token)"; then
    break
  fi
  sleep 2
done

if [[ -z "${nifi_token}" ]]; then
  echo "timed out waiting for NiFi API token minting through direct pod HTTPS" >&2
  exit 1
fi

nifi_request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local response status
  local attempt

  for attempt in $(seq 1 20); do
    response="$(
      nifi_exec env \
        NIFI_BASE_URL="${base_url}" \
        NIFI_TOKEN="${nifi_token}" \
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

nifi_exec cat "${status_file}" >"${tmpdir}/status.json"
if [[ ! -s "${tmpdir}/status.json" ]]; then
  echo "versioned flow import bootstrap status file ${status_file} is missing" >&2
  exit 1
fi

python3 - "${tmpdir}/status.json" "${selected_import_name}" "${expected_action}" "${expected_resolved_version}" "${expected_actual_version}" "${expected_version_drift_reconcile_skipped}" "${expected_rollout_strategy}" "${expected_drained_before_version_update}" "${expected_resumed_after_version_update}" >"${tmpdir}/status-entry.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
expected_action = sys.argv[3]
expected_resolved_version = sys.argv[4]
expected_actual_version = sys.argv[5]
expected_version_drift_reconcile_skipped = sys.argv[6]
expected_rollout_strategy = sys.argv[7]
expected_drained_before_version_update = sys.argv[8]
expected_resumed_after_version_update = sys.argv[9]

entry = None
for candidate in payload.get("imports", []):
    if candidate.get("name") == target_name:
        entry = candidate
        break

if entry is None:
    raise SystemExit(f"bootstrap status did not contain import {target_name!r}")

if entry.get("status") != "ok":
    raise SystemExit(f"import {target_name!r} status is {entry.get('status')!r}: {entry}")

allowed_bootstrap_statuses = {"ok", "blocked"}
bootstrap_status = payload.get("status")
if bootstrap_status not in allowed_bootstrap_statuses:
    raise SystemExit(f"bootstrap status is {bootstrap_status!r}: {payload}")

if expected_action:
    allowed = {value.strip() for value in expected_action.split(",") if value.strip()}
    if entry.get("action") not in allowed:
        raise SystemExit(f"expected action in {sorted(allowed)}, got {entry.get('action')!r}")

if expected_resolved_version and str(entry.get("resolvedVersion") or "") != expected_resolved_version:
    raise SystemExit(
        f"expected resolvedVersion {expected_resolved_version!r}, got {str(entry.get('resolvedVersion') or '')!r}"
    )

if expected_actual_version and str(entry.get("actualVersion") or "") != expected_actual_version:
    raise SystemExit(
        f"expected actualVersion {expected_actual_version!r}, got {str(entry.get('actualVersion') or '')!r}"
    )

if expected_version_drift_reconcile_skipped:
    actual = "true" if bool(entry.get("versionDriftReconcileSkipped")) else "false"
    if actual != expected_version_drift_reconcile_skipped:
        raise SystemExit(
            "expected versionDriftReconcileSkipped "
            f"{expected_version_drift_reconcile_skipped!r}, got {actual!r}"
        )

if expected_rollout_strategy and str(entry.get("rolloutStrategy") or "") != expected_rollout_strategy:
    raise SystemExit(
        f"expected rolloutStrategy {expected_rollout_strategy!r}, got {str(entry.get('rolloutStrategy') or '')!r}"
    )

if expected_drained_before_version_update:
    actual = "true" if bool(entry.get("drainedBeforeVersionUpdate")) else "false"
    if actual != expected_drained_before_version_update:
        raise SystemExit(
            "expected drainedBeforeVersionUpdate "
            f"{expected_drained_before_version_update!r}, got {actual!r}"
        )

if expected_resumed_after_version_update:
    actual = "true" if bool(entry.get("resumedAfterVersionUpdate")) else "false"
    if actual != expected_resumed_after_version_update:
        raise SystemExit(
            "expected resumedAfterVersionUpdate "
            f"{expected_resumed_after_version_update!r}, got {actual!r}"
        )

json.dump(entry, sys.stdout)
PY

mapfile -t status_meta < <(python3 - "${tmpdir}/status-entry.json" <<'PY'
import json
import sys

entry = json.load(open(sys.argv[1], encoding="utf-8"))
print(entry.get("action", ""))
print(entry.get("processGroupId", ""))
print(entry.get("resolvedVersion", ""))
print(entry.get("actualVersion", ""))
print(entry.get("registryClientId", ""))
print(entry.get("flowId", ""))
print(entry.get("parameterContextId", ""))
print("true" if entry.get("versionDriftReconcileSkipped") else "false")
print(entry.get("rolloutStrategy", ""))
print("true" if entry.get("drainedBeforeVersionUpdate") else "false")
print("true" if entry.get("resumedAfterVersionUpdate") else "false")
PY
)

status_action="${status_meta[0]:-}"
status_process_group_id="${status_meta[1]:-}"
status_resolved_version="${status_meta[2]:-}"
status_actual_version="${status_meta[3]:-}"
status_registry_id="${status_meta[4]:-}"
status_flow_id="${status_meta[5]:-}"
status_parameter_context_id="${status_meta[6]:-}"
status_version_drift_reconcile_skipped="${status_meta[7]:-false}"
status_rollout_strategy="${status_meta[8]:-}"
status_drained_before_version_update="${status_meta[9]:-false}"
status_resumed_after_version_update="${status_meta[10]:-false}"

nifi_request GET /flow/registries >"${tmpdir}/flow-registries.json"
nifi_request GET /flow/process-groups/root >"${tmpdir}/root-flow.json"

process_group_id="$(
  python3 - "${tmpdir}/root-flow.json" "${target_root_pg_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
flow = payload.get("processGroupFlow", {}).get("flow", {})
for entry in flow.get("processGroups", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        print(entry.get("id") or component.get("id") or "")
        break
PY
)"

if [[ -z "${process_group_id}" ]]; then
  echo "bounded imported process group ${target_root_pg_name} was not found under the root process group" >&2
  exit 1
fi

nifi_request GET "/process-groups/${process_group_id}" >"${tmpdir}/process-group.json"
nifi_request GET "/flow/process-groups/${process_group_id}" >"${tmpdir}/process-group-flow.json"
nifi_request GET /flow/parameter-contexts >"${tmpdir}/parameter-contexts.json"

python3 - \
  "${tmpdir}/process-group.json" \
  "${tmpdir}/process-group-flow.json" \
  "${tmpdir}/flow-registries.json" \
  "${tmpdir}/parameter-contexts.json" \
  "${target_root_pg_name}" \
  "${registry_client_name}" \
  "${bucket_name}" \
  "${flow_name}" \
  "${selected_version}" \
  "${parameter_context_name}" \
  "${selected_import_name}" \
  "${status_process_group_id}" \
  "${status_resolved_version}" \
  "${status_actual_version}" \
  "${status_registry_id}" \
  "${status_flow_id}" \
  "${status_parameter_context_id}" \
  "${expected_actual_version}" \
  "${expected_version_drift_reconcile_skipped}" \
  "${status_version_drift_reconcile_skipped}" <<'PY'
import json
import sys

process_group = json.load(open(sys.argv[1], encoding="utf-8"))
process_group_flow = json.load(open(sys.argv[2], encoding="utf-8"))
flow_registries = json.load(open(sys.argv[3], encoding="utf-8"))
parameter_contexts = json.load(open(sys.argv[4], encoding="utf-8"))

expected_name = sys.argv[5]
expected_registry_name = sys.argv[6]
expected_bucket = sys.argv[7]
expected_flow_name = sys.argv[8]
selected_version = sys.argv[9]
expected_parameter_context_name = sys.argv[10]
expected_import_name = sys.argv[11]
status_process_group_id = sys.argv[12]
status_resolved_version = sys.argv[13]
status_actual_version = sys.argv[14]
status_registry_id = sys.argv[15]
status_flow_id = sys.argv[16]
status_parameter_context_id = sys.argv[17]
expected_actual_version = sys.argv[18]
expected_version_drift_reconcile_skipped = sys.argv[19]
status_version_drift_reconcile_skipped = sys.argv[20]

component = process_group.get("component", {})
actual_name = component.get("name")
if actual_name != expected_name:
    raise SystemExit(f"expected imported process group name {expected_name!r}, got {actual_name!r}")

actual_process_group_id = process_group.get("id") or component.get("id") or ""
if status_process_group_id and actual_process_group_id != status_process_group_id:
    raise SystemExit(
        f"status file process group id {status_process_group_id!r} did not match live id {actual_process_group_id!r}"
    )

registry_id = ""
for entry in flow_registries.get("registries", []):
    candidate = entry.get("component", {})
    if candidate.get("name") == expected_registry_name:
        registry_id = entry.get("id") or candidate.get("id") or ""
        break

if not registry_id:
    raise SystemExit(f"expected live Flow Registry Client {expected_registry_name!r} was not found")

if status_registry_id and registry_id != status_registry_id:
    raise SystemExit(
        f"status file registry id {status_registry_id!r} did not match live registry id {registry_id!r}"
    )

version_control = {}
for candidate in (
    process_group.get("component", {}).get("versionControlInformation"),
    process_group_flow.get("processGroupFlow", {}).get("breadcrumb", {}).get("versionControlInformation"),
    process_group_flow.get("processGroupFlow", {}).get("flow", {}).get("versionControlInformation"),
):
    if isinstance(candidate, dict) and candidate:
        version_control = candidate
        break

if not version_control:
    raise SystemExit("imported process group does not expose version-control information")

actual_registry_id = version_control.get("registryId") or version_control.get("registryIdentifier") or ""
actual_bucket_id = version_control.get("bucketId") or version_control.get("bucketIdentifier") or ""
actual_flow_id = version_control.get("flowId") or version_control.get("identifier") or ""
actual_flow_name = version_control.get("flowName") or version_control.get("name") or ""
actual_version = version_control.get("version")
actual_version = "" if actual_version is None else str(actual_version)

if actual_registry_id and actual_registry_id != registry_id:
    raise SystemExit(
        f"imported process group registry id {actual_registry_id!r} did not match live registry id {registry_id!r}"
    )
if not actual_bucket_id:
    raise SystemExit("imported process group did not expose a bucket id")
if actual_flow_name and actual_flow_name != expected_flow_name:
    raise SystemExit(
        f"imported process group flow name {actual_flow_name!r} did not match expected flow name {expected_flow_name!r}"
    )
if status_flow_id and actual_flow_id and actual_flow_id != status_flow_id:
    raise SystemExit(
        f"imported process group flow id {actual_flow_id!r} did not match resolved flow id {status_flow_id!r}"
    )
if selected_version == "latest":
    if expected_version_drift_reconcile_skipped != "true" and status_resolved_version and actual_version != status_resolved_version:
        raise SystemExit(
            f"imported process group version {actual_version!r} did not match resolved latest version {status_resolved_version!r}"
        )
else:
    if expected_version_drift_reconcile_skipped != "true" and actual_version != selected_version:
        raise SystemExit(
            f"imported process group version {actual_version!r} did not match selected version {selected_version!r}"
        )

if expected_actual_version and actual_version != expected_actual_version:
    raise SystemExit(
        f"imported process group version {actual_version!r} did not match expected actual version {expected_actual_version!r}"
    )

if expected_version_drift_reconcile_skipped:
    actual_drift_skip = "true" if status_version_drift_reconcile_skipped == "true" else "false"
    if actual_drift_skip != expected_version_drift_reconcile_skipped:
        raise SystemExit(
            "status file versionDriftReconcileSkipped "
            f"{actual_drift_skip!r} did not match expected {expected_version_drift_reconcile_skipped!r}"
        )

actual_parameter_context_id = component.get("parameterContext", {}).get("id", "")
comments = component.get("comments", "") or ""
marker = "Managed by NiFi-Fabric versionedFlowImports: "
if marker not in comments:
    raise SystemExit("imported process group did not contain the bounded versioned-flow ownership marker in comments")
marker_payload = comments.split(marker, 1)[1]
ownership = json.loads(marker_payload)
if ownership.get("importName") != expected_import_name:
    raise SystemExit(
        f"ownership marker importName {ownership.get('importName')!r} did not match expected {expected_import_name!r}"
    )
if ownership.get("resolvedVersion") and status_resolved_version and ownership.get("resolvedVersion") != status_resolved_version:
    raise SystemExit(
        f"ownership marker resolvedVersion {ownership.get('resolvedVersion')!r} did not match status {status_resolved_version!r}"
    )

if expected_parameter_context_name:
    expected_parameter_context_id = ""
    for entry in parameter_contexts.get("parameterContexts", []):
        candidate = entry.get("component", {})
        if candidate.get("name") == expected_parameter_context_name:
            expected_parameter_context_id = entry.get("id") or candidate.get("id") or ""
            break
    if not expected_parameter_context_id:
        raise SystemExit(
            f"expected Parameter Context {expected_parameter_context_name!r} was not found in NiFi"
        )
    if actual_parameter_context_id != expected_parameter_context_id:
        raise SystemExit(
            f"imported process group parameter context {actual_parameter_context_id!r} did not match expected id {expected_parameter_context_id!r}"
        )
    if status_parameter_context_id and status_parameter_context_id != expected_parameter_context_id:
        raise SystemExit(
            f"status file parameter context id {status_parameter_context_id!r} did not match live id {expected_parameter_context_id!r}"
        )
elif actual_parameter_context_id:
    raise SystemExit("imported process group has a bound Parameter Context but none was declared")

imported_flow = process_group_flow.get("processGroupFlow", {}).get("flow", {})
if len(imported_flow.get("labels", [])) < 1:
    raise SystemExit("imported process group did not contain the seeded bounded flow contents")

print(
    json.dumps(
        {
            "bucketId": actual_bucket_id,
            "flowId": actual_flow_id,
            "flowName": expected_flow_name,
            "importedLabelCount": len(imported_flow.get("labels", [])),
            "parameterContextId": actual_parameter_context_id,
            "processGroupId": actual_process_group_id,
            "registryClientId": registry_id,
            "registryClientName": expected_registry_name,
            "ownershipImportName": ownership.get("importName", ""),
            "targetRootProcessGroupName": expected_name,
            "version": actual_version,
        },
        indent=2,
        sort_keys=True,
    )
)
PY

echo
echo "SUCCESS: bounded runtime-managed versioned flow import proof completed"
echo "  import: ${selected_import_name}"
echo "  action: ${status_action}"
echo "  target root child process group: ${target_root_pg_name}"
echo "  selected version: ${selected_version}"
echo "  resolved version: ${status_resolved_version:-${status_actual_version}}"
echo "  actual version: ${status_actual_version:-unknown}"
echo "  version drift reconcile skipped: ${status_version_drift_reconcile_skipped}"
echo "  rollout strategy: ${status_rollout_strategy:-unknown}"
echo "  drained before version update: ${status_drained_before_version_update}"
echo "  resumed after version update: ${status_resumed_after_version_update}"
echo "  registry client: ${registry_client_name}"
echo "  bucket: ${bucket_name}"
echo "  flow: ${flow_name}"
echo "  parameter context: ${parameter_context_name:-none}"
