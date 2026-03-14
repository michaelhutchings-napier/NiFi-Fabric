#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

sender_namespace="nifi"
sender_release="nifi"
sender_client_secret="nifi-site-to-site-receiver-client"
receiver_namespace="site-to-site-receiver"
receiver_release="site-to-site-receiver"
receiver_tls_secret="site-to-site-receiver-tls"
receiver_auth_secret="site-to-site-receiver-auth"
receiver_values_file="examples/standalone-site-to-site-receiver-kind-values.yaml"
input_port_name="nifi-metrics"
receiver_processor_name="site-to-site-receiver-log"
receiver_processor_log_prefix="site-to-site-metrics-proof"
client_identity=""
admin_username="${NIFI_SITE_TO_SITE_RECEIVER_USERNAME:-admin}"
admin_password="${NIFI_SITE_TO_SITE_RECEIVER_PASSWORD:-ChangeMeChangeMe1!}"
secret_password="${NIFI_SITE_TO_SITE_RECEIVER_KEYSTORE_PASSWORD:-ChangeMeChangeMe1!}"
nifi_image="${NIFI_IMAGE:-apache/nifi:2.0.0}"

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
    --sender-client-secret)
      sender_client_secret="$2"
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
    --receiver-tls-secret)
      receiver_tls_secret="$2"
      shift 2
      ;;
    --receiver-auth-secret)
      receiver_auth_secret="$2"
      shift 2
      ;;
    --receiver-values-file)
      receiver_values_file="$2"
      shift 2
      ;;
    --input-port)
      input_port_name="$2"
      shift 2
      ;;
    --receiver-processor-name)
      receiver_processor_name="$2"
      shift 2
      ;;
    --client-identity)
      client_identity="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

for cmd in kubectl helm openssl python3; do
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

receiver_service_name="${receiver_release}"
receiver_headless_name="${receiver_release}-headless"

cat >"${tmpdir}/receiver-openssl.cnf" <<EOF
[ req ]
distinguished_name = dn
prompt = no
req_extensions = req_ext

[ dn ]
CN = ${receiver_service_name}
O = NiFi-Fabric

[ req_ext ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = ${receiver_service_name}
DNS.2 = ${receiver_service_name}.${receiver_namespace}.svc
DNS.3 = ${receiver_service_name}.${receiver_namespace}.svc.cluster.local
DNS.4 = ${receiver_headless_name}
DNS.5 = ${receiver_headless_name}.${receiver_namespace}.svc
DNS.6 = ${receiver_headless_name}.${receiver_namespace}.svc.cluster.local
DNS.7 = *.${receiver_headless_name}.${receiver_namespace}.svc
DNS.8 = *.${receiver_headless_name}.${receiver_namespace}.svc.cluster.local
EOF

cat >"${tmpdir}/client-openssl.cnf" <<EOF
[ req ]
distinguished_name = dn
prompt = no

[ dn ]
${client_identity//,/\\n}
EOF

openssl genrsa -out "${tmpdir}/ca.key" 2048 >/dev/null 2>&1
openssl req -x509 -new -nodes \
  -key "${tmpdir}/ca.key" \
  -sha256 \
  -days 365 \
  -subj "/CN=${receiver_release}-site-to-site-ca/O=NiFi-Fabric" \
  -out "${tmpdir}/ca.crt" >/dev/null 2>&1

openssl genrsa -out "${tmpdir}/receiver.key" 2048 >/dev/null 2>&1
openssl req -new \
  -key "${tmpdir}/receiver.key" \
  -out "${tmpdir}/receiver.csr" \
  -config "${tmpdir}/receiver-openssl.cnf" >/dev/null 2>&1
openssl x509 -req \
  -in "${tmpdir}/receiver.csr" \
  -CA "${tmpdir}/ca.crt" \
  -CAkey "${tmpdir}/ca.key" \
  -CAcreateserial \
  -out "${tmpdir}/receiver.crt" \
  -days 365 \
  -sha256 \
  -extensions req_ext \
  -extfile "${tmpdir}/receiver-openssl.cnf" >/dev/null 2>&1

openssl genrsa -out "${tmpdir}/client.key" 2048 >/dev/null 2>&1
openssl req -new \
  -key "${tmpdir}/client.key" \
  -out "${tmpdir}/client.csr" \
  -subj "/CN=nifi-site-to-site-metrics-client/O=NiFi-Fabric" >/dev/null 2>&1
openssl x509 -req \
  -in "${tmpdir}/client.csr" \
  -CA "${tmpdir}/ca.crt" \
  -CAkey "${tmpdir}/ca.key" \
  -CAcreateserial \
  -out "${tmpdir}/client.crt" \
  -days 365 \
  -sha256 >/dev/null 2>&1

if [[ -z "${client_identity}" ]]; then
  client_identity="$(
    openssl x509 -in "${tmpdir}/client.crt" -noout -subject -nameopt RFC2253 \
      | sed 's/^subject=//' \
      | sed 's/,/, /g'
  )"
fi

openssl pkcs12 -export \
  -name nifi \
  -in "${tmpdir}/receiver.crt" \
  -inkey "${tmpdir}/receiver.key" \
  -certfile "${tmpdir}/ca.crt" \
  -out "${tmpdir}/receiver-keystore.p12" \
  -passout "pass:${secret_password}" >/dev/null 2>&1

openssl pkcs12 -export \
  -name nifi-site-to-site-metrics-client \
  -in "${tmpdir}/client.crt" \
  -inkey "${tmpdir}/client.key" \
  -certfile "${tmpdir}/ca.crt" \
  -out "${tmpdir}/client-keystore.p12" \
  -passout "pass:${secret_password}" >/dev/null 2>&1

cluster_keytool_truststore() {
  local namespace="$1"
  local temp_name="site-to-site-keytool-$$"

  kubectl create namespace "${namespace}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${namespace}" create configmap "${temp_name}" \
    --from-file=ca.crt="${tmpdir}/ca.crt" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  kubectl -n "${namespace}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${temp_name}
spec:
  restartPolicy: Never
  containers:
  - name: keytool
    image: ${nifi_image}
    imagePullPolicy: IfNotPresent
    command:
    - /bin/sh
    - -ec
    - |
      keytool -importcert \
        -alias site-to-site-kind-ca \
        -file /input/ca.crt \
        -keystore /output/truststore.p12 \
        -storetype PKCS12 \
        -storepass "${secret_password}" \
        -noprompt >/dev/null 2>&1
      touch /output/.ready
      sleep 3600
    volumeMounts:
    - name: input
      mountPath: /input
      readOnly: true
    - name: output
      mountPath: /output
  volumes:
  - name: input
    configMap:
      name: ${temp_name}
  - name: output
    emptyDir: {}
EOF

  local deadline=$(( $(date +%s) + 180 ))
  while true; do
    phase="$(kubectl -n "${namespace}" get pod "${temp_name}" -o jsonpath='{.status.phase}')"
    case "${phase}" in
      Running)
        if kubectl -n "${namespace}" exec "${temp_name}" -- test -f /output/.ready >/dev/null 2>&1; then
          break
        fi
        ;;
      Failed)
        kubectl -n "${namespace}" logs "${temp_name}" >&2 || true
        return 1
        ;;
    esac
    if (( $(date +%s) >= deadline )); then
      kubectl -n "${namespace}" logs "${temp_name}" >&2 || true
      echo "timed out waiting for truststore generation pod in ${namespace}" >&2
      return 1
    fi
    sleep 2
  done

  kubectl -n "${namespace}" cp "${temp_name}:/output/truststore.p12" "${tmpdir}/truststore.p12" >/dev/null
  kubectl -n "${namespace}" delete pod "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n "${namespace}" delete configmap "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
}

if command -v keytool >/dev/null 2>&1; then
  keytool -importcert \
    -alias site-to-site-kind-ca \
    -file "${tmpdir}/ca.crt" \
    -keystore "${tmpdir}/truststore.p12" \
    -storetype PKCS12 \
    -storepass "${secret_password}" \
    -noprompt >/dev/null 2>&1
else
  cluster_keytool_truststore "${receiver_namespace}"
fi

kubectl create namespace "${sender_namespace}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create namespace "${receiver_namespace}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

kubectl -n "${receiver_namespace}" create secret generic "${receiver_tls_secret}" \
  --from-file=keystore.p12="${tmpdir}/receiver-keystore.p12" \
  --from-file=truststore.p12="${tmpdir}/truststore.p12" \
  --from-file=ca.crt="${tmpdir}/ca.crt" \
  --from-literal=keystorePassword="${secret_password}" \
  --from-literal=truststorePassword="${secret_password}" \
  --from-literal=sensitivePropsKey="changeit-change-me" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

kubectl -n "${receiver_namespace}" create secret generic "${receiver_auth_secret}" \
  --from-literal=username="${admin_username}" \
  --from-literal=password="${admin_password}" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

kubectl -n "${sender_namespace}" create secret generic "${sender_client_secret}" \
  --from-file=keystore.p12="${tmpdir}/client-keystore.p12" \
  --from-file=truststore.p12="${tmpdir}/truststore.p12" \
  --from-file=ca.crt="${tmpdir}/ca.crt" \
  --from-literal=keystorePassword="${secret_password}" \
  --from-literal=truststorePassword="${secret_password}" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

helm -n "${receiver_namespace}" uninstall "${receiver_release}" >/dev/null 2>&1 || true

helm upgrade --install "${receiver_release}" "${ROOT_DIR}/charts/nifi" \
  --namespace "${receiver_namespace}" \
  --create-namespace \
  -f "${ROOT_DIR}/${receiver_values_file}" >/dev/null

kubectl -n "${receiver_namespace}" rollout status statefulset/"${receiver_release}" --timeout=10m >/dev/null

receiver_pod="${receiver_release}-0"
receiver_host="${receiver_release}-0.${receiver_release}-headless.${receiver_namespace}.svc.cluster.local"
receiver_base_url="https://${receiver_host}:8443/nifi-api"
receiver_token=""

receiver_curl() {
  local method="$1"
  local path="$2"
  local body="${3:-}"

  kubectl -n "${receiver_namespace}" exec -i -c nifi "${receiver_pod}" -- env \
    NIFI_USERNAME="${admin_username}" \
    NIFI_PASSWORD="${admin_password}" \
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
}

receiver_request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local response status

  response="$(receiver_curl "${method}" "${path}" "${body}")"
  status="${response##*$'\n'}"
  if [[ ! "${status}" =~ ^2 ]]; then
    echo "receiver NiFi API ${method} ${path} returned HTTP ${status}" >&2
    echo "${response%$'\n'*}" >&2
    return 1
  fi

  printf '%s' "${response%$'\n'*}"
}

receiver_request_retry() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local attempts="${4:-30}"
  local sleep_seconds="${5:-2}"
  local output
  local attempt

  for attempt in $(seq 1 "${attempts}"); do
    if output="$(receiver_request "${method}" "${path}" "${body}")"; then
      printf '%s' "${output}"
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  return 1
}

wait_for_cluster_summary() {
  local deadline=$(( $(date +%s) + 180 ))
  local payload

  while (( $(date +%s) < deadline )); do
    if payload="$(receiver_request GET /flow/cluster/summary)"; then
      if python3 - "${payload}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
summary = payload.get("clusterSummary", {})
connected = int(summary.get("connectedNodeCount", 0) or 0)
total = int(summary.get("totalNodeCount", 0) or 0)
if (
    summary.get("clustered") is True
    and summary.get("connectedToCluster") is True
    and connected >= 1
    and total >= 1
    and connected == total
):
    raise SystemExit(0)
raise SystemExit(1)
PY
      then
        return 0
      fi
    fi
    sleep 2
  done

  echo "timed out waiting for receiver cluster summary to stabilize" >&2
  return 1
}

for _ in $(seq 1 60); do
  if receiver_token="$(receiver_curl GET "")"; then
    break
  fi
  sleep 2
done

if [[ -z "${receiver_token}" ]]; then
  echo "timed out waiting for receiver NiFi API token" >&2
  exit 1
fi

receiver_request_retry GET /flow/process-groups/root "" 60 2 >/dev/null
wait_for_cluster_summary

python3 - "${input_port_name}" >"${tmpdir}/create-input-port.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-receiver-proof",
        "version": 0,
    },
    "component": {
        "name": sys.argv[1],
        "position": {"x": 120.0, "y": 120.0},
        "allowRemoteAccess": True,
    },
}
json.dump(payload, sys.stdout)
PY

receiver_request_retry POST /process-groups/root/input-ports "$(tr -d '\n' < "${tmpdir}/create-input-port.json")" 30 2 >"${tmpdir}/created-input-port.json"

mapfile -t port_meta < <(python3 - "${tmpdir}/created-input-port.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
component = payload.get("component", {})
revision = payload.get("revision", {})
print(payload.get("id") or component.get("id") or "")
print(revision.get("version", 0))
PY
)

port_id="${port_meta[0]:-}"
port_version="${port_meta[1]:-0}"

if [[ -z "${port_id}" ]]; then
  echo "failed to create receiver input port" >&2
  exit 1
fi

python3 - "${receiver_processor_name}" "${receiver_processor_log_prefix}" >"${tmpdir}/create-processor.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-receiver-proof",
        "version": 0,
    },
    "component": {
        "name": sys.argv[1],
        "type": "org.apache.nifi.processors.standard.LogMessage",
        "bundle": {
            "group": "org.apache.nifi",
            "artifact": "nifi-standard-nar",
            "version": "2.0.0",
        },
        "position": {"x": 380.0, "y": 120.0},
        "config": {
            "schedulingPeriod": "1 sec",
            "autoTerminatedRelationships": ["success"],
            "properties": {
                "log-level": "info",
                "log-prefix": sys.argv[2],
                "log-message": "typed site-to-site metrics export delivered",
            },
        },
    },
}
json.dump(payload, sys.stdout)
PY

receiver_request_retry POST /process-groups/root/processors "$(tr -d '\n' < "${tmpdir}/create-processor.json")" 30 2 >"${tmpdir}/created-processor.json"

mapfile -t processor_meta < <(python3 - "${tmpdir}/created-processor.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
component = payload.get("component", {})
revision = payload.get("revision", {})
parent_group = component.get("parentGroupId", "")
print(payload.get("id") or component.get("id") or "")
print(revision.get("version", 0))
print(parent_group)
PY
)

processor_id="${processor_meta[0]:-}"
processor_version="${processor_meta[1]:-0}"
root_group_id="${processor_meta[2]:-}"

if [[ -z "${processor_id}" ]]; then
  echo "failed to create receiver proof processor" >&2
  exit 1
fi

if [[ -z "${root_group_id}" ]]; then
  echo "failed to resolve receiver root process group id" >&2
  exit 1
fi

python3 - "${port_id}" "${processor_id}" "${root_group_id}" >"${tmpdir}/create-connection.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-receiver-proof",
        "version": 0,
    },
    "component": {
        "name": "site-to-site-metrics-delivery",
        "source": {
            "id": sys.argv[1],
            "groupId": sys.argv[3],
            "type": "INPUT_PORT",
        },
        "destination": {
            "id": sys.argv[2],
            "groupId": sys.argv[3],
            "type": "PROCESSOR",
        },
        "selectedRelationships": [],
        "backPressureObjectThreshold": "10000",
        "backPressureDataSizeThreshold": "1 GB",
        "flowFileExpiration": "0 sec",
    },
}
json.dump(payload, sys.stdout)
PY

receiver_request_retry POST /process-groups/root/connections "$(tr -d '\n' < "${tmpdir}/create-connection.json")" 30 2 >"${tmpdir}/created-connection.json"

python3 - "${client_identity}" >"${tmpdir}/create-user.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-receiver-proof",
        "version": 0,
    },
    "component": {
        "identity": sys.argv[1],
    },
}
json.dump(payload, sys.stdout)
PY

receiver_request_retry POST /tenants/users "$(tr -d '\n' < "${tmpdir}/create-user.json")" 30 2 >"${tmpdir}/created-user.json"

user_id="$(
  python3 - "${tmpdir}/created-user.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
print(payload.get("id") or payload.get("component", {}).get("id") or "")
PY
)"

if [[ -z "${user_id}" ]]; then
  echo "failed to create receiver Site-to-Site client user" >&2
  exit 1
fi

create_policy_payload() {
  local action="$1"
  local resource="$2"
  python3 - "${action}" "${resource}" "${user_id}" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-receiver-proof",
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

policy_id_for() {
  local action="$1"
  local resource="$2"
  local policy_action=""

  case "${action}" in
    read) policy_action="R" ;;
    write) policy_action="W" ;;
    *)
      echo "unsupported policy action lookup: ${action}" >&2
      return 1
      ;;
  esac

  kubectl -n "${receiver_namespace}" exec -i -c nifi "${receiver_pod}" -- env \
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

ensure_policy_user_binding() {
  local action="$1"
  local resource="$2"
  local policy_id=""

  policy_id="$(policy_id_for "${action}" "${resource}")"
  if [[ -z "${policy_id}" ]]; then
    receiver_request_retry POST /policies "$(create_policy_payload "${action}" "${resource}")" 30 2 >/dev/null
    return 0
  fi

  receiver_request_retry GET "/policies/${policy_id}" "" 20 2 >"${tmpdir}/policy-${policy_id}.json"
  if python3 - "${tmpdir}/policy-${policy_id}.json" "${user_id}" >"${tmpdir}/policy-${policy_id}-update.json" <<'PY'
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
        "clientId": "site-to-site-receiver-proof",
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
    receiver_request_retry PUT "/policies/${policy_id}" "$(tr -d '\n' < "${tmpdir}/policy-${policy_id}-update.json")" 20 2 >/dev/null
  fi
}

ensure_policy_user_binding read /controller
ensure_policy_user_binding read /site-to-site
ensure_policy_user_binding write "/data-transfer/input-ports/${port_id}"

python3 - "${processor_version}" >"${tmpdir}/start-processor.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-receiver-proof",
        "version": int(sys.argv[1]),
    },
    "state": "RUNNING",
    "disconnectedNodeAcknowledged": False,
}
json.dump(payload, sys.stdout)
PY

receiver_request_retry PUT "/processors/${processor_id}/run-status" "$(tr -d '\n' < "${tmpdir}/start-processor.json")" 30 2 >/dev/null

python3 - "${port_version}" >"${tmpdir}/start-input-port.json" <<'PY'
import json
import sys

payload = {
    "revision": {
        "clientId": "site-to-site-receiver-proof",
        "version": int(sys.argv[1]),
    },
    "state": "RUNNING",
    "disconnectedNodeAcknowledged": False,
}
json.dump(payload, sys.stdout)
PY

receiver_request_retry PUT "/input-ports/${port_id}/run-status" "$(tr -d '\n' < "${tmpdir}/start-input-port.json")" 30 2 >/dev/null

receiver_request_retry GET "/processors/${processor_id}" "" 20 2 >"${tmpdir}/receiver-processor.json"
receiver_request_retry GET "/input-ports/${port_id}" "" 20 2 >"${tmpdir}/receiver-input-port.json"

echo "PASS: receiver proof harness ready"
echo "  receiver namespace: ${receiver_namespace}"
echo "  receiver release: ${receiver_release}"
echo "  sender client secret: ${sender_namespace}/${sender_client_secret}"
echo "  receiver input port: ${input_port_name} (${port_id})"
echo "  receiver proof processor: ${receiver_processor_name} (${processor_id})"
echo "  sender client identity: ${client_identity}"
