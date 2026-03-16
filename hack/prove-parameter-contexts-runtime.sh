#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
context_name="platform-runtime"
expected_inline_parameter="external.api.baseUrl"
expected_inline_value=""
expected_sensitive_parameter="external.api.token"
expected_action=""
expected_root_process_group_name=""
expected_deleted_context_name=""
status_file="/opt/nifi/nifi-current/logs/parameter-contexts-bootstrap-status.json"

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
    --context-name)
      context_name="$2"
      shift 2
      ;;
    --expected-inline-parameter)
      expected_inline_parameter="$2"
      shift 2
      ;;
    --expected-inline-value)
      expected_inline_value="$2"
      shift 2
      ;;
    --expected-sensitive-parameter)
      expected_sensitive_parameter="$2"
      shift 2
      ;;
    --expected-action)
      expected_action="$2"
      shift 2
      ;;
    --expected-root-process-group-name)
      expected_root_process_group_name="$2"
      shift 2
      ;;
    --expected-deleted-context-name)
      expected_deleted_context_name="$2"
      shift 2
      ;;
    --status-file)
      status_file="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

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

ensure_root_process_group() {
  local target_name="$1"
  [[ -z "${target_name}" ]] && return 0

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
        "clientId": "parameter-context-proof",
        "version": 0,
    },
    "component": {
        "name": sys.argv[1],
        "position": {"x": 240.0, "y": 240.0},
    },
}
json.dump(payload, sys.stdout)
PY
  nifi_request POST /process-groups/root/process-groups "$(tr -d '\n' < "${tmpdir}/create-process-group.json")" >/dev/null
}

ensure_root_process_group "${expected_root_process_group_name}"

kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- cat "${status_file}" >"${tmpdir}/parameter-context-status.json"
nifi_request GET /flow/parameter-contexts >"${tmpdir}/flow-parameter-contexts.json"

context_id="$(
  python3 - "${tmpdir}/flow-parameter-contexts.json" "${context_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
for entry in payload.get("parameterContexts", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        print(entry.get("id") or component.get("id") or "")
        break
PY
)"

if [[ -z "${context_id}" ]]; then
  echo "expected runtime-managed parameter context ${context_name} to exist in NiFi" >&2
  exit 1
fi

nifi_request GET "/parameter-contexts/${context_id}" >"${tmpdir}/parameter-context.json"
if [[ -n "${expected_root_process_group_name}" ]]; then
  nifi_request GET /flow/process-groups/root >"${tmpdir}/root-flow.json"
  root_process_group_id="$(
    python3 - "${tmpdir}/root-flow.json" "${expected_root_process_group_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
for entry in payload.get("processGroupFlow", {}).get("flow", {}).get("processGroups", []):
    component = entry.get("component", {})
    if component.get("name") == target:
        print(entry.get("id") or component.get("id") or "")
        break
PY
  )"

  if [[ -z "${root_process_group_id}" ]]; then
    echo "expected direct root-child process group ${expected_root_process_group_name} to exist in NiFi" >&2
    exit 1
  fi

  nifi_request GET "/process-groups/${root_process_group_id}" >"${tmpdir}/root-process-group.json"
fi

python3 - \
  "${tmpdir}/parameter-context-status.json" \
  "${tmpdir}/parameter-context.json" \
  "${context_name}" \
  "${expected_inline_parameter}" \
  "${expected_inline_value}" \
  "${expected_sensitive_parameter}" \
  "${expected_action}" \
  "${expected_root_process_group_name}" \
  "${tmpdir}/root-process-group.json" \
  "${expected_deleted_context_name}" \
  "${tmpdir}/flow-parameter-contexts.json" <<'PY'
import json
import sys

status = json.load(open(sys.argv[1], encoding="utf-8"))
entity = json.load(open(sys.argv[2], encoding="utf-8"))
context_name = sys.argv[3]
expected_inline_parameter = sys.argv[4]
expected_inline_value = sys.argv[5]
expected_sensitive_parameter = sys.argv[6]
expected_action = sys.argv[7]
expected_root_process_group_name = sys.argv[8]
root_process_group_path = sys.argv[9]
expected_deleted_context_name = sys.argv[10]
parameter_contexts = json.load(open(sys.argv[11], encoding="utf-8"))

if status.get("status") != "ok":
    raise SystemExit(f"expected bootstrap status to be ok, got {status.get('status')!r}: {status}")

context_status = None
for entry in status.get("contexts", []):
    if entry.get("name") == context_name:
        context_status = entry
        break

if context_status is None:
    raise SystemExit(f"expected bootstrap status for context {context_name!r}, got {status}")

if expected_action:
    allowed_actions = [item.strip() for item in expected_action.split(",") if item.strip()]
    if context_status.get("action") not in allowed_actions:
        raise SystemExit(
            f"expected context action in {allowed_actions!r}, got {context_status.get('action')!r}: {context_status}"
        )

component = entity.get("component", {})
if component.get("name") != context_name:
    raise SystemExit(f"expected parameter context name {context_name!r}, got {component.get('name')!r}")

parameters = {}
for entry in component.get("parameters", []):
    parameter = entry.get("parameter", entry)
    parameters[parameter.get("name")] = parameter

inline_parameter = parameters.get(expected_inline_parameter)
if inline_parameter is None:
    raise SystemExit(f"expected inline parameter {expected_inline_parameter!r} in runtime context")
if inline_parameter.get("value") != expected_inline_value:
    raise SystemExit(
        f"expected inline parameter value {expected_inline_value!r}, got {inline_parameter.get('value')!r}"
    )

sensitive_parameter = parameters.get(expected_sensitive_parameter)
sensitive_status = None
sensitive_resolved = None
if expected_sensitive_parameter:
    if sensitive_parameter is None:
        raise SystemExit(f"expected sensitive parameter {expected_sensitive_parameter!r} in runtime context")
    if sensitive_parameter.get("sensitive") is not True:
        raise SystemExit(f"expected sensitive parameter {expected_sensitive_parameter!r} to be marked sensitive")

    for entry in context_status.get("parameters", []):
        if entry.get("name") == expected_sensitive_parameter:
            sensitive_status = entry
            break

    if sensitive_status is None:
        raise SystemExit(f"expected bootstrap status for sensitive parameter {expected_sensitive_parameter!r}")
    if sensitive_status.get("source") != "secretRef":
        raise SystemExit(f"expected sensitive parameter source secretRef, got {sensitive_status.get('source')!r}")
    if sensitive_status.get("secretResolved") is not True:
        raise SystemExit(f"expected sensitive parameter secret material to resolve successfully: {sensitive_status}")
    sensitive_resolved = sensitive_status.get("secretResolved")

attached_process_group = None
if expected_root_process_group_name:
    root_process_group = json.load(open(root_process_group_path, encoding="utf-8"))
    attached_process_group = root_process_group.get("component", {})
    actual_context_id = attached_process_group.get("parameterContext", {}).get("id", "")
    expected_context_id = entity.get("id") or component.get("id")
    if actual_context_id != expected_context_id:
        raise SystemExit(
            f"expected root child process group {expected_root_process_group_name!r} to bind context id {expected_context_id!r}, got {actual_context_id!r}"
        )

    attachment_status = None
    for entry in context_status.get("attachmentStatuses", []):
        if entry.get("rootProcessGroupName") == expected_root_process_group_name:
            attachment_status = entry
            break
    if attachment_status is None:
        raise SystemExit(f"expected attachment status for root child process group {expected_root_process_group_name!r}")
    if attachment_status.get("result") != "ok":
        raise SystemExit(f"expected attachment status ok, got {attachment_status}")

if expected_deleted_context_name:
    for entry in parameter_contexts.get("parameterContexts", []):
        candidate = entry.get("component", {})
        if candidate.get("name") == expected_deleted_context_name:
            raise SystemExit(f"expected removed owned context {expected_deleted_context_name!r} to be deleted from NiFi")

    deleted_status = None
    for entry in status.get("deletedContexts", []):
        if entry.get("name") == expected_deleted_context_name:
            deleted_status = entry
            break
    if deleted_status is None:
        raise SystemExit(f"expected deletedContexts status entry for removed context {expected_deleted_context_name!r}")
    if deleted_status.get("result") != "ok":
        raise SystemExit(f"expected deleted context status ok, got {deleted_status}")

print(
    json.dumps(
        {
            "context": context_name,
            "action": context_status.get("action"),
            "inlineParameter": expected_inline_parameter,
            "inlineValue": inline_parameter.get("value"),
            "sensitiveParameter": expected_sensitive_parameter,
            "sensitiveResolved": sensitive_resolved,
            "contextId": entity.get("id") or component.get("id"),
            "rootProcessGroupName": expected_root_process_group_name,
            "deletedContextName": expected_deleted_context_name,
        },
        indent=2,
        sort_keys=True,
    )
)
PY

echo
echo "SUCCESS: bounded runtime-managed parameter context proof completed"
echo "  context: ${context_name}"
echo "  expected action: ${expected_action:-any}"
echo "  inline parameter: ${expected_inline_parameter}=${expected_inline_value}"
if [[ -n "${expected_sensitive_parameter}" ]]; then
  echo "  sensitive parameter: ${expected_sensitive_parameter}"
else
  echo "  sensitive parameter: none"
fi
if [[ -n "${expected_root_process_group_name}" ]]; then
  echo "  attached root child process group: ${expected_root_process_group_name}"
fi
if [[ -n "${expected_deleted_context_name}" ]]; then
  echo "  deleted owned context: ${expected_deleted_context_name}"
fi
echo "  status file: ${status_file}"
