#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${NAMESPACE:-nifi}"
METRICS_AUTH_SECRET="${METRICS_AUTH_SECRET:-nifi-metrics-auth}"
METRICS_CA_SECRET="${METRICS_CA_SECRET:-nifi-metrics-ca}"
TLS_SECRET="${TLS_SECRET:-nifi-tls}"
TLS_CA_KEY="${TLS_CA_KEY:-ca.crt}"
CREATE_CA_SECRET="true"

AUTH_MODE="${AUTH_MODE:-authorizationHeader}"
AUTHORIZATION_TYPE="${AUTHORIZATION_TYPE:-Bearer}"
TOKEN_KEY="${TOKEN_KEY:-token}"
USERNAME_KEY="${USERNAME_KEY:-username}"
PASSWORD_KEY="${PASSWORD_KEY:-password}"

STATEFULSET="${STATEFULSET:-nifi}"
CONTAINER="${CONTAINER:-nifi}"
SOURCE_AUTH_SECRET="${SOURCE_AUTH_SECRET:-}"
SOURCE_USERNAME_KEY="${SOURCE_USERNAME_KEY:-username}"
SOURCE_PASSWORD_KEY="${SOURCE_PASSWORD_KEY:-password}"

TOKEN_VALUE=""
TOKEN_FILE=""
USERNAME_VALUE=""
USERNAME_FILE=""
PASSWORD_VALUE=""
PASSWORD_FILE=""
CREDENTIALS_VALUE=""
CREDENTIALS_FILE=""
MINT_TOKEN="false"

usage() {
  cat <<'EOF'
Usage:
  hack/bootstrap-metrics-machine-auth.sh [options]

Creates the Kubernetes Secret material used by the chart-owned metrics subsystem.
This helper is provider-agnostic at the public contract level:
it does not provision a machine principal in an IdP and does not write back to a provider.
It only creates:
  1. the metrics auth Secret expected by observability.metrics.*.machineAuth
  2. optionally, a metrics CA Secret copied from the NiFi TLS Secret

Common options:
  --namespace ns                  Namespace containing NiFi and the target Secrets.
  --metrics-auth-secret name      Secret name to create for metrics auth. Default: nifi-metrics-auth
  --metrics-ca-secret name        Secret name to create for the CA bundle. Default: nifi-metrics-ca
  --tls-secret name               Source NiFi TLS Secret. Default: nifi-tls
  --tls-ca-key key                Key in the TLS Secret containing the CA cert. Default: ca.crt
  --no-ca-secret                  Do not create or update the metrics CA Secret.

Auth contract options:
  --auth-mode mode                bearerToken | authorizationHeader | basicAuth
                                  Default: authorizationHeader
  --authorization-type type       Header type for authorizationHeader mode. Default: Bearer
  --token-key key                 Secret key for bearer tokens or authorization credentials. Default: token
  --username-key key              Secret key for basicAuth username. Default: username
  --password-key key              Secret key for basicAuth password. Default: password

Credential source options:
  --token value                   Pre-minted token value.
  --token-file path               Read pre-minted token value from file.
  --credentials value             Prebuilt authorization credentials string.
  --credentials-file path         Read authorization credentials string from file.
  --username value                Username for basicAuth or token minting.
  --username-file path            Read username from file.
  --password value                Password for basicAuth or token minting.
  --password-file path            Read password from file.
  --source-auth-secret name       Existing Secret containing username/password to reuse.
  --source-username-key key       Username key in --source-auth-secret. Default: username
  --source-password-key key       Password key in --source-auth-secret. Default: password

Token minting options:
  --mint-token                    Mint a NiFi access token through /nifi-api/access/token.
                                  Supported only with auth-mode=bearerToken or authorizationHeader.
  --statefulset name              NiFi StatefulSet name used for token minting. Default: nifi
  --container name                NiFi container name used for token minting. Default: nifi

Examples:
  Mint a Bearer token from an existing machine-credential Secret and create both Secrets:
    hack/bootstrap-metrics-machine-auth.sh \
      --namespace nifi \
      --auth-mode authorizationHeader \
      --source-auth-secret nifi-auth \
      --mint-token

  Use a pre-minted token:
    hack/bootstrap-metrics-machine-auth.sh \
      --namespace nifi \
      --auth-mode bearerToken \
      --token-file /secure/path/token.txt

  Reuse username/password directly for basicAuth:
    hack/bootstrap-metrics-machine-auth.sh \
      --namespace nifi \
      --auth-mode basicAuth \
      --source-auth-secret nifi-machine-auth
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

read_file_trimmed() {
  local path="$1"
  tr -d '\r' <"${path}" | sed -e :a -e '/^\n*$/{$d;N;};/\n$/ba' | tr -d '\n'
}

read_secret_key() {
  local secret_name="$1"
  local key="$2"
  kubectl -n "${NAMESPACE}" get secret "${secret_name}" -o go-template="{{index .data \"${key}\"}}" | base64 --decode
}

ensure_single_source() {
  local label="$1"
  shift
  local count=0
  local value
  for value in "$@"; do
    if [[ -n "${value}" ]]; then
      count=$((count + 1))
    fi
  done
  if (( count > 1 )); then
    echo "${label} was provided through more than one source" >&2
    exit 1
  fi
}

resolve_username_password() {
  ensure_single_source "username" "${USERNAME_VALUE}" "${USERNAME_FILE}" "${SOURCE_AUTH_SECRET}"
  ensure_single_source "password" "${PASSWORD_VALUE}" "${PASSWORD_FILE}" "${SOURCE_AUTH_SECRET}"

  if [[ -n "${USERNAME_FILE}" ]]; then
    USERNAME_VALUE="$(read_file_trimmed "${USERNAME_FILE}")"
  fi
  if [[ -n "${PASSWORD_FILE}" ]]; then
    PASSWORD_VALUE="$(read_file_trimmed "${PASSWORD_FILE}")"
  fi
  if [[ -n "${SOURCE_AUTH_SECRET}" ]]; then
    USERNAME_VALUE="$(read_secret_key "${SOURCE_AUTH_SECRET}" "${SOURCE_USERNAME_KEY}")"
    PASSWORD_VALUE="$(read_secret_key "${SOURCE_AUTH_SECRET}" "${SOURCE_PASSWORD_KEY}")"
  fi
}

mint_token() {
  local service_name
  local tls_mount_path
  local host

  resolve_username_password

  if [[ -z "${USERNAME_VALUE}" || -z "${PASSWORD_VALUE}" ]]; then
    echo "--mint-token requires username/password or --source-auth-secret" >&2
    exit 1
  fi

  service_name="$(kubectl -n "${NAMESPACE}" get statefulset "${STATEFULSET}" -o jsonpath='{.spec.serviceName}')"
  tls_mount_path="$(kubectl -n "${NAMESPACE}" get statefulset "${STATEFULSET}" -o jsonpath="{.spec.template.spec.containers[?(@.name==\"${CONTAINER}\")].volumeMounts[?(@.name==\"tls\")].mountPath}")"

  if [[ -z "${service_name}" || -z "${tls_mount_path}" ]]; then
    echo "failed to infer service name or tls mount path from statefulset/${STATEFULSET}" >&2
    exit 1
  fi

  host="${STATEFULSET}-0.${service_name}.${NAMESPACE}.svc.cluster.local"

  kubectl -n "${NAMESPACE}" exec "${STATEFULSET}-0" -c "${CONTAINER}" -- \
    env NIFI_HOST="${host}" NIFI_USERNAME="${USERNAME_VALUE}" NIFI_PASSWORD="${PASSWORD_VALUE}" TLS_CA_PATH="${tls_mount_path}/ca.crt" sh -ec '
      curl --silent --show-error --fail \
        --cacert "${TLS_CA_PATH}" \
        -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
        --data-urlencode "username=${NIFI_USERNAME}" \
        --data-urlencode "password=${NIFI_PASSWORD}" \
        "https://${NIFI_HOST}:8443/nifi-api/access/token"
    '
}

create_ca_secret() {
  local ca_b64
  local ca_file
  ca_b64="$(kubectl -n "${NAMESPACE}" get secret "${TLS_SECRET}" -o go-template="{{index .data \"${TLS_CA_KEY}\"}}")"
  if [[ -z "${ca_b64}" ]]; then
    echo "failed to read ${TLS_CA_KEY} from secret/${TLS_SECRET}" >&2
    exit 1
  fi

  ca_file="$(mktemp)"
  printf '%s' "${ca_b64}" | base64 --decode >"${ca_file}"

  kubectl -n "${NAMESPACE}" create secret generic "${METRICS_CA_SECRET}" \
    --from-file="${TLS_CA_KEY}=${ca_file}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  rm -f "${ca_file}"
}

create_metrics_auth_secret() {
  case "${AUTH_MODE}" in
    bearerToken)
      ensure_single_source "token" "${TOKEN_VALUE}" "${TOKEN_FILE}"
      if [[ -n "${TOKEN_FILE}" ]]; then
        TOKEN_VALUE="$(read_file_trimmed "${TOKEN_FILE}")"
      fi
      if [[ -z "${TOKEN_VALUE}" ]]; then
        echo "bearerToken mode requires --token, --token-file, or --mint-token" >&2
        exit 1
      fi
      kubectl -n "${NAMESPACE}" create secret generic "${METRICS_AUTH_SECRET}" \
        --from-literal="${TOKEN_KEY}=${TOKEN_VALUE}" \
        --dry-run=client -o yaml | kubectl apply -f - >/dev/null
      ;;
    authorizationHeader)
      ensure_single_source "authorization credentials" "${CREDENTIALS_VALUE}" "${CREDENTIALS_FILE}" "${TOKEN_VALUE}" "${TOKEN_FILE}"
      if [[ -n "${CREDENTIALS_FILE}" ]]; then
        CREDENTIALS_VALUE="$(read_file_trimmed "${CREDENTIALS_FILE}")"
      fi
      if [[ -n "${TOKEN_FILE}" ]]; then
        TOKEN_VALUE="$(read_file_trimmed "${TOKEN_FILE}")"
      fi
      if [[ -n "${TOKEN_VALUE}" ]]; then
        CREDENTIALS_VALUE="${TOKEN_VALUE}"
      fi
      if [[ -z "${CREDENTIALS_VALUE}" ]]; then
        echo "authorizationHeader mode requires --credentials, --credentials-file, --token, --token-file, or --mint-token" >&2
        exit 1
      fi
      kubectl -n "${NAMESPACE}" create secret generic "${METRICS_AUTH_SECRET}" \
        --from-literal="${TOKEN_KEY}=${CREDENTIALS_VALUE}" \
        --dry-run=client -o yaml | kubectl apply -f - >/dev/null
      ;;
    basicAuth)
      resolve_username_password
      if [[ -z "${USERNAME_VALUE}" || -z "${PASSWORD_VALUE}" ]]; then
        echo "basicAuth mode requires username/password or --source-auth-secret" >&2
        exit 1
      fi
      kubectl -n "${NAMESPACE}" create secret generic "${METRICS_AUTH_SECRET}" \
        --from-literal="${USERNAME_KEY}=${USERNAME_VALUE}" \
        --from-literal="${PASSWORD_KEY}=${PASSWORD_VALUE}" \
        --dry-run=client -o yaml | kubectl apply -f - >/dev/null
      ;;
    *)
      echo "unsupported --auth-mode: ${AUTH_MODE}" >&2
      exit 1
      ;;
  esac
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="$2"
      shift 2
      ;;
    --metrics-auth-secret)
      METRICS_AUTH_SECRET="$2"
      shift 2
      ;;
    --metrics-ca-secret)
      METRICS_CA_SECRET="$2"
      shift 2
      ;;
    --tls-secret)
      TLS_SECRET="$2"
      shift 2
      ;;
    --tls-ca-key)
      TLS_CA_KEY="$2"
      shift 2
      ;;
    --no-ca-secret)
      CREATE_CA_SECRET="false"
      shift
      ;;
    --auth-mode)
      AUTH_MODE="$2"
      shift 2
      ;;
    --authorization-type)
      AUTHORIZATION_TYPE="$2"
      shift 2
      ;;
    --token-key)
      TOKEN_KEY="$2"
      shift 2
      ;;
    --username-key)
      USERNAME_KEY="$2"
      shift 2
      ;;
    --password-key)
      PASSWORD_KEY="$2"
      shift 2
      ;;
    --token)
      TOKEN_VALUE="$2"
      shift 2
      ;;
    --token-file)
      TOKEN_FILE="$2"
      shift 2
      ;;
    --credentials)
      CREDENTIALS_VALUE="$2"
      shift 2
      ;;
    --credentials-file)
      CREDENTIALS_FILE="$2"
      shift 2
      ;;
    --username)
      USERNAME_VALUE="$2"
      shift 2
      ;;
    --username-file)
      USERNAME_FILE="$2"
      shift 2
      ;;
    --password)
      PASSWORD_VALUE="$2"
      shift 2
      ;;
    --password-file)
      PASSWORD_FILE="$2"
      shift 2
      ;;
    --source-auth-secret)
      SOURCE_AUTH_SECRET="$2"
      shift 2
      ;;
    --source-username-key)
      SOURCE_USERNAME_KEY="$2"
      shift 2
      ;;
    --source-password-key)
      SOURCE_PASSWORD_KEY="$2"
      shift 2
      ;;
    --mint-token)
      MINT_TOKEN="true"
      shift
      ;;
    --statefulset)
      STATEFULSET="$2"
      shift 2
      ;;
    --container)
      CONTAINER="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

require_command kubectl
require_command base64

if [[ "${AUTH_MODE}" != "bearerToken" && "${AUTH_MODE}" != "authorizationHeader" && "${AUTH_MODE}" != "basicAuth" ]]; then
  echo "--auth-mode must be one of: bearerToken, authorizationHeader, basicAuth" >&2
  exit 1
fi

if [[ "${MINT_TOKEN}" == "true" && "${AUTH_MODE}" == "basicAuth" ]]; then
  echo "--mint-token cannot be combined with --auth-mode=basicAuth" >&2
  exit 1
fi

if [[ "${MINT_TOKEN}" == "true" && ( -n "${TOKEN_VALUE}" || -n "${TOKEN_FILE}" || -n "${CREDENTIALS_VALUE}" || -n "${CREDENTIALS_FILE}" ) ]]; then
  echo "--mint-token cannot be combined with pre-supplied token or credentials inputs" >&2
  exit 1
fi

if [[ "${MINT_TOKEN}" == "true" ]]; then
  TOKEN_VALUE="$(mint_token)"
fi

if [[ "${CREATE_CA_SECRET}" == "true" ]]; then
  create_ca_secret
fi

create_metrics_auth_secret

echo "Created metrics auth Secret ${METRICS_AUTH_SECRET} in namespace ${NAMESPACE}"
case "${AUTH_MODE}" in
  bearerToken)
    echo "  auth contract: bearerToken via key ${TOKEN_KEY}"
    ;;
  authorizationHeader)
    echo "  auth contract: authorizationHeader via key ${TOKEN_KEY} and chart value type ${AUTHORIZATION_TYPE}"
    ;;
  basicAuth)
    echo "  auth contract: basicAuth via keys ${USERNAME_KEY}/${PASSWORD_KEY}"
    ;;
esac

if [[ "${CREATE_CA_SECRET}" == "true" ]]; then
  echo "Created metrics CA Secret ${METRICS_CA_SECRET} in namespace ${NAMESPACE} from ${TLS_SECRET}:${TLS_CA_KEY}"
else
  echo "Skipped metrics CA Secret creation"
fi

echo "Note: this helper does not provision a machine principal or write back to an IdP."
