#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
client_name="github-flows-kind"
mock_deployment="github-mock"
controller_namespace="nifi-system"
controller_deployment="nifi-fabric-controller-manager"
workflow_bucket="team-a"
workflow_flow_name=""
workflow_pg_name=""

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
    --client-name)
      client_name="$2"
      shift 2
      ;;
    --workflow-bucket)
      workflow_bucket="$2"
      shift 2
      ;;
    --workflow-flow-name)
      workflow_flow_name="$2"
      shift 2
      ;;
    --workflow-process-group-name)
      workflow_pg_name="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

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

dump_diagnostics() {
  set +e
  echo
  echo "==> GitHub versioned-flow workflow diagnostics"
  kubectl config current-context || true
  kubectl -n "${namespace}" get nificluster "${release}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${namespace}" get pods -o wide || true
  kubectl -n "${namespace}" get events --sort-by=.lastTimestamp | tail -n 80 || true
  kubectl -n "${controller_namespace}" logs deployment/"${controller_deployment}" --tail=200 || true
  kubectl -n "${namespace}" logs deployment/"${mock_deployment}" --tail=200 || true
  for file in \
    created-process-group.json \
    start-version-control.json \
    created-processor.json \
    workflow-processor-running.json \
    process-group-flow.json \
    flow-registries.json \
    bucket-flows.json \
    mock-state.json \
    last-error-response.txt; do
    if [[ -f "${tmpdir}/${file}" ]]; then
      echo
      echo "${file}:"
      cat "${tmpdir}/${file}"
    fi
  done
}

on_error() {
  echo "FAIL: GitHub versioned-flow workflow proof failed" >&2
  dump_diagnostics
  exit 1
}
trap on_error ERR

bash "$(dirname "${BASH_SOURCE[0]}")/prove-github-flow-registry-client.sh" \
  --namespace "${namespace}" \
  --release "${release}" \
  --auth-secret "${auth_secret}" \
  --client-name "${client_name}" \
  --expect-bucket "${workflow_bucket}" \
  --expect-bucket team-b

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
  printf '%s' "${LAST_HTTP_BODY}" >"${tmpdir}/last-error-response.txt"

  if [[ ! "${status}" =~ ^2 ]]; then
    echo "NiFi API ${method} ${path} returned HTTP ${status}" >&2
    return 1
  fi

  printf '%s' "${LAST_HTTP_BODY}"
}

nifi_request_retry() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local attempts="${4:-20}"
  local sleep_seconds="${5:-2}"
  local attempt

  for attempt in $(seq 1 "${attempts}"); do
    if nifi_request "${method}" "${path}" "${body}"; then
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  return 1
}

nifi_request GET /flow/registries >"${tmpdir}/flow-registries.json"
registry_id="$(
  python3 - "${tmpdir}/flow-registries.json" "${client_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
for registry in payload.get("registries", []):
    component = registry.get("component", {})
    if component.get("name") == target:
        print(registry["id"])
        break
PY
)"

if [[ -z "${registry_id}" ]]; then
  echo "unable to resolve registry id for client ${client_name}" >&2
  exit 1
fi

nifi_request GET /flow/processor-types >"${tmpdir}/processor-types.json"
mapfile -t generate_flow_file_bundle < <(python3 - "${tmpdir}/processor-types.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
target = "org.apache.nifi.processors.standard.GenerateFlowFile"
for entry in payload.get("processorTypes", []):
    if entry.get("type") != target:
        continue
    bundle = entry.get("bundle") or {}
    print(bundle.get("group") or "")
    print(bundle.get("artifact") or "")
    print(bundle.get("version") or "")
    raise SystemExit(0)
raise SystemExit(1)
PY
)
generate_flow_file_bundle_group="${generate_flow_file_bundle[0]:-}"
generate_flow_file_bundle_artifact="${generate_flow_file_bundle[1]:-}"
generate_flow_file_bundle_version="${generate_flow_file_bundle[2]:-}"

if [[ -z "${generate_flow_file_bundle_group}" || -z "${generate_flow_file_bundle_artifact}" || -z "${generate_flow_file_bundle_version}" ]]; then
  echo "failed to resolve GenerateFlowFile processor bundle from live NiFi runtime" >&2
  exit 1
fi

workflow_suffix="$(date +%s)"
workflow_pg_name="${workflow_pg_name:-workflow-pg-${workflow_suffix}}"
workflow_flow_name="${workflow_flow_name:-workflow-flow-${workflow_suffix}}"

python3 - "${workflow_pg_name}" >"${tmpdir}/create-process-group.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "github-flow-workflow-proof",
        "version": 0,
    },
    "component": {
        "name": sys.argv[1],
        "position": {"x": 120.0, "y": 120.0},
    },
}
json.dump(payload, sys.stdout)
PY

create_pg_payload="$(tr -d '\n' < "${tmpdir}/create-process-group.json")"
nifi_request POST /process-groups/root/process-groups "${create_pg_payload}" >"${tmpdir}/created-process-group.json"

mapfile -t created_pg_meta < <(python3 - "${tmpdir}/created-process-group.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
pg_id = payload.get("id") or payload.get("component", {}).get("id")
revision = payload.get("revision", {})
print(pg_id or "")
print(revision.get("version", 0))
PY
)
workflow_pg_id="${created_pg_meta[0]:-}"
workflow_pg_version="${created_pg_meta[1]:-0}"

if [[ -z "${workflow_pg_id}" ]]; then
  echo "failed to create workflow process group" >&2
  exit 1
fi

python3 >"${tmpdir}/create-label.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "github-flow-workflow-proof",
        "version": 0,
    },
    "component": {
        "label": "payments import seed",
        "width": 260.0,
        "height": 40.0,
        "position": {"x": 180.0, "y": 180.0},
        "style": {
            "background-color": "#fff4bf",
        },
    },
}
json.dump(payload, sys.stdout)
PY

create_label_payload="$(tr -d '\n' < "${tmpdir}/create-label.json")"
nifi_request POST "/process-groups/${workflow_pg_id}/labels" "${create_label_payload}" >"${tmpdir}/created-label.json"

python3 \
  - "${workflow_flow_name}-source" \
  "${generate_flow_file_bundle_group}" \
  "${generate_flow_file_bundle_artifact}" \
  "${generate_flow_file_bundle_version}" \
  >"${tmpdir}/create-processor.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "github-flow-workflow-proof",
        "version": 0,
    },
    "component": {
        "name": sys.argv[1],
        "type": "org.apache.nifi.processors.standard.GenerateFlowFile",
        "bundle": {
            "group": sys.argv[2],
            "artifact": sys.argv[3],
            "version": sys.argv[4],
        },
        "position": {"x": 180.0, "y": 260.0},
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

create_processor_payload="$(tr -d '\n' < "${tmpdir}/create-processor.json")"
nifi_request POST "/process-groups/${workflow_pg_id}/processors" "${create_processor_payload}" >"${tmpdir}/created-processor.json"

mapfile -t created_processor_meta < <(python3 - "${tmpdir}/created-processor.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
component = payload.get("component", {})
revision = payload.get("revision", {})
print(payload.get("id") or component.get("id") or "")
print(revision.get("version", 0))
PY
)
workflow_processor_id="${created_processor_meta[0]:-}"
workflow_processor_version="${created_processor_meta[1]:-0}"

if [[ -z "${workflow_processor_id}" ]]; then
  echo "failed to create workflow seed processor" >&2
  exit 1
fi

python3 - "${workflow_processor_version}" >"${tmpdir}/start-processor.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "github-flow-workflow-proof",
        "version": int(sys.argv[1]),
    },
    "state": "RUNNING",
    "disconnectedNodeAcknowledged": False,
}
json.dump(payload, sys.stdout)
PY

nifi_request PUT "/processors/${workflow_processor_id}/run-status" "$(tr -d '\n' < "${tmpdir}/start-processor.json")" > /dev/null
nifi_request GET "/processors/${workflow_processor_id}" >"${tmpdir}/workflow-processor-running.json"

python3 - "${tmpdir}/workflow-processor-running.json" "${workflow_flow_name}-source" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target_name = sys.argv[2]
component = payload.get("component", {})
if component.get("name") != target_name:
    raise SystemExit(f"expected workflow seed processor {target_name!r}, got {component.get('name')!r}")
if component.get("state") != "RUNNING":
    raise SystemExit(f"expected workflow seed processor RUNNING, got {component.get('state')!r}")
print("ok")
PY

nifi_request GET "/process-groups/${workflow_pg_id}" >"${tmpdir}/workflow-process-group.json"

workflow_pg_version="$(
  python3 - "${tmpdir}/workflow-process-group.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
print(payload.get("revision", {}).get("version", 0))
PY
)"

python3 - "${workflow_pg_version}" "${registry_id}" "${workflow_bucket}" "${workflow_flow_name}" >"${tmpdir}/start-version-control-request.json" <<'PY'
import json
import sys

payload = {
    "processGroupRevision": {
        "clientId": "github-flow-workflow-proof",
        "version": int(sys.argv[1]),
    },
    "versionedFlow": {
        "action": "COMMIT",
        "registryId": sys.argv[2],
        "bucketId": sys.argv[3],
        "flowName": sys.argv[4],
        "description": "Focused GitHub Flow Registry workflow proof",
        "comments": "Chart-first workflow proof",
    },
}
json.dump(payload, sys.stdout)
PY

start_payload="$(tr -d '\n' < "${tmpdir}/start-version-control-request.json")"
nifi_request POST "/versions/process-groups/${workflow_pg_id}" "${start_payload}" >"${tmpdir}/start-version-control.json"

nifi_request_retry GET "/flow/process-groups/${workflow_pg_id}" "" 20 2 >"${tmpdir}/process-group-flow.json"
nifi_request_retry GET "/flow/registries/${registry_id}/buckets/${workflow_bucket}/flows" "" 20 2 >"${tmpdir}/bucket-flows.json"

mapfile -t flow_meta < <(python3 - "${tmpdir}/bucket-flows.json" "${workflow_flow_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target_name = sys.argv[2]

flows = []
if "bucketFlowResults" in payload:
    flows = payload["bucketFlowResults"]
elif "versionedFlows" in payload:
    flows = payload["versionedFlows"]
elif "flows" in payload:
    flows = payload["flows"]

for flow in flows:
    candidate = flow.get("flow") or flow.get("versionedFlow") or flow
    name = (
        candidate.get("name")
        or candidate.get("flowName")
        or candidate.get("identifier")
        or ""
    )
    flow_id = (
        candidate.get("identifier")
        or candidate.get("flowId")
        or candidate.get("id")
        or ""
    )
    if name == target_name:
        print(flow_id)
        print(name)
        break
PY
)
versioned_flow_id="${flow_meta[0]:-}"
versioned_flow_name="${flow_meta[1]:-}"

if [[ -z "${versioned_flow_id}" ]]; then
  echo "saved flow ${workflow_flow_name} was not returned from bucket ${workflow_bucket}" >&2
  exit 1
fi

kubectl -n "${namespace}" exec deployment/"${mock_deployment}" -c mock -- \
  python3 -c 'import json, urllib.request; print(urllib.request.urlopen("http://127.0.0.1:8080/debug/state").read().decode())' \
  >"${tmpdir}/mock-state.json"

python3 - "${tmpdir}/mock-state.json" "${workflow_bucket}" "${versioned_flow_id}" "${workflow_flow_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
bucket = sys.argv[2]
flow_id = sys.argv[3]
flow_name = sys.argv[4]
expected_path = f"flows/{bucket}/{flow_id}.json"

files = payload.get("files", {})
if expected_path not in files:
    raise SystemExit(f"expected mock repository to contain {expected_path}, got {sorted(files)}")

content = files[expected_path]["content"]
if flow_name not in content:
    raise SystemExit(f"expected {expected_path} to contain flow name {flow_name!r}")
if "payments import seed" not in content:
    raise SystemExit(f"expected {expected_path} to contain the seeded label content")

print(json.dumps({"storedPath": expected_path}, indent=2, sort_keys=True))
PY

python3 - "${tmpdir}/process-group-flow.json" "${versioned_flow_id}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
process_group_flow = payload.get("processGroupFlow", {})
flow = process_group_flow.get("flow", {})
breadcrumb = process_group_flow.get("breadcrumb", {}).get("breadcrumb", {})
vc = flow.get("versionControlInformation", {}) or breadcrumb.get("versionControlInformation", {})
flow_id = vc.get("flowId") or vc.get("versionedFlow", {}).get("flowId")
if flow_id != sys.argv[2]:
    raise SystemExit(f"expected process group version control info to reference flow {sys.argv[2]!r}, got {flow_id!r}")
print(json.dumps({"processGroupFlowId": flow_id}, indent=2, sort_keys=True))
PY

echo
echo "SUCCESS: GitHub versioned-flow workflow proof completed"
echo "  workflow process group: ${workflow_pg_name} (${workflow_pg_id})"
echo "  workflow flow name: ${versioned_flow_name}"
echo "  workflow flow id: ${versioned_flow_id}"
echo "  registry id: ${registry_id}"
