#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
configmap=""
client_name="github-flows-kind"
controller_namespace="nifi-system"
controller_deployment="nifi-fabric-controller-manager"
mock_deployment="github-mock"
expected_buckets=()

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
    --configmap)
      configmap="$2"
      shift 2
      ;;
    --client-name)
      client_name="$2"
      shift 2
      ;;
    --expect-bucket)
      expected_buckets+=("$2")
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

configmap="${configmap:-${release}-flow-registry-clients}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

for cmd in kubectl curl jq python3; do
  require_command "${cmd}"
done

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

dump_diagnostics() {
  set +e
  echo
  echo "==> GitHub Flow Registry diagnostics"
  kubectl config current-context || true
  kubectl -n "${namespace}" get nificluster "${release}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${namespace}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,UID:.metadata.uid,REV:.metadata.labels.controller-revision-hash || true
  kubectl -n "${namespace}" get configmap "${configmap}" -o jsonpath='{.data.clients\.json}' || true
  printf '\n' || true
  kubectl -n "${namespace}" get events --sort-by=.lastTimestamp | tail -n 50 || true
  kubectl -n "${controller_namespace}" logs deployment/"${controller_deployment}" --tail=200 || true
  kubectl -n "${namespace}" logs deployment/"${mock_deployment}" --tail=200 || true
  if [[ -f "${tmpdir}/flow-registries.json" ]]; then
    echo
    echo "flow registries:"
    cat "${tmpdir}/flow-registries.json"
  fi
  if [[ -f "${tmpdir}/buckets-response.txt" ]]; then
    echo
    echo "buckets response:"
    cat "${tmpdir}/buckets-response.txt"
  fi
}

on_error() {
  echo "FAIL: GitHub Flow Registry runtime proof failed" >&2
  dump_diagnostics
  exit 1
}
trap on_error ERR

host="nifi-0.${release}-headless.${namespace}.svc.cluster.local"
base_url="https://${host}:8443/nifi-api"

kubectl -n "${namespace}" get configmap "${configmap}" -o jsonpath='{.data.clients\.json}' >"${tmpdir}/clients.json"
if [[ ! -s "${tmpdir}/clients.json" ]]; then
  echo "flow registry client catalog ${configmap} is missing data.clients.json" >&2
  exit 1
fi

python3 - "${tmpdir}/clients.json" "${client_name}" >"${tmpdir}/client.json" <<'PY'
import json
import sys

catalog_path, client_name = sys.argv[1], sys.argv[2]
catalog = json.load(open(catalog_path))
for client in catalog["clients"]:
    if client["name"] == client_name:
        json.dump(client, sys.stdout)
        sys.exit(0)

raise SystemExit(f"client {client_name!r} not found in prepared catalog")
PY

provider="$(python3 - "${tmpdir}/client.json" <<'PY'
import json
import sys

client = json.load(open(sys.argv[1]))
print(client["provider"])
PY
)"

if [[ "${provider}" != "github" ]]; then
  echo "client ${client_name} is provider=${provider}, expected github" >&2
  exit 1
fi

token_secret_name="$(python3 - "${tmpdir}/client.json" <<'PY'
import json
import sys

client = json.load(open(sys.argv[1]))
ref = client.get("sensitivePropertyRefs", {}).get("Personal Access Token")
if not ref:
    raise SystemExit("prepared GitHub client is missing Personal Access Token secret reference")
print(ref["secretName"])
print(ref["secretKey"])
PY
)"
token_secret="$(printf '%s\n' "${token_secret_name}" | sed -n '1p')"
token_key="$(printf '%s\n' "${token_secret_name}" | sed -n '2p')"

if [[ -z "${token_secret}" || -z "${token_key}" ]]; then
  echo "prepared GitHub client is missing Personal Access Token secret reference" >&2
  exit 1
fi

github_token="$(kubectl -n "${namespace}" get secret "${token_secret}" -o "jsonpath={.data.${token_key}}" | base64 -d)"
if [[ -z "${github_token}" ]]; then
  echo "GitHub token Secret ${token_secret}/${token_key} is empty" >&2
  exit 1
fi

username="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.username}' | base64 -d)"
password="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.password}' | base64 -d)"
pod="${release}-0"

nifi_curl() {
  local method="$1"
  local path="$2"
  local body="${3:-}"

  kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- env \
    NIFI_USERNAME="${username}" \
    NIFI_PASSWORD="${password}" \
    NIFI_TOKEN="${nifi_token:-}" \
    NIFI_BASE_URL="${base_url}" \
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

nifi_token=""
for _ in $(seq 1 20); do
  if nifi_token="$(nifi_curl GET "")"; then
    break
  fi
  sleep 2
done

if [[ -z "${nifi_token}" ]]; then
  echo "timed out waiting for NiFi API token minting through direct pod HTTPS" >&2
  exit 1
fi

nifi_curl GET /controller/registry-types >"${tmpdir}/registry-types.json"
python3 - "${tmpdir}/registry-types.json" "${tmpdir}/client.json" >"${tmpdir}/bundle.json" <<'PY'
import json
import sys

types_path, client_path = sys.argv[1], sys.argv[2]
types = json.load(open(types_path))
client = json.load(open(client_path))
implementation_class = client["implementationClass"]
for flow_type in types["flowRegistryClientTypes"]:
    if flow_type["type"] == implementation_class:
        json.dump(flow_type["bundle"], sys.stdout)
        sys.exit(0)

raise SystemExit(f"NiFi runtime does not expose registry type {implementation_class}")
PY

nifi_curl GET /flow/registries >"${tmpdir}/flow-registries.json"
existing_id="$(
  python3 - "${tmpdir}/flow-registries.json" "${client_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
for registry in payload["registries"]:
    if registry["component"]["name"] == target:
        print(registry["id"])
        break
PY
)"

if [[ -z "${existing_id}" ]]; then
  python3 - "${tmpdir}/client.json" "${tmpdir}/bundle.json" "${github_token}" >"${tmpdir}/create-registry-client.json" <<'PY'
import json
import sys

client_path, bundle_path, token = sys.argv[1], sys.argv[2], sys.argv[3]
client = json.load(open(client_path))
bundle = json.load(open(bundle_path))
properties = dict(client["properties"])
properties["Personal Access Token"] = token

payload = {
    "revision": {"version": 0},
    "component": {
        "name": client["name"],
        "type": client["implementationClass"],
        "bundle": bundle,
        "properties": properties,
    },
}
json.dump(payload, sys.stdout)
PY
  create_payload="$(tr -d '\n' < "${tmpdir}/create-registry-client.json")"
  nifi_curl POST /controller/registry-clients "${create_payload}" >"${tmpdir}/created-registry-client.json"
  registry_id="$(python3 - "${tmpdir}/created-registry-client.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1]))["id"])
PY
)"
else
  registry_id="${existing_id}"
fi

nifi_curl GET "/flow/registries/${registry_id}/buckets" >"${tmpdir}/buckets-response.txt"

python3 - "${tmpdir}/buckets-response.txt" "${expected_buckets[@]}" <<'PY'
import json
import sys

payload_path = sys.argv[1]
expected = sys.argv[2:]
content = open(payload_path).read().strip()
try:
    payload = json.loads(content)
except json.JSONDecodeError:
    raise SystemExit(f"NiFi bucket response is not valid JSON: {content}")

if "bucketResults" in payload:
    buckets = [bucket["name"] for bucket in payload["bucketResults"]]
elif "buckets" in payload:
    buckets = []
    for bucket in payload["buckets"]:
        if "name" in bucket:
            buckets.append(bucket["name"])
        elif isinstance(bucket.get("bucket"), dict) and "name" in bucket["bucket"]:
            buckets.append(bucket["bucket"]["name"])
        elif "id" in bucket:
            buckets.append(bucket["id"])
        else:
            raise SystemExit(f"NiFi bucket entry does not contain a usable name: {bucket}")
else:
    raise SystemExit(f"NiFi bucket response does not contain buckets: {payload}")

missing = [bucket for bucket in expected if bucket not in buckets]
if missing:
    raise SystemExit(f"expected buckets {missing} were not returned; got {buckets}")

print(json.dumps({"buckets": buckets}, indent=2, sort_keys=True))
PY
