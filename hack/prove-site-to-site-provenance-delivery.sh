#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

sender_namespace="nifi"
sender_release="nifi"
sender_auth_secret="nifi-auth"
sender_expected_destination_url=""
sender_expected_input_port="nifi-provenance"
sender_expected_transport="HTTP"
sender_expected_platform="nifi"
sender_expected_auth_type=""
sender_expected_authorized_identity=""
sender_expected_auth_secret_ref_name=""
sender_expected_start_position="endOfStream"
sender_processor_name="site-to-site-provenance-proof-generate"
receiver_namespace="site-to-site-receiver"
receiver_release="site-to-site-receiver"
receiver_auth_secret="site-to-site-receiver-auth"
receiver_expected_authorized_identity=""
receiver_input_port_name="nifi-provenance"
receiver_processor_name="site-to-site-receiver-log"
delivery_timeout_seconds="180"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --sender-namespace)
      sender_namespace="$2"
      shift 2
      ;;
    --sender-release)
      sender_release="$2"
      shift 2
      ;;
    --sender-auth-secret)
      sender_auth_secret="$2"
      shift 2
      ;;
    --sender-expected-destination-url)
      sender_expected_destination_url="$2"
      shift 2
      ;;
    --sender-expected-input-port)
      sender_expected_input_port="$2"
      shift 2
      ;;
    --sender-expected-transport)
      sender_expected_transport="$2"
      shift 2
      ;;
    --sender-expected-platform)
      sender_expected_platform="$2"
      shift 2
      ;;
    --sender-expected-auth-type)
      sender_expected_auth_type="$2"
      shift 2
      ;;
    --sender-expected-authorized-identity)
      sender_expected_authorized_identity="$2"
      shift 2
      ;;
    --sender-expected-auth-secret-ref-name)
      sender_expected_auth_secret_ref_name="$2"
      shift 2
      ;;
    --sender-expected-start-position)
      sender_expected_start_position="$2"
      shift 2
      ;;
    --sender-processor-name)
      sender_processor_name="$2"
      shift 2
      ;;
    --receiver-namespace)
      receiver_namespace="$2"
      shift 2
      ;;
    --receiver-release)
      receiver_release="$2"
      shift 2
      ;;
    --receiver-auth-secret)
      receiver_auth_secret="$2"
      shift 2
      ;;
    --receiver-expected-authorized-identity)
      receiver_expected_authorized_identity="$2"
      shift 2
      ;;
    --receiver-input-port)
      receiver_input_port_name="$2"
      shift 2
      ;;
    --receiver-processor-name)
      receiver_processor_name="$2"
      shift 2
      ;;
    --delivery-timeout-seconds)
      delivery_timeout_seconds="$2"
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

sender_pod="${sender_release}-0"
sender_host="${sender_release}-0.${sender_release}-headless.${sender_namespace}.svc.cluster.local"
sender_base_url="https://${sender_host}:8443/nifi-api"
sender_username="$(kubectl -n "${sender_namespace}" get secret "${sender_auth_secret}" -o jsonpath='{.data.username}' | base64 -d)"
sender_password="$(kubectl -n "${sender_namespace}" get secret "${sender_auth_secret}" -o jsonpath='{.data.password}' | base64 -d)"
sender_token=""
sender_processor_id=""

dump_receiver_diagnostics() {
  set +e
  echo
  echo "==> Site-to-Site provenance delivery diagnostics"
  kubectl -n "${receiver_namespace}" get pods,svc,secret || true
  kubectl -n "${receiver_namespace}" get events --sort-by=.lastTimestamp | tail -n 80 || true
  kubectl -n "${receiver_namespace}" logs "${receiver_release}-0" -c nifi --tail=200 || true
  kubectl -n "${sender_namespace}" logs "${sender_release}-0" -c nifi --tail=200 || true
  if [[ -f "${tmpdir}/sender-processor.json" ]]; then
    echo
    echo "sender proof processor entity:"
    cat "${tmpdir}/sender-processor.json"
  fi
  if [[ -f "${tmpdir}/receiver-input-port.json" ]]; then
    echo
    echo "receiver input port entity:"
    cat "${tmpdir}/receiver-input-port.json"
  fi
  if [[ -f "${tmpdir}/receiver-input-port-status.json" ]]; then
    echo
    echo "receiver input port status:"
    cat "${tmpdir}/receiver-input-port-status.json"
  fi
  if [[ -f "${tmpdir}/receiver-processor.json" ]]; then
    echo
    echo "receiver proof processor entity:"
    cat "${tmpdir}/receiver-processor.json"
  fi
  if [[ -f "${tmpdir}/receiver-processor-status.json" ]]; then
    echo
    echo "receiver proof processor status:"
    cat "${tmpdir}/receiver-processor-status.json"
  fi
}

sender_curl() {
  local method="$1"
  local path="$2"
  local body="${3:-}"

  kubectl -n "${sender_namespace}" exec -i -c nifi "${sender_pod}" -- env \
    NIFI_USERNAME="${sender_username}" \
    NIFI_PASSWORD="${sender_password}" \
    NIFI_TOKEN="${sender_token:-}" \
    NIFI_BASE_URL="${sender_base_url}" \
    REQUEST_METHOD="${method}" \
    REQUEST_PATH="${path}" \
    REQUEST_BODY="${body}" \
    sh -ec '
      if [ -z "${NIFI_TOKEN}" ]; then
        curl --silent --show-error --fail \
          --cacert /opt/nifi/tls/ca.crt \
          -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
          --data-urlencode "username=${NIFI_USERNAME}" \
          --data-urlencode "password=${NIFI_PASSWORD}" \
          "${NIFI_BASE_URL}/access/token"
      elif [ -n "${REQUEST_BODY}" ]; then
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

sender_curl_retry() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local attempts="${4:-30}"
  local sleep_seconds="${5:-2}"
  local output
  local attempt

  for attempt in $(seq 1 "${attempts}"); do
    if output="$(sender_curl "${method}" "${path}" "${body}")"; then
      printf '%s' "${output}"
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  return 1
}

cleanup_sender_processor() {
  set +e
  if [[ -n "${sender_processor_id}" ]]; then
    sender_curl_retry GET "/processors/${sender_processor_id}" "" 5 1 >"${tmpdir}/sender-processor-final.json" || return 0
    local version
    version="$(python3 - "${tmpdir}/sender-processor-final.json" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1]))
print(payload.get("revision", {}).get("version", 0))
PY
)"
    sender_curl PUT "/processors/${sender_processor_id}/run-status" "{\"revision\":{\"version\":${version}},\"state\":\"STOPPED\",\"disconnectedNodeAcknowledged\":false}" >/dev/null 2>&1 || true
    sender_curl_retry GET "/processors/${sender_processor_id}" "" 5 1 >"${tmpdir}/sender-processor-final.json" || return 0
    version="$(python3 - "${tmpdir}/sender-processor-final.json" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1]))
print(payload.get("revision", {}).get("version", 0))
PY
)"
    sender_curl DELETE "/processors/${sender_processor_id}?version=${version}&disconnectedNodeAcknowledged=false" >/dev/null 2>&1 || true
  fi
}

trap 'echo "FAIL: typed Site-to-Site provenance delivery proof failed" >&2; cleanup_sender_processor; dump_receiver_diagnostics; exit 1' ERR
trap 'cleanup_sender_processor' EXIT

sender_expect_ssl_service="true"
if [[ "${sender_expected_auth_type}" == "none" ]]; then
  sender_expect_ssl_service="false"
fi

bash "${ROOT_DIR}/hack/prove-site-to-site-provenance.sh" \
  --namespace "${sender_namespace}" \
  --release "${sender_release}" \
  --auth-secret "${sender_auth_secret}" \
  --expected-destination-url "${sender_expected_destination_url}" \
  --expected-input-port "${sender_expected_input_port}" \
  --expected-transport "${sender_expected_transport}" \
  --expected-platform "${sender_expected_platform}" \
  --expected-auth-type "${sender_expected_auth_type}" \
  --expected-authorized-identity "${sender_expected_authorized_identity}" \
  --expected-auth-secret-ref-name "${sender_expected_auth_secret_ref_name}" \
  --expected-start-position "${sender_expected_start_position}" \
  --expect-ssl-service "${sender_expect_ssl_service}"

for _ in $(seq 1 40); do
  if sender_token="$(sender_curl GET "")"; then
    break
  fi
  sleep 2
done

if [[ -z "${sender_token}" ]]; then
  echo "timed out waiting for sender NiFi API token" >&2
  exit 1
fi

sender_curl_retry GET /flow/process-groups/root "" 30 2 >"${tmpdir}/sender-root-flow.json"
sender_root_group_id="$(
  python3 - "${tmpdir}/sender-root-flow.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
flow = payload.get("processGroupFlow", {})
print(flow.get("id") or flow.get("breadcrumb", {}).get("breadcrumb", {}).get("id") or "")
PY
)"
if [[ -z "${sender_root_group_id}" ]]; then
  echo "failed to resolve sender root process group id" >&2
  exit 1
fi

kubectl -n "${sender_namespace}" exec -i -c nifi "${sender_pod}" -- cat /opt/nifi/nifi-current/conf/users.xml >"${tmpdir}/sender-users.xml"
sender_admin_user_id="$(
  python3 - "${tmpdir}/sender-users.xml" "${sender_username}" <<'PY'
import sys
import xml.etree.ElementTree as ET

root = ET.parse(sys.argv[1]).getroot()
identity = sys.argv[2]
for user in root.findall(".//user"):
    if user.get("identity") == identity:
        print(user.get("identifier", ""))
        raise SystemExit(0)
raise SystemExit("failed to resolve sender admin user id")
PY
)"

sender_create_policy_payload() {
  local action="$1"
  local resource="$2"
  python3 - "${action}" "${resource}" "${sender_admin_user_id}" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-provenance-proof",
        "version": 0,
    },
    "component": {
        "action": sys.argv[1],
        "resource": sys.argv[2],
        "users": [{"id": sys.argv[3]}],
    },
}
json.dump(payload, sys.stdout)
PY
}

sender_policy_id_for() {
  local action="$1"
  local resource="$2"
  local policy_action=""

  case "${action}" in
    read) policy_action="R" ;;
    write) policy_action="W" ;;
    *)
      echo "unsupported sender policy action lookup: ${action}" >&2
      return 1
      ;;
  esac

  kubectl -n "${sender_namespace}" exec -i -c nifi "${sender_pod}" -- env \
    POLICY_ACTION="${policy_action}" \
    POLICY_RESOURCE="${resource}" \
    python3 - <<'PY'
import os
import xml.etree.ElementTree as ET

root = ET.parse("/opt/nifi/nifi-current/conf/authorizations.xml").getroot()
for policy in root.findall(".//policy"):
    if (
        policy.get("action") == os.environ["POLICY_ACTION"]
        and policy.get("resource") == os.environ["POLICY_RESOURCE"]
    ):
        print(policy.get("identifier", ""))
        break
PY
}

sender_ensure_policy_user_binding() {
  local action="$1"
  local resource="$2"
  local policy_id=""

  policy_id="$(sender_policy_id_for "${action}" "${resource}")"
  if [[ -z "${policy_id}" ]]; then
    sender_curl_retry POST /policies "$(sender_create_policy_payload "${action}" "${resource}")" 30 2 >/dev/null
    return 0
  fi

  sender_curl_retry GET "/policies/${policy_id}" "" 20 2 >"${tmpdir}/sender-policy-${policy_id}.json"
  if python3 - "${tmpdir}/sender-policy-${policy_id}.json" "${sender_admin_user_id}" >"${tmpdir}/sender-policy-${policy_id}-update.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
user_id = sys.argv[2]
component = payload.get("component", {})
users = component.get("users", [])
user_ids = {user.get("id") for user in users}
if user_id in user_ids:
    raise SystemExit(1)

users.append({"id": user_id})
update = {
    "revision": {
        "clientId": "site-to-site-provenance-proof",
        "version": payload.get("revision", {}).get("version", 0),
    },
    "component": {
        "id": component.get("id"),
        "resource": component.get("resource"),
        "action": component.get("action"),
        "users": [{"id": user.get("id")} for user in users],
        "userGroups": [{"id": group.get("id")} for group in component.get("userGroups", [])],
    },
}
json.dump(update, sys.stdout)
PY
  then
    sender_curl_retry PUT "/policies/${policy_id}" "$(tr -d '\n' < "${tmpdir}/sender-policy-${policy_id}-update.json")" 20 2 >/dev/null
  fi
}

sender_ensure_policy_user_binding read "/process-groups/${sender_root_group_id}"
sender_ensure_policy_user_binding write "/process-groups/${sender_root_group_id}"

existing_sender_processor_id="$(
  python3 - "${tmpdir}/sender-root-flow.json" "${sender_processor_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
for processor in payload.get("processGroupFlow", {}).get("flow", {}).get("processors", []):
    component = processor.get("component", {})
    if component.get("name") == target:
        print(processor.get("id") or component.get("id") or "")
        raise SystemExit(0)
print("")
PY
)"
if [[ -n "${existing_sender_processor_id}" ]]; then
  sender_processor_id="${existing_sender_processor_id}"
  cleanup_sender_processor
  sender_processor_id=""
fi

python3 - "${sender_processor_name}" >"${tmpdir}/create-sender-processor.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-provenance-proof",
        "version": 0,
    },
    "component": {
        "name": sys.argv[1],
        "type": "org.apache.nifi.processors.standard.GenerateFlowFile",
        "bundle": {
            "group": "org.apache.nifi",
            "artifact": "nifi-standard-nar",
            "version": "2.0.0",
        },
        "position": {"x": 120.0, "y": 120.0},
        "config": {
            "schedulingPeriod": "1 sec",
            "autoTerminatedRelationships": ["success"],
            "properties": {
                "File Size": "1 KB",
                "Batch Size": "1",
                "Data Format": "Text",
                "Unique FlowFiles": "true",
            },
        },
    },
}
json.dump(payload, sys.stdout)
PY

sender_curl_retry POST /process-groups/root/processors "$(tr -d '\n' < "${tmpdir}/create-sender-processor.json")" 30 2 >"${tmpdir}/sender-processor.json"
mapfile -t sender_processor_meta < <(python3 - "${tmpdir}/sender-processor.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
component = payload.get("component", {})
revision = payload.get("revision", {})
print(payload.get("id") or component.get("id") or "")
print(revision.get("version", 0))
PY
)

sender_processor_id="${sender_processor_meta[0]:-}"
sender_processor_version="${sender_processor_meta[1]:-0}"

if [[ -z "${sender_processor_id}" ]]; then
  echo "failed to create sender provenance proof processor" >&2
  exit 1
fi

python3 - "${sender_processor_version}" >"${tmpdir}/start-sender-processor.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-provenance-proof",
        "version": int(sys.argv[1]),
    },
    "state": "RUNNING",
    "disconnectedNodeAcknowledged": False,
}
json.dump(payload, sys.stdout)
PY

sender_curl_retry PUT "/processors/${sender_processor_id}/run-status" "$(tr -d '\n' < "${tmpdir}/start-sender-processor.json")" 30 2 >/dev/null
sender_curl_retry GET "/processors/${sender_processor_id}" "" 20 2 >"${tmpdir}/sender-processor-running.json"
python3 - "${tmpdir}/sender-processor-running.json" "${sender_processor_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target_name = sys.argv[2]
component = payload.get("component", {})
if component.get("name") != target_name:
    raise SystemExit(f"expected sender proof processor {target_name!r}, got {component.get('name')!r}")
if component.get("state") != "RUNNING":
    raise SystemExit(f"expected sender proof processor RUNNING, got {component.get('state')!r}")
print("ok")
PY

receiver_pod="${receiver_release}-0"
receiver_host="${receiver_release}-0.${receiver_release}-headless.${receiver_namespace}.svc.cluster.local"
receiver_base_url="https://${receiver_host}:8443/nifi-api"
receiver_username="$(kubectl -n "${receiver_namespace}" get secret "${receiver_auth_secret}" -o jsonpath='{.data.username}' | base64 -d)"
receiver_password="$(kubectl -n "${receiver_namespace}" get secret "${receiver_auth_secret}" -o jsonpath='{.data.password}' | base64 -d)"
receiver_token=""

receiver_curl() {
  local method="$1"
  local path="$2"
  local body="${3:-}"

  kubectl -n "${receiver_namespace}" exec -i -c nifi "${receiver_pod}" -- env \
    NIFI_USERNAME="${receiver_username}" \
    NIFI_PASSWORD="${receiver_password}" \
    NIFI_TOKEN="${receiver_token:-}" \
    NIFI_BASE_URL="${receiver_base_url}" \
    REQUEST_METHOD="${method}" \
    REQUEST_PATH="${path}" \
    REQUEST_BODY="${body}" \
    sh -ec '
      if [ -z "${NIFI_TOKEN}" ]; then
        curl --silent --show-error --fail \
          --cacert /opt/nifi/tls/ca.crt \
          -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
          --data-urlencode "username=${NIFI_USERNAME}" \
          --data-urlencode "password=${NIFI_PASSWORD}" \
          "${NIFI_BASE_URL}/access/token"
      elif [ -n "${REQUEST_BODY}" ]; then
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

receiver_curl_retry() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local attempts="${4:-30}"
  local sleep_seconds="${5:-2}"
  local output
  local attempt

  for attempt in $(seq 1 "${attempts}"); do
    if output="$(receiver_curl "${method}" "${path}" "${body}")"; then
      printf '%s' "${output}"
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  return 1
}

for _ in $(seq 1 40); do
  if receiver_token="$(receiver_curl GET "")"; then
    break
  fi
  sleep 2
done

if [[ -z "${receiver_token}" ]]; then
  echo "timed out waiting for receiver NiFi API token" >&2
  exit 1
fi

receiver_curl_retry GET /flow/process-groups/root "" 30 2 >"${tmpdir}/receiver-root-flow.json"
mapfile -t receiver_component_ids < <(
  python3 - "${tmpdir}/receiver-root-flow.json" "${receiver_input_port_name}" "${receiver_processor_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target_port = sys.argv[2]
target_processor = sys.argv[3]
ports = payload.get("processGroupFlow", {}).get("flow", {}).get("inputPorts", [])
for port in ports:
    component = port.get("component", {})
    if component.get("name") == target_port:
        print(port.get("id") or component.get("id") or "")
        break
else:
    print("")

processors = payload.get("processGroupFlow", {}).get("flow", {}).get("processors", [])
for processor in processors:
    component = processor.get("component", {})
    if component.get("name") == target_processor:
        print(processor.get("id") or component.get("id") or "")
        break
else:
    print("")
PY
)

receiver_port_id="${receiver_component_ids[0]:-}"
receiver_processor_id="${receiver_component_ids[1]:-}"

if [[ -z "${receiver_port_id}" ]]; then
  echo "expected receiver input port ${receiver_input_port_name} to exist" >&2
  exit 1
fi

if [[ -z "${receiver_processor_id}" ]]; then
  echo "expected receiver proof processor ${receiver_processor_name} to exist" >&2
  exit 1
fi

receiver_curl_retry GET "/input-ports/${receiver_port_id}" "" 20 2 >"${tmpdir}/receiver-input-port.json"
python3 - "${tmpdir}/receiver-input-port.json" "${receiver_input_port_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
component = payload.get("component", {})
if component.get("name") != target:
    raise SystemExit(f"expected receiver input port name {target!r}, got {component.get('name')!r}")
if component.get("state") != "RUNNING":
    raise SystemExit(f"expected receiver input port state RUNNING, got {component.get('state')!r}")
if component.get("allowRemoteAccess") is not True:
    raise SystemExit("expected receiver input port to allow remote access")
print("ok")
PY

receiver_curl_retry GET "/processors/${receiver_processor_id}" "" 20 2 >"${tmpdir}/receiver-processor.json"
python3 - "${tmpdir}/receiver-processor.json" "${receiver_processor_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target_name = sys.argv[2]
component = payload.get("component", {})
if component.get("name") != target_name:
    raise SystemExit(f"expected receiver processor name {target_name!r}, got {component.get('name')!r}")
if component.get("state") != "RUNNING":
    raise SystemExit(f"expected receiver processor state RUNNING, got {component.get('state')!r}")
print("ok")
PY

if [[ -n "${receiver_expected_authorized_identity}" ]]; then
  kubectl -n "${receiver_namespace}" exec -i -c nifi "${receiver_pod}" -- cat /opt/nifi/nifi-current/conf/users.xml >"${tmpdir}/receiver-users.xml"
  kubectl -n "${receiver_namespace}" exec -i -c nifi "${receiver_pod}" -- cat /opt/nifi/nifi-current/conf/authorizations.xml >"${tmpdir}/receiver-authorizations.xml"
  python3 - "${tmpdir}/receiver-users.xml" "${tmpdir}/receiver-authorizations.xml" "${receiver_expected_authorized_identity}" "${receiver_port_id}" <<'PY'
import sys
import xml.etree.ElementTree as ET

users_root = ET.parse(sys.argv[1]).getroot()
authz_root = ET.parse(sys.argv[2]).getroot()
expected_identity = sys.argv[3]
port_id = sys.argv[4]

user_id = ""
for user in users_root.findall(".//user"):
    if user.get("identity") == expected_identity:
        user_id = user.get("identifier", "")
        break

if not user_id:
    raise SystemExit(f"expected receiver users.xml to contain identity {expected_identity!r}")

required_policies = [
    ("/controller", "R"),
    ("/site-to-site", "R"),
    (f"/data-transfer/input-ports/{port_id}", "W"),
]

for resource, action in required_policies:
    policy = None
    for candidate in authz_root.findall(".//policy"):
        if candidate.get("resource") == resource and candidate.get("action") == action:
            policy = candidate
            break
    if policy is None:
        raise SystemExit(f"expected receiver authorizations.xml to contain policy {(resource, action)!r}")
    user_ids = {entry.get("identifier") for entry in policy.findall("./user")}
    if user_id not in user_ids:
        raise SystemExit(f"expected receiver policy {(resource, action)!r} to bind identity {expected_identity!r}")

print("ok")
PY
fi

deadline=$(( $(date +%s) + delivery_timeout_seconds ))
delivery_ok=""
while (( $(date +%s) < deadline )); do
  receiver_curl_retry GET "/flow/input-ports/${receiver_port_id}/status" "" 5 2 >"${tmpdir}/receiver-input-port-status.json"
  receiver_curl_retry GET "/flow/processors/${receiver_processor_id}/status" "" 5 2 >"${tmpdir}/receiver-processor-status.json"
  if delivery_ok="$(
    python3 - "${tmpdir}/receiver-input-port-status.json" "${tmpdir}/receiver-processor-status.json" "${receiver_input_port_name}" "${receiver_processor_name}" <<'PY'
import json
import re
import sys

port_payload = json.load(open(sys.argv[1]))
processor_payload = json.load(open(sys.argv[2]))
target_port = sys.argv[3]
target_processor = sys.argv[4]

def find_name(node, target):
    if isinstance(node, dict):
        if node.get("name") == target:
            return node
        for value in node.values():
            result = find_name(value, target)
            if result is not None:
                return result
    elif isinstance(node, list):
        for item in node:
            result = find_name(item, target)
            if result is not None:
                return result
    return None

def positive_metric(node):
    if isinstance(node, dict):
        for key, value in node.items():
            lowered = key.lower()
            if lowered in {"flowfilesin", "bytesin", "inputcount", "inputbytes"}:
                if isinstance(value, (int, float)) and value > 0:
                    return f"{key}={value}"
                if isinstance(value, str):
                    match = re.search(r"[1-9][0-9]*", value)
                    if match:
                        return f"{key}={value}"
            result = positive_metric(value)
            if result is not None:
                return result
    elif isinstance(node, list):
        for item in node:
            result = positive_metric(item)
            if result is not None:
                return result
    return None

port_node = find_name(port_payload, target_port)
processor_node = find_name(processor_payload, target_processor)
for node in (port_node, processor_node, port_payload, processor_payload):
    metric = positive_metric(node)
    if metric is not None:
        print(metric)
        raise SystemExit(0)
raise SystemExit(1)
PY
  )"; then
    break
  fi
  sleep 5
done

if [[ -z "${delivery_ok}" ]]; then
  echo "timed out waiting for receiver-side provenance delivery observation" >&2
  exit 1
fi

echo "PASS: typed Site-to-Site provenance delivery proof succeeded (${delivery_ok})"
