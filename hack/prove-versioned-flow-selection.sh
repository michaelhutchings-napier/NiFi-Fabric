#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
imports_configmap=""
import_name=""

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
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

imports_configmap="${imports_configmap:-${release}-versioned-flow-imports}"

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

kubectl -n "${namespace}" get configmap "${imports_configmap}" -o jsonpath='{.data.imports\.json}' >"${tmpdir}/imports.json"
if [[ ! -s "${tmpdir}/imports.json" ]]; then
  echo "versioned flow import catalog ${imports_configmap} is missing data.imports.json" >&2
  exit 1
fi

python3 - "${tmpdir}/imports.json" "${import_name}" >"${tmpdir}/selected-import.json" <<'PY'
import json
import sys

catalog = json.load(open(sys.argv[1]))
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

selected = json.load(open(sys.argv[1]))
print(selected["name"])
print(selected["registryClientRef"]["name"])
print(selected["source"]["bucket"])
print(selected["source"]["flowName"])
print(selected["source"]["version"])
print(selected["target"]["rootProcessGroupName"])
print(",".join(ref["name"] for ref in selected.get("parameterContextRefs", [])))
PY
)

selected_import_name="${selected_meta[0]:-}"
registry_client_name="${selected_meta[1]:-}"
bucket_name="${selected_meta[2]:-}"
flow_name="${selected_meta[3]:-}"
selected_version="${selected_meta[4]:-}"
target_root_pg_name="${selected_meta[5]:-}"
parameter_context_refs="${selected_meta[6]:-}"
seed_workflow_pg_name="selection-seed-${selected_import_name}-$(date +%s)"

echo "==> Creating the GitHub Flow Registry client for bounded selection proof"
bash "$(dirname "${BASH_SOURCE[0]}")/prove-github-flow-registry-client.sh" \
  --namespace "${namespace}" \
  --release "${release}" \
  --auth-secret "${auth_secret}" \
  --client-name "${registry_client_name}" \
  --expect-bucket "${bucket_name}" \
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

  if [[ ! "${status}" =~ ^2 ]]; then
    echo "NiFi API ${method} ${path} returned HTTP ${status}" >&2
    printf '%s\n' "${LAST_HTTP_BODY}" >&2
    return 1
  fi

  printf '%s' "${LAST_HTTP_BODY}"
}

nifi_request_with_status() {
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
}

resolve_flow_id() {
  python3 - "$1" "$2" <<'PY'
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
    name = candidate.get("name") or candidate.get("flowName") or candidate.get("identifier") or ""
    flow_id = candidate.get("identifier") or candidate.get("flowId") or candidate.get("id") or ""
    if name == target_name:
        print(flow_id)
        break
PY
}

nifi_request GET /flow/registries >"${tmpdir}/flow-registries.json"
registry_id="$(
  python3 - "${tmpdir}/flow-registries.json" "${registry_client_name}" <<'PY'
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
  echo "unable to resolve registry id for client ${registry_client_name}" >&2
  exit 1
fi

nifi_request_with_status GET "/flow/registries/${registry_id}/buckets/${bucket_name}/flows"
if [[ "${LAST_HTTP_STATUS}" =~ ^2 ]]; then
  printf '%s' "${LAST_HTTP_BODY}" >"${tmpdir}/bucket-flows.json"
elif [[ "${LAST_HTTP_STATUS}" == "404" || "${LAST_HTTP_STATUS}" == "409" ]]; then
  # A fresh registry path may exist at the bucket level before any flow content has been seeded.
  printf '{"bucketFlowResults":[]}\n' >"${tmpdir}/bucket-flows.json"
else
  echo "NiFi API GET /flow/registries/${registry_id}/buckets/${bucket_name}/flows returned HTTP ${LAST_HTTP_STATUS}" >&2
  printf '%s\n' "${LAST_HTTP_BODY}" >&2
  exit 1
fi
flow_id="$(resolve_flow_id "${tmpdir}/bucket-flows.json" "${flow_name}")"

if [[ -z "${flow_id}" ]]; then
  echo "==> Seeding a live versioned flow for bounded selection proof"
  bash "$(dirname "${BASH_SOURCE[0]}")/prove-github-flow-registry-workflow.sh" \
    --namespace "${namespace}" \
    --release "${release}" \
    --auth-secret "${auth_secret}" \
    --client-name "${registry_client_name}" \
    --workflow-bucket "${bucket_name}" \
    --workflow-flow-name "${flow_name}" \
    --workflow-process-group-name "${seed_workflow_pg_name}"
  nifi_request GET "/flow/registries/${registry_id}/buckets/${bucket_name}/flows" >"${tmpdir}/bucket-flows.json"
  flow_id="$(resolve_flow_id "${tmpdir}/bucket-flows.json" "${flow_name}")"
fi

if [[ -z "${flow_id}" ]]; then
  echo "unable to resolve flow id for flow ${flow_name} in bucket ${bucket_name}" >&2
  exit 1
fi

nifi_request GET "/flow/registries/${registry_id}/buckets/${bucket_name}/flows/${flow_id}/versions" >"${tmpdir}/flow-versions.json"

mapfile -t resolved_version_meta < <(python3 - "${tmpdir}/flow-versions.json" "${selected_version}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
selected_version = sys.argv[2]
entries = []

metadata_sets = []
if isinstance(payload, dict):
    if isinstance(payload.get("versionedFlowSnapshotMetadataSet"), list):
        metadata_sets.append({"items": payload["versionedFlowSnapshotMetadataSet"]})
    if isinstance(payload.get("versionedFlowSnapshotMetadataSet"), dict):
        metadata_sets.append(payload["versionedFlowSnapshotMetadataSet"])
    if isinstance(payload.get("versionedFlowSnapshotMetadata"), list):
        metadata_sets.append({"items": payload["versionedFlowSnapshotMetadata"]})
    if isinstance(payload.get("versionedFlowSnapshots"), list):
        metadata_sets.append({"items": payload["versionedFlowSnapshots"]})
    if isinstance(payload.get("versionedFlowSnapshot"), dict):
        metadata_sets.append({"items": [payload["versionedFlowSnapshot"]]})

for metadata_set in metadata_sets:
    items = metadata_set.get("versionedFlowSnapshotMetadata") or metadata_set.get("items") or []
    for index, item in enumerate(items):
        candidate = item.get("versionedFlowSnapshotMetadata") or item.get("snapshotMetadata") or item
        version = candidate.get("version")
        if version is None:
            continue
        timestamp = candidate.get("timestamp")
        if not isinstance(timestamp, int):
            timestamp = -1
        entries.append(
            {
                "version": str(version),
                "timestamp": timestamp,
                "index": index,
            }
        )

unique_entries = {}
for entry in entries:
    existing = unique_entries.get(entry["version"])
    if existing is None:
        unique_entries[entry["version"]] = entry
        continue
    if (entry["timestamp"], entry["index"]) > (existing["timestamp"], existing["index"]):
        unique_entries[entry["version"]] = entry

ordered = sorted(unique_entries.values(), key=lambda entry: (entry["timestamp"], entry["index"], entry["version"]))
available = ",".join(entry["version"] for entry in ordered)

resolved = ""
if ordered:
    if selected_version == "latest":
        resolved = ordered[-1]["version"]
    else:
        for entry in ordered:
            if entry["version"] == selected_version:
                resolved = entry["version"]
                break

print(resolved)
print(available)
PY
)

resolved_version="${resolved_version_meta[0]:-}"
available_versions_csv="${resolved_version_meta[1]:-}"

if [[ -z "${available_versions_csv}" ]]; then
  echo "selected flow ${flow_name} did not return any discoverable versions from NiFi" >&2
  cat "${tmpdir}/flow-versions.json" >&2
  exit 1
fi

if [[ -z "${resolved_version}" ]]; then
  echo "selected version ${selected_version} was not returned for flow ${flow_name}; available versions: ${available_versions_csv}" >&2
  exit 1
fi

echo
echo "SUCCESS: bounded versioned-flow selection proof completed"
echo "  import name: ${selected_import_name}"
echo "  registry client: ${registry_client_name}"
echo "  bucket: ${bucket_name}"
echo "  flow name: ${flow_name}"
echo "  flow id: ${flow_id}"
echo "  selected version: ${selected_version}"
echo "  resolved version: ${resolved_version}"
echo "  intended root child process group: ${target_root_pg_name}"
echo "  parameter context refs: ${parameter_context_refs:-none}"
