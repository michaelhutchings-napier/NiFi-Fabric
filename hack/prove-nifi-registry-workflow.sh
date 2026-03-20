#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
client_name="nifi-registry-flows"
registry_url=""
registry_base_url=""
registry_deployment="nifi-registry"
controller_namespace="nifi-system"
controller_deployment=""
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
    --registry-url)
      registry_url="$2"
      shift 2
      ;;
    --registry-base-url)
      registry_base_url="$2"
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

registry_base_url="${registry_base_url:-http://nifi-registry.${namespace}.svc.cluster.local:18080/nifi-registry-api}"
controller_deployment="${controller_deployment:-${release}-controller-manager}"

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
  echo "==> NiFi Registry versioned-flow workflow diagnostics"
  kubectl config current-context || true
  kubectl -n "${namespace}" get nificluster "${release}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${namespace}" get pods -o wide || true
  kubectl -n "${namespace}" get events --sort-by=.lastTimestamp | tail -n 80 || true
  kubectl -n "${controller_namespace}" logs deployment/"${controller_deployment}" --tail=200 || true
  kubectl -n "${namespace}" logs deployment/"${registry_deployment}" --tail=200 || true
  for file in \
    created-process-group.json \
    start-version-control.json \
    process-group-flow.json \
    flow-registries.json \
    bucket-flows.json \
    latest-version.json \
    buckets.json \
    last-error-response.txt; do
    if [[ -f "${tmpdir}/${file}" ]]; then
      echo
      echo "${file}:"
      cat "${tmpdir}/${file}"
    fi
  done
}

on_error() {
  echo "FAIL: NiFi Registry versioned-flow workflow proof failed" >&2
  dump_diagnostics
  exit 1
}
trap on_error ERR

flow_registry_client_args=(
  --namespace "${namespace}"
  --release "${release}"
  --auth-secret "${auth_secret}"
  --client-name "${client_name}"
)
if [[ -n "${registry_url}" ]]; then
  flow_registry_client_args+=(--registry-url "${registry_url}")
fi

bash "$(dirname "${BASH_SOURCE[0]}")/prove-nifi-registry-flow-registry-client.sh" \
  "${flow_registry_client_args[@]}"

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

registry_request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"

  kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- env \
    REGISTRY_BASE_URL="${registry_base_url}" \
    REQUEST_METHOD="${method}" \
    REQUEST_PATH="${path}" \
    REQUEST_BODY="${body}" \
    sh -ec '
      if [ -n "${REQUEST_BODY}" ]; then
        curl --silent --show-error --fail \
          -H "Content-Type: application/json" \
          -X "${REQUEST_METHOD}" \
          --data "${REQUEST_BODY}" \
          "${REGISTRY_BASE_URL}${REQUEST_PATH}"
      else
        curl --silent --show-error --fail \
          -X "${REQUEST_METHOD}" \
          "${REGISTRY_BASE_URL}${REQUEST_PATH}"
      fi
    '
}

resolve_bucket_flow() {
  local bucket_id="$1"
  local flow_name="$2"

  registry_request GET "/buckets/${bucket_id}/flows" >"${tmpdir}/bucket-flows.json"
  python3 - "${tmpdir}/bucket-flows.json" "${flow_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target_name = sys.argv[2]

if isinstance(payload, list):
    flows = payload
else:
    flows = payload.get("bucketFlowResults") or payload.get("versionedFlows") or payload.get("flows") or []
for flow in flows:
    candidate = flow.get("flow") or flow.get("versionedFlow") or flow
    name = candidate.get("name") or candidate.get("flowName") or candidate.get("identifier") or ""
    flow_id = candidate.get("identifier") or candidate.get("flowId") or candidate.get("id") or ""
    if name == target_name:
        print(flow_id)
        print(name)
        break
PY
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

registry_request GET /buckets >"${tmpdir}/buckets.json"
bucket_id="$(
  python3 - "${tmpdir}/buckets.json" "${workflow_bucket}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
for bucket in payload:
    if bucket.get("name") == target:
        print(bucket.get("identifier") or "")
        break
PY
)"

if [[ -z "${bucket_id}" ]]; then
  python3 - "${workflow_bucket}" >"${tmpdir}/create-bucket.json" <<'PY'
import json
import sys

json.dump({"name": sys.argv[1], "description": "Bounded NiFi Registry compatibility proof bucket"}, sys.stdout)
PY
  create_bucket_payload="$(tr -d '\n' < "${tmpdir}/create-bucket.json")"
  registry_request POST /buckets "${create_bucket_payload}" >"${tmpdir}/created-bucket.json"
  bucket_id="$(
    python3 - "${tmpdir}/created-bucket.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
print(payload.get("identifier") or "")
PY
  )"
fi

if [[ -z "${bucket_id}" ]]; then
  echo "failed to resolve or create bucket ${workflow_bucket}" >&2
  exit 1
fi

workflow_suffix="$(date +%s)"
workflow_pg_name="${workflow_pg_name:-workflow-pg-${workflow_suffix}}"
workflow_flow_name="${workflow_flow_name:-workflow-flow-${workflow_suffix}}"

mapfile -t existing_flow_meta < <(resolve_bucket_flow "${bucket_id}" "${workflow_flow_name}")
existing_flow_id="${existing_flow_meta[0]:-}"
existing_flow_name="${existing_flow_meta[1]:-}"

if [[ -n "${existing_flow_id}" ]]; then
  registry_request GET "/buckets/${bucket_id}/flows/${existing_flow_id}/versions/latest" >"${tmpdir}/latest-version-before-update.json"

  python3 - "${tmpdir}/latest-version-before-update.json" "${workflow_pg_name}" >"${tmpdir}/import-next-version.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
proof_name = sys.argv[2]

metadata = payload.setdefault("snapshotMetadata", {})
comments = metadata.get("comments") or ""
update_note = f"Bounded NiFi Registry compatibility workflow proof update via {proof_name}"
metadata["comments"] = update_note if not comments else f"{comments} | {update_note}"
metadata.pop("timestamp", None)
metadata.pop("link", None)

bucket = payload.get("bucket")
if isinstance(bucket, dict):
    bucket.pop("revision", None)
    bucket.pop("permissions", None)
    bucket.pop("link", None)

json.dump(payload, sys.stdout)
PY

  import_payload="$(tr -d '\n' < "${tmpdir}/import-next-version.json")"
  registry_request POST "/buckets/${bucket_id}/flows/${existing_flow_id}/versions/import" "${import_payload}" >"${tmpdir}/latest-version.json"

  python3 - "${tmpdir}/latest-version-before-update.json" "${tmpdir}/latest-version.json" "${workflow_flow_name}" <<'PY'
import json
import sys

before = json.load(open(sys.argv[1]))
after = json.load(open(sys.argv[2]))
expected_name = sys.argv[3]

before_meta = before.get("snapshotMetadata") or {}
after_meta = after.get("snapshotMetadata") or {}

before_version = before_meta.get("version")
after_version = after_meta.get("version")
if before_version is None or after_version is None:
    raise SystemExit("expected NiFi Registry latest version metadata before and after import")
if int(after_version) <= int(before_version):
    raise SystemExit(
        f"expected imported flow version to advance beyond {before_version!r}, got {after_version!r}"
    )

flow = after.get("flow") or {}
flow_name = after_meta.get("flowName") or flow.get("name") or ""
if flow_name != expected_name:
    raise SystemExit(
        f"expected latest flow version to carry flow name {expected_name!r}, got {flow_name!r}"
    )

print(json.dumps({"latestVersion": after_version}, indent=2, sort_keys=True))
PY

  echo
  echo "SUCCESS: NiFi Registry versioned-flow workflow proof completed"
  echo "  workflow process group: reused existing flow without creating a new process group"
  echo "  workflow flow name: ${existing_flow_name}"
  echo "  workflow flow id: ${existing_flow_id}"
  echo "  registry id: ${registry_id}"
  echo "  bucket id: ${bucket_id}"
  exit 0
fi

python3 - "${workflow_pg_name}" >"${tmpdir}/create-process-group.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "nifi-registry-flow-workflow-proof",
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
        "clientId": "nifi-registry-flow-workflow-proof",
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
nifi_request GET "/process-groups/${workflow_pg_id}" >"${tmpdir}/workflow-process-group.json"

workflow_pg_version="$(
  python3 - "${tmpdir}/workflow-process-group.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
print(payload.get("revision", {}).get("version", 0))
PY
)"

python3 - "${workflow_pg_version}" "${registry_id}" "${bucket_id}" "${workflow_flow_name}" >"${tmpdir}/start-version-control-request.json" <<'PY'
import json
import sys

payload = {
    "processGroupRevision": {
        "clientId": "nifi-registry-flow-workflow-proof",
        "version": int(sys.argv[1]),
    },
    "versionedFlow": {
        "action": "COMMIT",
        "registryId": sys.argv[2],
        "bucketId": sys.argv[3],
        "flowName": sys.argv[4],
        "description": "Focused NiFi Registry compatibility workflow proof",
        "comments": "Chart-first workflow proof",
    },
}
json.dump(payload, sys.stdout)
PY

start_payload="$(tr -d '\n' < "${tmpdir}/start-version-control-request.json")"
nifi_request POST "/versions/process-groups/${workflow_pg_id}" "${start_payload}" >"${tmpdir}/start-version-control.json"

nifi_request_retry GET "/flow/process-groups/${workflow_pg_id}" "" 20 2 >"${tmpdir}/process-group-flow.json"
nifi_request_retry GET "/flow/registries/${registry_id}/buckets/${bucket_id}/flows" "" 20 2 >"${tmpdir}/bucket-flows.json"

mapfile -t flow_meta < <(python3 - "${tmpdir}/bucket-flows.json" "${workflow_flow_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target_name = sys.argv[2]

if isinstance(payload, list):
    flows = payload
else:
    flows = payload.get("bucketFlowResults") or payload.get("versionedFlows") or payload.get("flows") or []
for flow in flows:
    candidate = flow.get("flow") or flow.get("versionedFlow") or flow
    name = candidate.get("name") or candidate.get("flowName") or candidate.get("identifier") or ""
    flow_id = candidate.get("identifier") or candidate.get("flowId") or candidate.get("id") or ""
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

registry_request GET "/buckets/${bucket_id}/flows/${versioned_flow_id}/versions/latest" >"${tmpdir}/latest-version.json"

python3 - "${tmpdir}/latest-version.json" "${workflow_flow_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
metadata = payload.get("snapshotMetadata") or {}
flow = payload.get("flow") or {}
flow_name = metadata.get("flowName") or flow.get("name") or ""
if flow_name != sys.argv[2]:
    raise SystemExit(f"expected latest flow version to carry flow name {sys.argv[2]!r}, got {flow_name!r}")

labels = (payload.get("flowContents") or {}).get("labels") or []
if not any(label.get("label") == "payments import seed" for label in labels):
    raise SystemExit("expected latest flow snapshot to contain the seeded label")

print(json.dumps({"latestVersion": metadata.get("version")}, indent=2, sort_keys=True))
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

nifi_request_retry GET "/process-groups/${workflow_pg_id}" "" 20 2 >"${tmpdir}/workflow-process-group-after-commit.json"
workflow_pg_delete_version="$(
  python3 - "${tmpdir}/workflow-process-group-after-commit.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
print(payload.get("revision", {}).get("version", 0))
PY
)"
nifi_request DELETE "/process-groups/${workflow_pg_id}?version=${workflow_pg_delete_version}&disconnectedNodeAcknowledged=false" >/dev/null

echo
echo "SUCCESS: NiFi Registry versioned-flow workflow proof completed"
echo "  workflow process group: ${workflow_pg_name} (${workflow_pg_id}, deleted after commit)"
echo "  workflow flow name: ${versioned_flow_name}"
echo "  workflow flow id: ${versioned_flow_id}"
echo "  registry id: ${registry_id}"
echo "  bucket id: ${bucket_id}"
