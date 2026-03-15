#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
task_name="fabric-site-to-site-provenance-export"
ssl_service_name="fabric-site-to-site-provenance-ssl"
expected_destination_url=""
expected_input_port=""
expected_transport="HTTP"
expected_platform="nifi"
expected_auth_type=""
expected_authorized_identity=""
expected_auth_secret_ref_name=""
expected_start_position="endOfStream"
expect_ssl_service="true"

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
    --task-name)
      task_name="$2"
      shift 2
      ;;
    --ssl-service-name)
      ssl_service_name="$2"
      shift 2
      ;;
    --expected-destination-url)
      expected_destination_url="$2"
      shift 2
      ;;
    --expected-input-port)
      expected_input_port="$2"
      shift 2
      ;;
    --expected-transport)
      expected_transport="$2"
      shift 2
      ;;
    --expected-platform)
      expected_platform="$2"
      shift 2
      ;;
    --expected-auth-type)
      expected_auth_type="$2"
      shift 2
      ;;
    --expected-authorized-identity)
      expected_authorized_identity="$2"
      shift 2
      ;;
    --expected-auth-secret-ref-name)
      expected_auth_secret_ref_name="$2"
      shift 2
      ;;
    --expected-start-position)
      expected_start_position="$2"
      shift 2
      ;;
    --expect-ssl-service)
      expect_ssl_service="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

for cmd in kubectl curl python3; do
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
  echo "==> Site-to-Site provenance diagnostics"
  kubectl -n "${namespace}" get nificluster "${release}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${namespace}" get configmap "${release}-site-to-site-provenance" -o yaml || true
  kubectl -n "${namespace}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount || true
  kubectl -n "${namespace}" logs "${release}-0" -c nifi --tail=200 || true
  if [[ -f "${tmpdir}/reporting-task.json" ]]; then
    echo
    echo "reporting task entity:"
    cat "${tmpdir}/reporting-task.json"
  fi
  if [[ -f "${tmpdir}/controller-service.json" ]]; then
    echo
    echo "controller service entity:"
    cat "${tmpdir}/controller-service.json"
  fi
  if [[ -f "${tmpdir}/site-to-site-config.json" ]]; then
    echo
    echo "site-to-site provenance config contract:"
    cat "${tmpdir}/site-to-site-config.json"
  fi
}

on_error() {
  echo "FAIL: typed Site-to-Site provenance runtime proof failed" >&2
  dump_diagnostics
  exit 1
}
trap on_error ERR

host="${release}-0.${release}-headless.${namespace}.svc.cluster.local"
base_url="https://${host}:8443/nifi-api"
pod="${release}-0"

username="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.username}' | base64 -d)"
password="$(kubectl -n "${namespace}" get secret "${auth_secret}" -o jsonpath='{.data.password}' | base64 -d)"

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

nifi_curl_retry() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local attempts="${4:-30}"
  local sleep_seconds="${5:-2}"
  local attempt
  local output

  for attempt in $(seq 1 "${attempts}"); do
    if output="$(nifi_curl "${method}" "${path}" "${body}")"; then
      printf '%s' "${output}"
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  return 1
}

nifi_token=""
for _ in $(seq 1 40); do
  if nifi_token="$(nifi_curl GET "")"; then
    break
  fi
  sleep 2
done

if [[ -z "${nifi_token}" ]]; then
  echo "timed out waiting for NiFi API token minting through direct pod HTTPS" >&2
  exit 1
fi

kubectl -n "${namespace}" get configmap "${release}-site-to-site-provenance" -o jsonpath='{.data.config\.json}' >"${tmpdir}/site-to-site-config.json"
python3 - "${tmpdir}/site-to-site-config.json" "${expected_destination_url}" "${expected_input_port}" "${expected_transport}" "${expected_platform}" "${expected_auth_type}" "${expected_authorized_identity}" "${expected_auth_secret_ref_name}" "${expected_start_position}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
destination_url, input_port, transport, platform, auth_type, authorized_identity, auth_secret_ref_name, start_position = sys.argv[2:]
auth = payload.get("auth", {})
material = auth.get("material", {})
resolved_input_port = input_port or payload.get("destination", {}).get("inputPortName", "")
required = {
    ("/controller", "R"),
    ("/site-to-site", "R"),
    ("destination-input-port", "W", resolved_input_port),
}

if destination_url and payload.get("destination", {}).get("url") != destination_url:
    raise SystemExit(f"expected config destination url {destination_url!r}, got {payload.get('destination', {}).get('url')!r}")
if input_port and payload.get("destination", {}).get("inputPortName") != input_port:
    raise SystemExit(f"expected config input port {input_port!r}, got {payload.get('destination', {}).get('inputPortName')!r}")
if transport and payload.get("transport", {}).get("protocol") != transport:
    raise SystemExit(f"expected config transport {transport!r}, got {payload.get('transport', {}).get('protocol')!r}")
if platform and payload.get("source", {}).get("platform") != platform:
    raise SystemExit(f"expected config platform {platform!r}, got {payload.get('source', {}).get('platform')!r}")
if auth_type and auth.get("type") != auth_type:
    raise SystemExit(f"expected config auth.type {auth_type!r}, got {auth.get('type')!r}")
if authorized_identity and auth.get("authorizedIdentity") != authorized_identity:
    raise SystemExit(f"expected config auth.authorizedIdentity {authorized_identity!r}, got {auth.get('authorizedIdentity')!r}")
if auth_secret_ref_name and material.get("secretName") != auth_secret_ref_name:
    raise SystemExit(f"expected config auth.material.secretName {auth_secret_ref_name!r}, got {material.get('secretName')!r}")
if payload.get("reporting", {}).get("batchSize") != "1000":
    raise SystemExit(f"expected fixed provenance batchSize '1000', got {payload.get('reporting', {}).get('batchSize')!r}")
if start_position and payload.get("provenance", {}).get("startPosition") != start_position:
    raise SystemExit(f"expected config provenance.startPosition {start_position!r}, got {payload.get('provenance', {}).get('startPosition')!r}")

actual = set()
for item in payload.get("receiverRequirements", {}).get("requiredPolicies", []):
    resource = item.get("resource")
    action = item.get("action")
    if resource == "destination-input-port":
        actual.add((resource, action, item.get("inputPortName")))
    else:
        actual.add((resource, action))
if required != actual:
    raise SystemExit(f"expected receiver requirements {sorted(required)!r}, got {sorted(actual)!r}")
print("ok")
PY

nifi_curl_retry GET /flow/reporting-tasks "" 30 2 >"${tmpdir}/reporting-tasks.json"
task_id="$(
  python3 - "${tmpdir}/reporting-tasks.json" "${task_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
matching_type = 0
target_id = ""
for task in payload.get("reportingTasks", []):
    component = task.get("component", {})
    if component.get("type") == "org.apache.nifi.reporting.SiteToSiteProvenanceReportingTask":
        matching_type += 1
    if component.get("name") == target:
        target_id = task["id"]
if matching_type != 1:
    raise SystemExit(f"expected exactly one SiteToSiteProvenanceReportingTask, found {matching_type}")
if not target_id:
    raise SystemExit(f"expected reporting task {target!r} to exist")
print(target_id)
PY
)"

nifi_curl_retry GET "/reporting-tasks/${task_id}" "" 20 2 >"${tmpdir}/reporting-task.json"
python3 - "${tmpdir}/reporting-task.json" "${task_name}" "${expected_destination_url}" "${expected_input_port}" "${expected_transport}" "${expected_platform}" "${expected_start_position}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
task_name, destination_url, input_port, transport, platform, start_position = sys.argv[2:]
component = payload.get("component", {})
properties = component.get("properties", {})
transport_value = properties.get("Transport Protocol") or properties.get("s2s-transport-protocol")
start_position_mapping = {
    "beginningOfStream": "beginning-of-stream",
    "endOfStream": "end-of-stream",
}
if component.get("name") != task_name:
    raise SystemExit(f"expected reporting task name {task_name!r}, got {component.get('name')!r}")
if component.get("type") != "org.apache.nifi.reporting.SiteToSiteProvenanceReportingTask":
    raise SystemExit(f"unexpected reporting task type: {component.get('type')!r}")
if component.get("state") != "RUNNING":
    raise SystemExit(f"expected reporting task state RUNNING, got {component.get('state')!r}")
if properties.get("Destination URL") != destination_url:
    raise SystemExit(f"expected Destination URL {destination_url!r}, got {properties.get('Destination URL')!r}")
if properties.get("Input Port Name") != input_port:
    raise SystemExit(f"expected Input Port Name {input_port!r}, got {properties.get('Input Port Name')!r}")
if transport_value != transport:
    raise SystemExit(f"expected transport {transport!r}, got {transport_value!r}")
if properties.get("Platform") != platform:
    raise SystemExit(f"expected Platform {platform!r}, got {properties.get('Platform')!r}")
if properties.get("Batch Size") not in {"1000", 1000}:
    raise SystemExit(f"expected Batch Size 1000, got {properties.get('Batch Size')!r}")
actual_start_position = properties.get("start-position") or properties.get("Start Position")
if actual_start_position != start_position_mapping[start_position]:
    raise SystemExit(f"expected start-position {start_position_mapping[start_position]!r}, got {actual_start_position!r}")
validation_errors = component.get("validationErrors", []) or []
if validation_errors:
    raise SystemExit("unexpected reporting task validation errors: " + "; ".join(validation_errors))
print("ok")
PY

nifi_curl_retry GET /flow/controller/controller-services "" 30 2 >"${tmpdir}/controller-services.json"
if [[ "${expect_ssl_service}" == "true" ]]; then
  service_id="$(
    python3 - "${tmpdir}/controller-services.json" "${ssl_service_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
target_count = 0
service_id = ""
for service in payload.get("controllerServices", []):
    component = service.get("component", {})
    if component.get("name") == target:
        target_count += 1
        service_id = service["id"]
if target_count != 1:
    raise SystemExit(f"expected exactly one controller service named {target!r}, found {target_count}")
print(service_id)
PY
  )"
  nifi_curl_retry GET "/controller-services/${service_id}" "" 20 2 >"${tmpdir}/controller-service.json"
  python3 - "${tmpdir}/controller-service.json" "${ssl_service_name}" "${expected_auth_type}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
service_name = sys.argv[2]
expected_auth_type = sys.argv[3]
component = payload.get("component", {})
properties = component.get("properties", {})
if component.get("name") != service_name:
    raise SystemExit(f"expected controller service name {service_name!r}, got {component.get('name')!r}")
if component.get("type") != "org.apache.nifi.ssl.StandardRestrictedSSLContextService":
    raise SystemExit(f"unexpected controller service type: {component.get('type')!r}")
if component.get("state") != "ENABLED":
    raise SystemExit(f"expected controller service state ENABLED, got {component.get('state')!r}")
for key in ("Keystore Filename", "Truststore Filename"):
    value = properties.get(key, "")
    if not value:
        raise SystemExit(f"expected controller service property {key!r} to be populated")
if expected_auth_type == "secretRef":
    for key in ("Keystore Filename", "Truststore Filename"):
        value = properties.get(key, "")
        if not value.startswith("/opt/nifi/fabric/site-to-site-provenance-ssl/"):
            raise SystemExit(f"expected {key!r} to use the dedicated site-to-site Secret mount, got {value!r}")
elif expected_auth_type == "workloadTLS":
    for key in ("Keystore Filename", "Truststore Filename"):
        value = properties.get(key, "")
        if not value.startswith("/opt/nifi/tls/"):
            raise SystemExit(f"expected {key!r} to use the main workload TLS mount, got {value!r}")
print("ok")
PY
else
  python3 - "${tmpdir}/controller-services.json" "${ssl_service_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
for service in payload.get("controllerServices", []):
    component = service.get("component", {})
    if component.get("name") == target:
        raise SystemExit(f"expected controller service {target!r} to be absent for insecure mode")
print("ok")
PY
fi

echo "PASS: typed Site-to-Site provenance runtime proof succeeded"
