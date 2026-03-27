#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
audit_archive_dir="/opt/nifi/nifi-current/database_repository/flow-audit-archive"

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
    --audit-archive-dir)
      audit_archive_dir="$2"
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

pod="${release}-0"
host="${pod}.${release}-headless.${namespace}.svc.cluster.local"
base_url="https://${host}:8443/nifi-api"
username="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.username}' | base64 -d)"
password="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.password}' | base64 -d)"
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

proof_group_name="flow-action-audit-proof-$(date +%s)"

python3 - "${proof_group_name}" >"${tmpdir}/create-process-group.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "flow-action-audit-proof",
        "version": 0,
    },
    "component": {
        "name": sys.argv[1],
        "position": {"x": 320.0, "y": 320.0},
    },
}
json.dump(payload, sys.stdout)
PY

nifi_request POST /process-groups/root/process-groups "$(tr -d '\n' < "${tmpdir}/create-process-group.json")" >"${tmpdir}/created-process-group.json"

process_group_id="$(
  python3 - "${tmpdir}/created-process-group.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload.get("id") or payload.get("component", {}).get("id") or "")
PY
)"

if [[ -z "${process_group_id}" ]]; then
  echo "failed to discover created process group id from NiFi response" >&2
  cat "${tmpdir}/created-process-group.json" >&2
  exit 1
fi

deadline=$(( $(date +%s) + 120 ))
while true; do
  kubectl -n "${namespace}" logs "${pod}" -c nifi --since=10m >"${tmpdir}/nifi.log"
if python3 - "${tmpdir}/nifi.log" "${process_group_id}" "${username}" "${proof_group_name}" >"${tmpdir}/matched-event.json" <<'PY'
import json
import sys

log_path, target_id, expected_user, expected_name = sys.argv[1:5]

with open(log_path, encoding="utf-8") as handle:
    for raw_line in handle:
        marker = raw_line.find('{"schemaVersion":"v1"')
        if marker < 0:
            marker = raw_line.find('{"eventType":"nifi.flowAction"')
        if marker < 0:
            continue

        candidate = raw_line[marker:].strip()
        end = candidate.rfind("}")
        if end < 0:
            continue
        candidate = candidate[: end + 1]

        try:
            event = json.loads(candidate)
        except json.JSONDecodeError:
            continue

        if event.get("eventType") != "nifi.flowAction":
            continue

        component = event.get("component", {})
        attributes = event.get("attributes", {})
        component_id = component.get("id") or ""
        component_payload = json.dumps(component, sort_keys=True)
        attributes_payload = json.dumps(attributes, sort_keys=True)
        if target_id not in {component_id} and target_id not in component_payload and target_id not in attributes_payload:
            continue

        if event.get("user", {}).get("identity") != expected_user:
            continue

        if event.get("action", {}).get("operation") != "Add":
            raise SystemExit("matched audit event missing action.operation")

        if event.get("component", {}).get("type") != "ProcessGroup":
            raise SystemExit("matched audit event missing component.type")

        component_name = event.get("component", {}).get("name")
        if component_name and component_name != expected_name:
            raise SystemExit("matched audit event has unexpected component.name")

        print(json.dumps(event))
        raise SystemExit(0)

raise SystemExit(1)
PY
  then
    break
  fi

  if (( $(date +%s) >= deadline )); then
    echo "timed out waiting for flow-action audit event for process group ${process_group_id}" >&2
    tail -n 120 "${tmpdir}/nifi.log" >&2 || true
    exit 1
  fi

  sleep 5
done

archive_file="$(
  kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- sh -ec \
    "find '${audit_archive_dir}' -type f | head -n 1" 2>/dev/null || true
)"

if [[ -z "${archive_file}" ]]; then
  echo "expected at least one file under ${audit_archive_dir}" >&2
  exit 1
fi

echo "matched flow-action audit event:"
cat "${tmpdir}/matched-event.json"
echo
echo "archive file: ${archive_file}"
