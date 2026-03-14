#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
auth_secret="nifi-auth"
task_name="fabric-site-to-site-metrics-export"
ssl_service_name="fabric-site-to-site-metrics-ssl"
expected_destination_url=""
expected_input_port=""
expected_transport="HTTP"
expected_format="AmbariFormat"
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
    --expected-format)
      expected_format="$2"
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
  echo "==> Site-to-Site metrics diagnostics"
  kubectl -n "${namespace}" get nificluster "${release}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${namespace}" get configmap "${release}-site-to-site-metrics" -o yaml || true
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
}

on_error() {
  echo "FAIL: typed Site-to-Site metrics runtime proof failed" >&2
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

nifi_curl_retry GET /flow/reporting-tasks "" 30 2 >"${tmpdir}/reporting-tasks.json"
task_id="$(
  python3 - "${tmpdir}/reporting-tasks.json" "${task_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
for task in payload.get("reportingTasks", []):
    component = task.get("component", {})
    if component.get("name") == target:
        print(task["id"])
        break
PY
)"

if [[ -z "${task_id}" ]]; then
  echo "expected reporting task ${task_name} to exist" >&2
  exit 1
fi

nifi_curl_retry GET "/reporting-tasks/${task_id}" "" 20 2 >"${tmpdir}/reporting-task.json"
python3 - "${tmpdir}/reporting-task.json" "${task_name}" "${expected_destination_url}" "${expected_input_port}" "${expected_transport}" "${expected_format}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
task_name, destination_url, input_port, transport, fmt = sys.argv[2:]
component = payload.get("component", {})
properties = component.get("properties", {})
transport_value = properties.get("s2s-transport-protocol") or properties.get("Transport Protocol")
format_value = properties.get("s2s-metrics-format") or properties.get("Output Format") or properties.get("Metrics Format")
if component.get("name") != task_name:
    raise SystemExit(f"expected reporting task name {task_name!r}, got {component.get('name')!r}")
if component.get("type") != "org.apache.nifi.reporting.SiteToSiteMetricsReportingTask":
    raise SystemExit(f"unexpected reporting task type: {component.get('type')!r}")
if component.get("state") != "RUNNING":
    raise SystemExit(f"expected reporting task state RUNNING, got {component.get('state')!r}")
if properties.get("Destination URL") != destination_url:
    raise SystemExit(f"expected Destination URL {destination_url!r}, got {properties.get('Destination URL')!r}")
if properties.get("Input Port Name") != input_port:
    raise SystemExit(f"expected Input Port Name {input_port!r}, got {properties.get('Input Port Name')!r}")
if transport_value != transport:
    raise SystemExit(f"expected transport {transport!r}, got {transport_value!r}")
acceptable_formats = {fmt, "ambari-format"} if fmt == "AmbariFormat" else {fmt}
if format_value not in acceptable_formats:
    raise SystemExit(f"expected output format in {sorted(acceptable_formats)!r}, got {format_value!r}")
validation_errors = component.get("validationErrors", []) or []
if validation_errors:
    raise SystemExit("unexpected reporting task validation errors: " + "; ".join(validation_errors))
print("ok")
PY

if [[ "${expect_ssl_service}" == "true" ]]; then
  nifi_curl_retry GET /flow/controller/controller-services "" 30 2 >"${tmpdir}/controller-services.json"
  service_id="$(
    python3 - "${tmpdir}/controller-services.json" "${ssl_service_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
target = sys.argv[2]
for service in payload.get("controllerServices", []):
    component = service.get("component", {})
    if component.get("name") == target:
        print(service["id"])
        break
PY
  )"
  if [[ -z "${service_id}" ]]; then
    echo "expected controller service ${ssl_service_name} to exist" >&2
    exit 1
  fi
  nifi_curl_retry GET "/controller-services/${service_id}" "" 20 2 >"${tmpdir}/controller-service.json"
  python3 - "${tmpdir}/controller-service.json" "${ssl_service_name}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
service_name = sys.argv[2]
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
print("ok")
PY
fi

echo "PASS: typed Site-to-Site metrics runtime proof succeeded"
