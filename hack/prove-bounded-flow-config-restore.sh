#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
parameter_contexts_configmap=""
imports_configmap=""
import_name=""
github_mock_state_url=""

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
    --parameter-contexts-configmap)
      parameter_contexts_configmap="$2"
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
    --github-mock-state-url)
      github_mock_state_url="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

parameter_contexts_configmap="${parameter_contexts_configmap:-${release}-parameter-contexts}"
imports_configmap="${imports_configmap:-${release}-versioned-flow-imports}"
github_mock_state_url="${github_mock_state_url:-http://github-mock.${namespace}.svc.cluster.local:8080/debug/state}"

for cmd in kubectl curl jq python3; do
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

echo "==> Reconnecting the Flow Registry client and resolving the selected flow"
bash "$(dirname "${BASH_SOURCE[0]}")/prove-versioned-flow-selection.sh" \
  --namespace "${namespace}" \
  --release "${release}" \
  --auth-secret "${auth_secret}" \
  --imports-configmap "${imports_configmap}" \
  --import-name "${import_name}"

kubectl -n "${namespace}" get configmap "${imports_configmap}" -o jsonpath='{.data.imports\.json}' >"${tmpdir}/imports.json"
kubectl -n "${namespace}" get configmap "${parameter_contexts_configmap}" -o jsonpath='{.data.parameter-contexts\.json}' >"${tmpdir}/parameter-contexts.json"

python3 - "${tmpdir}/imports.json" "${import_name}" >"${tmpdir}/selected-import.json" <<'PY'
import json
import sys

catalog = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
imports = catalog.get("imports", [])
if not imports:
    raise SystemExit("catalog does not contain any imports")

selected = None
if target_name:
    for item in imports:
        if item.get("name") == target_name:
            selected = item
            break
    if selected is None:
        raise SystemExit(f"import {target_name!r} not found in catalog")
else:
    selected = imports[0]

json.dump(selected, sys.stdout)
PY

mapfile -t selected_meta < <(python3 - "${tmpdir}/selected-import.json" <<'PY'
import json
import sys

selected = json.load(open(sys.argv[1], encoding="utf-8"))
refs = selected.get("parameterContextRefs", [])
print(selected["name"])
print(selected["registryClientRef"]["name"])
print(selected["source"]["bucket"])
print(selected["source"]["flowName"])
print(selected["source"]["version"])
print(selected["target"]["rootProcessGroupName"])
print(len(refs))
for ref in refs:
    print(ref["name"])
PY
)

selected_import_name="${selected_meta[0]:-}"
registry_client_name="${selected_meta[1]:-}"
bucket_name="${selected_meta[2]:-}"
flow_name="${selected_meta[3]:-}"
selected_version="${selected_meta[4]:-}"
target_root_pg_name="${selected_meta[5]:-}"
parameter_context_ref_count="${selected_meta[6]:-0}"

if [[ "${selected_version}" != "latest" ]]; then
  echo "bounded restore proof currently supports imports with version=latest; got ${selected_version}" >&2
  exit 1
fi

if (( parameter_context_ref_count > 1 )); then
  echo "bounded restore proof currently supports at most one direct parameterContextRef per imported process group" >&2
  exit 1
fi

parameter_context_ref_name=""
if (( parameter_context_ref_count == 1 )); then
  parameter_context_ref_name="${selected_meta[7]:-}"
fi

python3 - "${tmpdir}/parameter-contexts.json" "${parameter_context_ref_name}" >"${tmpdir}/selected-contexts.json" <<'PY'
import json
import sys

catalog = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
contexts = catalog.get("contexts", [])

if not target_name:
    json.dump([], sys.stdout)
    raise SystemExit(0)

for context in contexts:
    if context.get("name") == target_name:
        json.dump([context], sys.stdout)
        raise SystemExit(0)

raise SystemExit(f"parameter context {target_name!r} not found in catalog")
PY

username="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.username}' | base64 -d)"
password="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.password}' | base64 -d)"
pod="${release}-0"
host="nifi-0.${release}-headless.${namespace}.svc.cluster.local"
base_url="https://${host}:8443/nifi-api"
nifi_token=""

mint_nifi_token() {
  kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- env \
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

  response="$(
    kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- env \
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

  if [[ ! "${status}" =~ ^2 ]]; then
    echo "NiFi API ${method} ${path} returned HTTP ${status}" >&2
    printf '%s\n' "${LAST_HTTP_BODY}" >&2
    return 1
  fi

  printf '%s' "${LAST_HTTP_BODY}"
}

delete_root_process_group_by_name() {
  local target_name="$1"
  nifi_request GET /flow/process-groups/root >"${tmpdir}/root-flow.json"
  mapfile -t existing_ids < <(python3 - "${tmpdir}/root-flow.json" "${target_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
flow = payload.get("processGroupFlow", {}).get("flow", {})
for process_group in flow.get("processGroups", []):
    component = process_group.get("component", {})
    if component.get("name") == target_name:
        print(process_group.get("id") or component.get("id") or "")
PY
  )

  for existing_id in "${existing_ids[@]}"; do
    [[ -z "${existing_id}" ]] && continue
    nifi_request GET "/process-groups/${existing_id}" >"${tmpdir}/existing-process-group.json"
    local existing_version
    existing_version="$(python3 - "${tmpdir}/existing-process-group.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload.get("revision", {}).get("version", 0))
PY
)"
    nifi_request DELETE "/process-groups/${existing_id}?version=${existing_version}&clientId=restore-proof-delete" >/dev/null
  done
}

delete_parameter_context_by_name() {
  local target_name="$1"
  nifi_request GET /flow/parameter-contexts >"${tmpdir}/flow-parameter-contexts.json"
  mapfile -t existing_ids < <(python3 - "${tmpdir}/flow-parameter-contexts.json" "${target_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
for context in payload.get("parameterContexts", []):
    component = context.get("component", {})
    if component.get("name") == target_name:
        print(context.get("id") or component.get("id") or "")
PY
  )

  for existing_id in "${existing_ids[@]}"; do
    [[ -z "${existing_id}" ]] && continue
    nifi_request GET "/parameter-contexts/${existing_id}" >"${tmpdir}/existing-parameter-context.json"
    local existing_version
    existing_version="$(python3 - "${tmpdir}/existing-parameter-context.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload.get("revision", {}).get("version", 0))
PY
)"
    nifi_request DELETE "/parameter-contexts/${existing_id}?version=${existing_version}&clientId=restore-proof-delete" >/dev/null
  done
}

delete_root_process_group_by_name "${target_root_pg_name}"
if [[ -n "${parameter_context_ref_name}" ]]; then
  delete_parameter_context_by_name "${parameter_context_ref_name}"
fi

created_parameter_context_id=""
if [[ -n "${parameter_context_ref_name}" ]]; then
  python3 - "${tmpdir}/selected-contexts.json" "${namespace}" >"${tmpdir}/parameter-context-create.json" <<'PY'
import json
import subprocess
import sys

namespace = sys.argv[2]
catalog = json.load(open(sys.argv[1], encoding="utf-8"))
if not catalog:
    raise SystemExit("selected parameter context list is empty")

context = catalog[0]
parameters = []
for entry in context.get("parameters", []):
    parameter = {
        "name": entry["name"],
        "sensitive": bool(entry.get("sensitive", False)),
    }
    if entry.get("description"):
        parameter["description"] = entry["description"]

    source = entry.get("source", {}).get("type")
    if source == "secretRef":
        secret_ref = entry.get("secretRef", {})
        secret_name = secret_ref.get("name")
        secret_key = secret_ref.get("key")
        if not secret_name or not secret_key:
            raise SystemExit(f"incomplete secretRef for parameter {entry['name']!r}")
        result = subprocess.run(
            [
                "kubectl",
                "-n",
                namespace,
                "get",
                "secret",
                secret_name,
                "-o",
                f"jsonpath={{.data.{secret_key}}}",
            ],
            check=True,
            capture_output=True,
            text=True,
        )
        parameter["value"] = subprocess.run(
            ["python3", "-c", "import base64,sys; print(base64.b64decode(sys.stdin.read()).decode())"],
            input=result.stdout,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.rstrip("\n")
    else:
        parameter["value"] = entry.get("value", "")

    parameters.append({"parameter": parameter})

payload = {
    "revision": {
        "clientId": "restore-proof",
        "version": 0,
    },
    "component": {
        "name": context["name"],
        "description": context.get("description", ""),
        "parameters": parameters,
    },
}
json.dump(payload, sys.stdout)
PY
  created_parameter_context="$(nifi_request POST /parameter-contexts "$(tr -d '\n' < "${tmpdir}/parameter-context-create.json")")"
  printf '%s' "${created_parameter_context}" >"${tmpdir}/created-parameter-context.json"
  created_parameter_context_id="$(
    python3 - "${tmpdir}/created-parameter-context.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload.get("id") or payload.get("component", {}).get("id") or "")
PY
  )"
fi

kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- env STATE_URL="${github_mock_state_url}" sh -ec 'curl --silent "${STATE_URL}"' >"${tmpdir}/github-state.json"
python3 - "${tmpdir}/github-state.json" "${bucket_name}" "${flow_name}" >"${tmpdir}/flow-snapshot.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
bucket_name = sys.argv[2]
flow_name = sys.argv[3]
path = f"flows/{bucket_name}/{flow_name}.json"
entry = payload.get("files", {}).get(path)
if entry is None:
    raise SystemExit(f"registry-backed snapshot {path!r} not found in GitHub evaluator state")
snapshot = json.loads(entry["content"])
json.dump(snapshot, sys.stdout)
PY

python3 - "${tmpdir}/flow-snapshot.json" "${target_root_pg_name}" >"${tmpdir}/import-process-group.json" <<'PY'
import json
import sys

snapshot = json.load(open(sys.argv[1], encoding="utf-8"))
target_name = sys.argv[2]
payload = {
    "revisionDTO": {
        "clientId": "restore-proof",
        "version": 0,
    },
    "groupName": target_name,
    "positionDTO": {
        "x": 400.0,
        "y": 400.0,
    },
    "flowSnapshot": snapshot,
}
json.dump(payload, sys.stdout)
PY

created_process_group="$(
  nifi_request POST /process-groups/root/process-groups/import "$(tr -d '\n' < "${tmpdir}/import-process-group.json")"
)"
printf '%s' "${created_process_group}" >"${tmpdir}/created-process-group.json"

mapfile -t imported_process_group_meta < <(python3 - "${tmpdir}/created-process-group.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload.get("id") or payload.get("component", {}).get("id") or "")
print(payload.get("revision", {}).get("version", 0))
PY
)

imported_process_group_id="${imported_process_group_meta[0]:-}"
imported_process_group_revision="${imported_process_group_meta[1]:-0}"

if [[ -z "${imported_process_group_id}" ]]; then
  echo "failed to import the selected flow snapshot into the root process group" >&2
  exit 1
fi

if [[ -n "${created_parameter_context_id}" ]]; then
  python3 - "${imported_process_group_id}" "${imported_process_group_revision}" "${created_parameter_context_id}" >"${tmpdir}/bind-parameter-context.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "restore-proof",
        "version": int(sys.argv[2]),
    },
    "component": {
        "id": sys.argv[1],
        "parameterContext": {
            "id": sys.argv[3],
        },
    },
}
json.dump(payload, sys.stdout)
PY
  nifi_request PUT "/process-groups/${imported_process_group_id}" "$(tr -d '\n' < "${tmpdir}/bind-parameter-context.json")" >"${tmpdir}/bound-process-group.json"
else
  nifi_request GET "/process-groups/${imported_process_group_id}" >"${tmpdir}/bound-process-group.json"
fi

python3 - "${tmpdir}/bound-process-group.json" "${target_root_pg_name}" "${created_parameter_context_id}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
expected_name = sys.argv[2]
expected_context_id = sys.argv[3]
component = payload.get("component", {})
actual_name = component.get("name")
if actual_name != expected_name:
    raise SystemExit(f"expected imported process group name {expected_name!r}, got {actual_name!r}")

actual_context_id = component.get("parameterContext", {}).get("id", "")
if expected_context_id and actual_context_id != expected_context_id:
    raise SystemExit(
        f"expected imported process group to bind parameter context {expected_context_id!r}, got {actual_context_id!r}"
    )
PY

echo
echo "SUCCESS: bounded flow config restore proof completed"
echo "  import name: ${selected_import_name}"
echo "  registry client: ${registry_client_name}"
echo "  bucket: ${bucket_name}"
echo "  flow name: ${flow_name}"
echo "  selected version: ${selected_version}"
echo "  imported process group: ${target_root_pg_name} (${imported_process_group_id})"
echo "  restored parameter context: ${parameter_context_ref_name:-none} (${created_parameter_context_id:-none})"
