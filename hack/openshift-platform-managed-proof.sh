#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

OC="${OC:-oc}"
HELM="${HELM:-helm}"
KUBECTL="${KUBECTL:-kubectl}"

NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
NIFI_RESOURCE_NAME="${NIFI_RESOURCE_NAME:-}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
AUTH_SECRET="${AUTH_SECRET:-nifi-auth}"
NIFI_SERVICE_ACCOUNT="${NIFI_SERVICE_ACCOUNT:-}"
BASE_VALUES_FILE="${BASE_VALUES_FILE:-examples/platform-managed-values.yaml}"
OPENSHIFT_VALUES_FILE="${OPENSHIFT_VALUES_FILE:-examples/openshift/managed-values.yaml}"
HEALTH_TIMEOUT_SECONDS="${HEALTH_TIMEOUT_SECONDS:-900}"
ROUTE_PROOF="${ROUTE_PROOF:-false}"
AUTH_PROOF_MODE="${AUTH_PROOF_MODE:-none}"
ROUTE_NAME="${ROUTE_NAME:-}"
ROUTE_HOST="${ROUTE_HOST:-}"
ROUTE_TIMEOUT_SECONDS="${ROUTE_TIMEOUT_SECONDS:-300}"
TLS_SECRET_NAME="${TLS_SECRET_NAME:-nifi-tls}"
START_EPOCH="$(date +%s)"
APPS_DOMAIN="${APPS_DOMAIN:-}"

KEYCLOAK_DEPLOYMENT="${KEYCLOAK_DEPLOYMENT:-keycloak}"
KEYCLOAK_SERVICE_NAME="${KEYCLOAK_SERVICE_NAME:-keycloak}"
KEYCLOAK_ROUTE_NAME="${KEYCLOAK_ROUTE_NAME:-keycloak}"
KEYCLOAK_ROUTE_HOST="${KEYCLOAK_ROUTE_HOST:-}"
KEYCLOAK_ROUTE_SECRET_NAME="${KEYCLOAK_ROUTE_SECRET_NAME:-keycloak-route-ca}"
KEYCLOAK_ADMIN_SECRET_NAME="${KEYCLOAK_ADMIN_SECRET_NAME:-keycloak-admin}"
OIDC_SECRET_NAME="${OIDC_SECRET_NAME:-nifi-oidc}"
OIDC_CLIENT_ID="${OIDC_CLIENT_ID:-nifi-fabric}"
OIDC_CLIENT_SECRET="${OIDC_CLIENT_SECRET:-ChangeMeChangeMe1!}"
KEYCLOAK_ADMIN_USER="${KEYCLOAK_ADMIN_USER:-admin}"
KEYCLOAK_ADMIN_PASSWORD="${KEYCLOAK_ADMIN_PASSWORD:-ChangeMeChangeMe1!}"
OIDC_ALICE_PASSWORD="${OIDC_ALICE_PASSWORD:-ChangeMeChangeMe1!}"
OIDC_BOB_PASSWORD="${OIDC_BOB_PASSWORD:-ChangeMeChangeMe1!}"
OIDC_DORA_PASSWORD="${OIDC_DORA_PASSWORD:-ChangeMeChangeMe1!}"
OIDC_VICTOR_PASSWORD="${OIDC_VICTOR_PASSWORD:-ChangeMeChangeMe1!}"
KEYCLOAK_IMAGE="${KEYCLOAK_IMAGE:-quay.io/keycloak/keycloak:26.1}"

LDAP_DEPLOYMENT="${LDAP_DEPLOYMENT:-ldap}"
LDAP_SERVICE_NAME="${LDAP_SERVICE_NAME:-ldap}"
LDAP_SERVICE_ACCOUNT="${LDAP_SERVICE_ACCOUNT:-ldap-proof}"
LDAP_BIND_SECRET_NAME="${LDAP_BIND_SECRET_NAME:-nifi-ldap-bind}"
LDAP_ADMIN_PASSWORD="${LDAP_ADMIN_PASSWORD:-ChangeMeChangeMe1!}"
LDAP_USER_PASSWORD="${LDAP_USER_PASSWORD:-ChangeMeChangeMe1!}"
LDAP_IMAGE="${LDAP_IMAGE:-docker.io/osixia/openldap:1.5.0}"

CONTROLLER_IMAGE_REPOSITORY="${CONTROLLER_IMAGE_REPOSITORY:-}"
CONTROLLER_IMAGE_TAG="${CONTROLLER_IMAGE_TAG:-}"
CONTROLLER_IMAGE_PULL_POLICY="${CONTROLLER_IMAGE_PULL_POLICY:-IfNotPresent}"
NIFI_IMAGE_REPOSITORY="${NIFI_IMAGE_REPOSITORY:-}"
NIFI_IMAGE_TAG="${NIFI_IMAGE_TAG:-}"
NIFI_IMAGE_PULL_POLICY="${NIFI_IMAGE_PULL_POLICY:-IfNotPresent}"
ROUTE_TLS_PROOF_SECRET="${ROUTE_TLS_PROOF_SECRET:-true}"

elapsed() {
  echo "$(( $(date +%s) - START_EPOCH ))"
}

resolve_nifi_resource_name() {
  if [[ -n "${NIFI_RESOURCE_NAME}" ]]; then
    return 0
  fi

  if [[ "${HELM_RELEASE}" == *"nifi"* ]]; then
    NIFI_RESOURCE_NAME="${HELM_RELEASE}"
  else
    NIFI_RESOURCE_NAME="${HELM_RELEASE}-nifi"
  fi
}

auth_proof_mode() {
  case "${AUTH_PROOF_MODE}" in
    none|oidc|ldap)
      printf '%s' "${AUTH_PROOF_MODE}"
      ;;
    *)
      echo "AUTH_PROOF_MODE must be one of: none, oidc, ldap" >&2
      return 1
      ;;
  esac
}

oidc_proof_enabled() {
  [[ "$(auth_proof_mode)" == "oidc" ]]
}

ldap_proof_enabled() {
  [[ "$(auth_proof_mode)" == "ldap" ]]
}

route_proof_enabled() {
  [[ "${ROUTE_PROOF}" == "true" || "$(auth_proof_mode)" != "none" ]]
}

controller_health_gate_required() {
  [[ "$(auth_proof_mode)" == "none" ]]
}

require_condition_true() {
  local type="$1"
  local actual
  resolve_nifi_resource_name
  actual="$("${KUBECTL}" -n "${NAMESPACE}" get nificluster "${NIFI_RESOURCE_NAME}" -o jsonpath="{.status.conditions[?(@.type==\"${type}\")].status}")"
  if [[ "${actual}" != "True" ]]; then
    echo "expected condition ${type}=True, got ${actual:-<empty>}" >&2
    return 1
  fi
}

wait_for_condition_true() {
  local type="$1"
  local timeout_seconds="${2:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    if require_condition_true "${type}" >/dev/null 2>&1; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      require_condition_true "${type}"
      return 1
    fi
    sleep 5
  done
}

check_openshift_prereqs() {
  require_command "${OC}"
  require_command "${KUBECTL}"
  require_command "${HELM}"
  require_command curl
  require_command jq
  require_command python3
  require_command base64
  require_command openssl
}

resolve_apps_domain() {
  if [[ -n "${APPS_DOMAIN}" ]]; then
    return 0
  fi

  APPS_DOMAIN="$("${OC}" get ingress.config.openshift.io cluster -o jsonpath='{.spec.domain}')"
  if [[ -z "${APPS_DOMAIN}" ]]; then
    echo "unable to determine the OpenShift apps domain" >&2
    return 1
  fi
}

resolve_route_host() {
  if ! route_proof_enabled; then
    return 0
  fi

  resolve_nifi_resource_name
  if [[ -z "${ROUTE_NAME}" ]]; then
    ROUTE_NAME="${NIFI_RESOURCE_NAME}"
  fi

  if [[ -n "${ROUTE_HOST}" ]]; then
    return 0
  fi

  resolve_apps_domain
  ROUTE_HOST="${ROUTE_NAME}-${NAMESPACE}.${APPS_DOMAIN}"
}

resolve_keycloak_route_host() {
  if ! oidc_proof_enabled; then
    return 0
  fi

  if [[ -n "${KEYCLOAK_ROUTE_HOST}" ]]; then
    return 0
  fi

  resolve_apps_domain
  KEYCLOAK_ROUTE_HOST="${KEYCLOAK_ROUTE_NAME}-${NAMESPACE}.${APPS_DOMAIN}"
}

route_tls_proof_secret_enabled() {
  [[ "${ROUTE_TLS_PROOF_SECRET}" == "true" ]]
}

curl_connect_host_for() {
  local host="$1"
  local resolved_host=""
  local wsl_gateway=""

  resolved_host="$(getent ahostsv4 "${host}" 2>/dev/null | awk 'NR==1{print $1}')"
  if [[ "${resolved_host}" == "127.0.0.1" ]]; then
    wsl_gateway="$(ip route show default 2>/dev/null | awk '/default/ {print $3; exit}')"
    if [[ -n "${wsl_gateway}" ]]; then
      echo "${wsl_gateway}"
      return 0
    fi
  fi

  if [[ -n "${resolved_host}" ]]; then
    echo "${resolved_host}"
  fi
}

route_curl_connect_host() {
  curl_connect_host_for "${ROUTE_HOST}"
}

cluster_keytool_truststore() {
  local temp_name="nifi-route-keytool-$$"
  local keytool_image=""

  if [[ -n "${NIFI_IMAGE_REPOSITORY}" && -n "${NIFI_IMAGE_TAG}" ]]; then
    keytool_image="${NIFI_IMAGE_REPOSITORY}:${NIFI_IMAGE_TAG}"
  elif [[ -n "${NIFI_IMAGE_REPOSITORY}" ]]; then
    keytool_image="${NIFI_IMAGE_REPOSITORY}"
  else
    keytool_image="docker.io/apache/nifi:${NIFI_IMAGE_TAG:-2.0.0}"
  fi

  "${KUBECTL}" -n "${NAMESPACE}" create configmap "${temp_name}" \
    --from-file=ca.crt="${1}" \
    --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null

  "${KUBECTL}" -n "${NAMESPACE}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${temp_name}
spec:
  restartPolicy: Never
  containers:
  - name: keytool
    image: ${keytool_image}
    imagePullPolicy: ${NIFI_IMAGE_PULL_POLICY}
    command:
    - /bin/sh
    - -ec
    - |
      keytool -importcert \
        -alias nifi-route-ca \
        -file /input/ca.crt \
        -keystore /output/truststore.p12 \
        -storetype PKCS12 \
        -storepass "${2}" \
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
    local phase=""
    phase="$("${KUBECTL}" -n "${NAMESPACE}" get pod "${temp_name}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    case "${phase}" in
      Running)
        if "${KUBECTL}" -n "${NAMESPACE}" exec "${temp_name}" -- test -f /output/.ready >/dev/null 2>&1; then
          break
        fi
        ;;
      Failed)
        "${KUBECTL}" -n "${NAMESPACE}" logs "${temp_name}" >&2 || true
        "${KUBECTL}" -n "${NAMESPACE}" delete pod "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
        "${KUBECTL}" -n "${NAMESPACE}" delete configmap "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
        return 1
        ;;
    esac
    if (( $(date +%s) >= deadline )); then
      "${KUBECTL}" -n "${NAMESPACE}" logs "${temp_name}" >&2 || true
      "${KUBECTL}" -n "${NAMESPACE}" delete pod "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
      "${KUBECTL}" -n "${NAMESPACE}" delete configmap "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
      echo "timed out waiting for Route proof truststore generation" >&2
      return 1
    fi
    sleep 2
  done

  "${KUBECTL}" -n "${NAMESPACE}" cp "${temp_name}:/output/truststore.p12" "${3}" >/dev/null
  "${KUBECTL}" -n "${NAMESPACE}" delete pod "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
  "${KUBECTL}" -n "${NAMESPACE}" delete configmap "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
}

prepare_route_tls_secret() {
  local tmpdir=""
  local secret_password=""
  local sensitive_props_key=""

  if ! route_proof_enabled || ! route_tls_proof_secret_enabled; then
    return 0
  fi

  phase "Preparing Route-compatible NiFi TLS Secret"

  secret_password="$("${KUBECTL}" -n "${NAMESPACE}" get secret "${TLS_SECRET_NAME}" -o jsonpath='{.data.keystorePassword}' 2>/dev/null | base64 -d || true)"
  sensitive_props_key="$("${KUBECTL}" -n "${NAMESPACE}" get secret "${TLS_SECRET_NAME}" -o jsonpath='{.data.sensitivePropsKey}' 2>/dev/null | base64 -d || true)"
  secret_password="${secret_password:-ChangeMeChangeMe1!}"
  sensitive_props_key="${sensitive_props_key:-changeit-change-me}"
  resolve_nifi_resource_name

  tmpdir="$(mktemp -d)"

  cat >"${tmpdir}/openssl.cnf" <<EOF
[ req ]
distinguished_name = dn
prompt = no
req_extensions = req_ext

[ dn ]
CN = ${NIFI_RESOURCE_NAME}
O = NiFi-Fabric

[ req_ext ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = ${NIFI_RESOURCE_NAME}
DNS.2 = ${NIFI_RESOURCE_NAME}.${NAMESPACE}.svc
DNS.3 = ${NIFI_RESOURCE_NAME}.${NAMESPACE}.svc.cluster.local
DNS.4 = ${NIFI_RESOURCE_NAME}-headless
DNS.5 = ${NIFI_RESOURCE_NAME}-headless.${NAMESPACE}.svc
DNS.6 = ${NIFI_RESOURCE_NAME}-headless.${NAMESPACE}.svc.cluster.local
DNS.7 = *.${NIFI_RESOURCE_NAME}-headless.${NAMESPACE}.svc
DNS.8 = *.${NIFI_RESOURCE_NAME}-headless.${NAMESPACE}.svc.cluster.local
DNS.9 = ${ROUTE_HOST}
EOF

  openssl genrsa -out "${tmpdir}/ca.key" 2048 >/dev/null 2>&1
  openssl req -x509 -new -nodes \
    -key "${tmpdir}/ca.key" \
    -sha256 \
    -days 365 \
    -subj "/CN=${NIFI_RESOURCE_NAME}-openshift-ca/O=NiFi-Fabric" \
    -out "${tmpdir}/ca.crt" >/dev/null 2>&1

  openssl genrsa -out "${tmpdir}/server.key" 2048 >/dev/null 2>&1
  openssl req -new \
    -key "${tmpdir}/server.key" \
    -out "${tmpdir}/server.csr" \
    -config "${tmpdir}/openssl.cnf" >/dev/null 2>&1

  openssl x509 -req \
    -in "${tmpdir}/server.csr" \
    -CA "${tmpdir}/ca.crt" \
    -CAkey "${tmpdir}/ca.key" \
    -CAcreateserial \
    -out "${tmpdir}/server.crt" \
    -days 365 \
    -sha256 \
    -extensions req_ext \
    -extfile "${tmpdir}/openssl.cnf" >/dev/null 2>&1

  openssl pkcs12 -export \
    -name nifi \
    -in "${tmpdir}/server.crt" \
    -inkey "${tmpdir}/server.key" \
    -certfile "${tmpdir}/ca.crt" \
    -out "${tmpdir}/keystore.p12" \
    -passout "pass:${secret_password}" >/dev/null 2>&1

  cluster_keytool_truststore "${tmpdir}/ca.crt" "${secret_password}" "${tmpdir}/truststore.p12"

  "${KUBECTL}" create namespace "${NAMESPACE}" --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null
  "${KUBECTL}" -n "${NAMESPACE}" create secret generic "${TLS_SECRET_NAME}" \
    --from-file=keystore.p12="${tmpdir}/keystore.p12" \
    --from-file=truststore.p12="${tmpdir}/truststore.p12" \
    --from-file=ca.crt="${tmpdir}/ca.crt" \
    --from-literal=keystorePassword="${secret_password}" \
    --from-literal=truststorePassword="${secret_password}" \
    --from-literal=sensitivePropsKey="${sensitive_props_key}" \
    --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null

  info "updated ${TLS_SECRET_NAME} with SAN for ${ROUTE_HOST}"
  rm -rf "${tmpdir}"
}

create_host_certificate_material() {
  local host="$1"
  local cn_label="$2"
  local output_dir="$3"

  cat >"${output_dir}/openssl.cnf" <<EOF
[req]
distinguished_name = dn
prompt = no
req_extensions = req_ext

[dn]
CN = ${host}
O = NiFi-Fabric

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = ${host}
EOF

  openssl genrsa -out "${output_dir}/ca.key" 2048 >/dev/null 2>&1
  openssl req -x509 -new -nodes \
    -key "${output_dir}/ca.key" \
    -sha256 \
    -days 365 \
    -subj "/CN=${cn_label}-ca/O=NiFi-Fabric" \
    -out "${output_dir}/ca.crt" >/dev/null 2>&1

  openssl genrsa -out "${output_dir}/server.key" 2048 >/dev/null 2>&1
  openssl req -new \
    -key "${output_dir}/server.key" \
    -out "${output_dir}/server.csr" \
    -config "${output_dir}/openssl.cnf" >/dev/null 2>&1

  openssl x509 -req \
    -in "${output_dir}/server.csr" \
    -CA "${output_dir}/ca.crt" \
    -CAkey "${output_dir}/ca.key" \
    -CAcreateserial \
    -out "${output_dir}/server.crt" \
    -days 365 \
    -sha256 \
    -extensions req_ext \
    -extfile "${output_dir}/openssl.cnf" >/dev/null 2>&1
}

wait_for_deployment_ready() {
  local namespace="$1"
  local deployment="$2"
  local timeout="${3:-600s}"

  "${KUBECTL}" -n "${namespace}" rollout status deployment/"${deployment}" --timeout="${timeout}" >/dev/null
}

wait_for_nifi_pod_ready() {
  local timeout_seconds="${1:-900}"
  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    local deletion_timestamp=""
    local host_ip=""
    local ready=""

    resolve_nifi_resource_name
    deletion_timestamp="$("${KUBECTL}" -n "${NAMESPACE}" get pod "${NIFI_RESOURCE_NAME}-0" -o jsonpath='{.metadata.deletionTimestamp}' 2>/dev/null || true)"
    host_ip="$("${KUBECTL}" -n "${NAMESPACE}" get pod "${NIFI_RESOURCE_NAME}-0" -o jsonpath='{.status.hostIP}' 2>/dev/null || true)"
    ready="$("${KUBECTL}" -n "${NAMESPACE}" get pod "${NIFI_RESOURCE_NAME}-0" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}' 2>/dev/null || true)"
    if [[ -z "${deletion_timestamp}" && -n "${host_ip}" && "${ready}" == "True" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for pod/${NIFI_RESOURCE_NAME}-0 readiness" >&2
      return 1
    fi
    sleep 5
  done
}

nifi_exec() {
  local attempt output rc

  for attempt in $(seq 1 24); do
    resolve_nifi_resource_name
    if output="$("${KUBECTL}" -n "${NAMESPACE}" exec -c nifi "${NIFI_RESOURCE_NAME}-0" -- "$@" 2>&1)"; then
      printf '%s' "${output}"
      return 0
    fi
    rc=$?
    if grep -Eq 'container not found|unable to upgrade connection|container .* is not running' <<<"${output}"; then
      sleep 5
      continue
    fi
    printf '%s\n' "${output}" >&2
    return "${rc}"
  done

  printf '%s\n' "${output}" >&2
  return 1
}

refresh_nifi_pods_for_auth_proof() {
  local timeout_seconds="${1:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  local old_uid=""
  local current_uid=""
  local deletion_timestamp=""
  local forced_delete="false"

  resolve_nifi_resource_name
  old_uid="$("${KUBECTL}" -n "${NAMESPACE}" get pod "${NIFI_RESOURCE_NAME}-0" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  "${KUBECTL}" -n "${NAMESPACE}" delete pod -l "app.kubernetes.io/name=nifi,app.kubernetes.io/instance=${HELM_RELEASE}" --wait=false >/dev/null 2>&1 || true

  if [[ -z "${old_uid}" ]]; then
    return 0
  fi

  while true; do
    current_uid="$("${KUBECTL}" -n "${NAMESPACE}" get pod "${NIFI_RESOURCE_NAME}-0" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
    deletion_timestamp="$("${KUBECTL}" -n "${NAMESPACE}" get pod "${NIFI_RESOURCE_NAME}-0" -o jsonpath='{.metadata.deletionTimestamp}' 2>/dev/null || true)"
    if [[ -n "${current_uid}" && "${current_uid}" != "${old_uid}" && -z "${deletion_timestamp}" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      if [[ "${forced_delete}" != "true" && "${current_uid}" == "${old_uid}" ]]; then
        info "force deleting stuck terminating pod/${NIFI_RESOURCE_NAME}-0 to continue the auth proof refresh"
        "${KUBECTL}" -n "${NAMESPACE}" delete pod "${NIFI_RESOURCE_NAME}-0" --force --grace-period=0 >/dev/null 2>&1 || true
        forced_delete="true"
        deadline=$(( $(date +%s) + 180 ))
      else
        echo "timed out waiting for pod/${NIFI_RESOURCE_NAME}-0 to refresh onto the auth proof template" >&2
        return 1
      fi
    fi
    sleep 5
  done
}

bootstrap_keycloak() {
  local tmpdir=""
  local cert=""
  local key=""
  local ca=""

  if ! oidc_proof_enabled; then
    return 0
  fi

  phase "Bootstrapping Keycloak for OpenShift OIDC proof"

  tmpdir="$(mktemp -d)"
  create_host_certificate_material "${KEYCLOAK_ROUTE_HOST}" "${KEYCLOAK_ROUTE_NAME}" "${tmpdir}"
  cert="$(<"${tmpdir}/server.crt")"
  key="$(<"${tmpdir}/server.key")"
  ca="$(<"${tmpdir}/ca.crt")"

  "${KUBECTL}" create namespace "${NAMESPACE}" --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null
  "${KUBECTL}" -n "${NAMESPACE}" create secret generic "${KEYCLOAK_ROUTE_SECRET_NAME}" \
    --from-file=ca.crt="${tmpdir}/ca.crt" \
    --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null
  "${KUBECTL}" -n "${NAMESPACE}" create secret generic "${KEYCLOAK_ADMIN_SECRET_NAME}" \
    --from-literal=username="${KEYCLOAK_ADMIN_USER}" \
    --from-literal=password="${KEYCLOAK_ADMIN_PASSWORD}" \
    --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null
  "${KUBECTL}" -n "${NAMESPACE}" create secret generic "${OIDC_SECRET_NAME}" \
    --from-literal=clientSecret="${OIDC_CLIENT_SECRET}" \
    --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null

  "${KUBECTL}" -n "${NAMESPACE}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: keycloak-realm
data:
  nifi-realm.json: |
    {
      "realm": "nifi",
      "enabled": true,
      "registrationAllowed": false,
      "defaultSignatureAlgorithm": "RS256",
      "clients": [
        {
          "clientId": "${OIDC_CLIENT_ID}",
          "name": "NiFi Fabric",
          "enabled": true,
          "protocol": "openid-connect",
          "publicClient": false,
          "secret": "${OIDC_CLIENT_SECRET}",
          "standardFlowEnabled": true,
          "directAccessGrantsEnabled": false,
          "attributes": {
            "id.token.signed.response.alg": "RS256",
            "access.token.signed.response.alg": "RS256"
          },
          "redirectUris": [
            "https://${ROUTE_HOST}/*",
            "https://${ROUTE_HOST}:443/*"
          ],
          "webOrigins": [
            "https://${ROUTE_HOST}",
            "https://${ROUTE_HOST}:443"
          ],
          "protocolMappers": [
            {
              "name": "groups",
              "protocol": "openid-connect",
              "protocolMapper": "oidc-group-membership-mapper",
              "consentRequired": false,
              "config": {
                "full.path": "false",
                "id.token.claim": "true",
                "access.token.claim": "true",
                "userinfo.token.claim": "true",
                "claim.name": "groups"
              }
            }
          ]
        }
      ],
      "groups": [
        { "name": "nifi-platform-admins" },
        { "name": "nifi-viewers" },
        { "name": "nifi-editors" },
        { "name": "nifi-version-managers" }
      ],
      "users": [
        {
          "username": "alice",
          "enabled": true,
          "emailVerified": true,
          "firstName": "Alice",
          "lastName": "Admin",
          "email": "alice@example.com",
          "credentials": [
            { "type": "password", "value": "${OIDC_ALICE_PASSWORD}", "temporary": false }
          ],
          "groups": [ "nifi-platform-admins" ]
        },
        {
          "username": "bob",
          "enabled": true,
          "emailVerified": true,
          "firstName": "Bob",
          "lastName": "Viewer",
          "email": "bob@example.com",
          "credentials": [
            { "type": "password", "value": "${OIDC_BOB_PASSWORD}", "temporary": false }
          ],
          "groups": [ "nifi-viewers" ]
        },
        {
          "username": "dora",
          "enabled": true,
          "emailVerified": true,
          "firstName": "Dora",
          "lastName": "Editor",
          "email": "dora@example.com",
          "credentials": [
            { "type": "password", "value": "${OIDC_DORA_PASSWORD}", "temporary": false }
          ],
          "groups": [ "nifi-editors" ]
        },
        {
          "username": "victor",
          "enabled": true,
          "emailVerified": true,
          "firstName": "Victor",
          "lastName": "VersionManager",
          "email": "victor@example.com",
          "credentials": [
            { "type": "password", "value": "${OIDC_VICTOR_PASSWORD}", "temporary": false }
          ],
          "groups": [ "nifi-version-managers" ]
        }
      ]
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${KEYCLOAK_DEPLOYMENT}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: keycloak
  template:
    metadata:
      labels:
        app: keycloak
    spec:
      enableServiceLinks: false
      containers:
      - name: keycloak
        image: ${KEYCLOAK_IMAGE}
        args:
        - start-dev
        - --import-realm
        - --http-enabled=true
        - --hostname-strict=false
        - --hostname=https://${KEYCLOAK_ROUTE_HOST}
        - --proxy-headers=xforwarded
        env:
        - name: KEYCLOAK_ADMIN
          valueFrom:
            secretKeyRef:
              name: ${KEYCLOAK_ADMIN_SECRET_NAME}
              key: username
        - name: KEYCLOAK_ADMIN_PASSWORD
          valueFrom:
            secretKeyRef:
              name: ${KEYCLOAK_ADMIN_SECRET_NAME}
              key: password
        ports:
        - name: http
          containerPort: 8080
        readinessProbe:
          httpGet:
            path: /realms/nifi/.well-known/openid-configuration
            port: http
          initialDelaySeconds: 20
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 30
        volumeMounts:
        - name: realm
          mountPath: /opt/keycloak/data/import
      volumes:
      - name: realm
        configMap:
          name: keycloak-realm
---
apiVersion: v1
kind: Service
metadata:
  name: ${KEYCLOAK_SERVICE_NAME}
spec:
  selector:
    app: keycloak
  ports:
  - name: http
    port: 8080
    targetPort: http
---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: ${KEYCLOAK_ROUTE_NAME}
spec:
  host: ${KEYCLOAK_ROUTE_HOST}
  to:
    kind: Service
    name: ${KEYCLOAK_SERVICE_NAME}
  port:
    targetPort: http
  tls:
    termination: edge
    insecureEdgeTerminationPolicy: Redirect
    certificate: |
$(sed 's/^/      /' "${tmpdir}/server.crt")
    key: |
$(sed 's/^/      /' "${tmpdir}/server.key")
EOF

  "${KUBECTL}" -n "${NAMESPACE}" rollout restart deployment/"${KEYCLOAK_DEPLOYMENT}" >/dev/null
  wait_for_deployment_ready "${NAMESPACE}" "${KEYCLOAK_DEPLOYMENT}" 10m
  rm -rf "${tmpdir}"
  info "Keycloak Route host: ${KEYCLOAK_ROUTE_HOST}"
}

bootstrap_ldap() {
  if ! ldap_proof_enabled; then
    return 0
  fi

  phase "Bootstrapping LDAP for OpenShift proof"
  "${KUBECTL}" create namespace "${NAMESPACE}" --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null
  "${KUBECTL}" -n "${NAMESPACE}" create serviceaccount "${LDAP_SERVICE_ACCOUNT}" --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null
  "${OC}" adm policy add-scc-to-user anyuid "system:serviceaccount:${NAMESPACE}:${LDAP_SERVICE_ACCOUNT}" >/dev/null
  "${KUBECTL}" -n "${NAMESPACE}" create secret generic "${LDAP_BIND_SECRET_NAME}" \
    --from-literal=managerDn='cn=admin,dc=example,dc=com' \
    --from-literal=managerPassword="${LDAP_ADMIN_PASSWORD}" \
    --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null
  "${KUBECTL}" -n "${NAMESPACE}" create secret generic "${AUTH_SECRET}" \
    --from-literal=username='alice' \
    --from-literal=password="${LDAP_USER_PASSWORD}" \
    --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null

  "${KUBECTL}" -n "${NAMESPACE}" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${LDAP_DEPLOYMENT}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ldap
  template:
    metadata:
      labels:
        app: ldap
    spec:
      serviceAccountName: ${LDAP_SERVICE_ACCOUNT}
      enableServiceLinks: false
      containers:
      - name: ldap
        image: ${LDAP_IMAGE}
        env:
        - name: LDAP_ORGANISATION
          value: NiFi Fabric
        - name: LDAP_DOMAIN
          value: example.com
        - name: LDAP_ADMIN_PASSWORD
          value: ${LDAP_ADMIN_PASSWORD}
        - name: LDAP_TLS
          value: "false"
        ports:
        - name: ldap
          containerPort: 389
        readinessProbe:
          tcpSocket:
            port: ldap
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 24
---
apiVersion: v1
kind: Service
metadata:
  name: ${LDAP_SERVICE_NAME}
spec:
  selector:
    app: ldap
  ports:
  - name: ldap
    port: 389
    targetPort: ldap
EOF

  wait_for_deployment_ready "${NAMESPACE}" "${LDAP_DEPLOYMENT}" 10m
  "${KUBECTL}" -n "${NAMESPACE}" exec deployment/"${LDAP_DEPLOYMENT}" -- sh -ec '
    cat >/tmp/nifi-seed.ldif <<'"'"'LDIF'"'"'
dn: ou=People,dc=example,dc=com
objectClass: organizationalUnit
ou: People

dn: ou=Groups,dc=example,dc=com
objectClass: organizationalUnit
ou: Groups

dn: uid=alice,ou=People,dc=example,dc=com
objectClass: inetOrgPerson
cn: Alice Admin
sn: Admin
uid: alice
mail: alice@example.com
userPassword: '"${LDAP_USER_PASSWORD}"'

dn: uid=bob,ou=People,dc=example,dc=com
objectClass: inetOrgPerson
cn: Bob Operator
sn: Operator
uid: bob
mail: bob@example.com
userPassword: '"${LDAP_USER_PASSWORD}"'

dn: uid=charlie,ou=People,dc=example,dc=com
objectClass: inetOrgPerson
cn: Charlie Viewer
sn: Viewer
uid: charlie
mail: charlie@example.com
userPassword: '"${LDAP_USER_PASSWORD}"'

dn: cn=nifi-platform-admins,ou=Groups,dc=example,dc=com
objectClass: groupOfNames
cn: nifi-platform-admins
member: uid=alice,ou=People,dc=example,dc=com

dn: cn=nifi-flow-operators,ou=Groups,dc=example,dc=com
objectClass: groupOfNames
cn: nifi-flow-operators
member: uid=bob,ou=People,dc=example,dc=com
LDIF
    ldapadd -x -H ldap://127.0.0.1:389 -D "cn=admin,dc=example,dc=com" -w "'"${LDAP_ADMIN_PASSWORD}"'" -f /tmp/nifi-seed.ldif || rc=$?
    if [ "${rc:-0}" -ne 0 ] && [ "${rc:-0}" -ne 68 ]; then
      exit "${rc}"
    fi
  '
}

print_project_state() {
  local namespace="$1"
  echo "-- namespace/project: ${namespace}"
  "${KUBECTL}" get namespace "${namespace}" -o wide || true
  "${OC}" get project "${namespace}" || true
  "${KUBECTL}" -n "${namespace}" get all || true
}

dump_route_diagnostics() {
  if ! route_proof_enabled; then
    return 0
  fi

  echo "-- Route diagnostics"
  "${KUBECTL}" -n "${NAMESPACE}" get route "${ROUTE_NAME}" -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get route "${ROUTE_NAME}" -o yaml || true
  "${KUBECTL}" -n "${NAMESPACE}" describe route "${ROUTE_NAME}" || true
  "${KUBECTL}" -n "${NAMESPACE}" get service "${ROUTE_NAME}" -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get endpointslice -l "kubernetes.io/service-name=${ROUTE_NAME}" -o wide || true
  resolve_nifi_resource_name
  "${KUBECTL}" -n "${NAMESPACE}" exec "${NIFI_RESOURCE_NAME}-0" -c nifi -- sh -ec \
    "grep '^nifi\\.web\\.https\\.host=' /opt/nifi/nifi-current/conf/nifi.properties; \
     grep '^nifi\\.web\\.https\\.port=' /opt/nifi/nifi-current/conf/nifi.properties; \
     grep '^nifi\\.web\\.proxy\\.host=' /opt/nifi/nifi-current/conf/nifi.properties" || true
}

dump_auth_helper_diagnostics() {
  if oidc_proof_enabled; then
    echo "-- OIDC helper diagnostics"
    "${KUBECTL}" -n "${NAMESPACE}" get deployment "${KEYCLOAK_DEPLOYMENT}" -o wide || true
    "${KUBECTL}" -n "${NAMESPACE}" get route "${KEYCLOAK_ROUTE_NAME}" -o wide || true
    "${KUBECTL}" -n "${NAMESPACE}" get route "${KEYCLOAK_ROUTE_NAME}" -o yaml || true
    "${KUBECTL}" -n "${NAMESPACE}" describe route "${KEYCLOAK_ROUTE_NAME}" || true
    "${KUBECTL}" -n "${NAMESPACE}" logs deployment/"${KEYCLOAK_DEPLOYMENT}" --tail=200 || true
  fi

  if ldap_proof_enabled; then
    echo "-- LDAP helper diagnostics"
    "${KUBECTL}" -n "${NAMESPACE}" get deployment "${LDAP_DEPLOYMENT}" -o wide || true
    "${KUBECTL}" -n "${NAMESPACE}" logs deployment/"${LDAP_DEPLOYMENT}" --tail=200 || true
  fi
}

require_specific_route_admitted() {
  local route_name="$1"
  local admitted

  admitted="$("${KUBECTL}" -n "${NAMESPACE}" get route "${route_name}" -o jsonpath='{.status.ingress[0].conditions[?(@.type=="Admitted")].status}')"
  if [[ "${admitted}" != "True" ]]; then
    echo "expected Route/${route_name} to be admitted, got ${admitted:-<empty>}" >&2
    return 1
  fi
}

wait_for_specific_route_admitted() {
  local route_name="$1"
  local timeout_seconds="${2:-300}"
  local deadline=$(( $(date +%s) + timeout_seconds ))

  while true; do
    if require_specific_route_admitted "${route_name}" >/dev/null 2>&1; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      require_specific_route_admitted "${route_name}"
      return 1
    fi
    sleep 5
  done
}

wait_for_route_admitted() {
  wait_for_specific_route_admitted "${ROUTE_NAME}" "${1:-300}"
}

verify_route_service_mapping() {
  local route_service=""
  local route_target_port=""
  local service_https_port=""

  route_service="$("${KUBECTL}" -n "${NAMESPACE}" get route "${ROUTE_NAME}" -o jsonpath='{.spec.to.name}')"
  route_target_port="$("${KUBECTL}" -n "${NAMESPACE}" get route "${ROUTE_NAME}" -o jsonpath='{.spec.port.targetPort}')"
  service_https_port="$("${KUBECTL}" -n "${NAMESPACE}" get service "${route_service}" -o jsonpath='{.spec.ports[?(@.name=="https")].port}')"

  if [[ "${route_service}" != "${ROUTE_NAME}" ]]; then
    echo "expected Route/${ROUTE_NAME} backend service ${ROUTE_NAME}, got ${route_service:-<empty>}" >&2
    return 1
  fi
  if [[ "${route_target_port}" != "https" ]]; then
    echo "expected Route/${ROUTE_NAME} targetPort=https, got ${route_target_port:-<empty>}" >&2
    return 1
  fi
  if [[ -z "${service_https_port}" ]]; then
    echo "Service/${route_service} does not expose a named https port" >&2
    return 1
  fi
}

verify_route_proxy_configuration() {
  local proxy_host_line=""

  resolve_nifi_resource_name
  proxy_host_line="$("${KUBECTL}" -n "${NAMESPACE}" exec "${NIFI_RESOURCE_NAME}-0" -c nifi -- sh -ec \
    "grep '^nifi\\.web\\.proxy\\.host=' /opt/nifi/nifi-current/conf/nifi.properties" || true)"
  if [[ "${proxy_host_line}" != *"${ROUTE_HOST}"* ]]; then
    echo "expected nifi.web.proxy.host to include ${ROUTE_HOST}, got ${proxy_host_line:-<empty>}" >&2
    return 1
  fi
}

write_route_ca_file() {
  local output_file="$1"
  "${KUBECTL}" -n "${NAMESPACE}" get secret "${TLS_SECRET_NAME}" -o jsonpath='{.data.ca\.crt}' | base64 -d >"${output_file}"
}

write_keycloak_ca_file() {
  local output_file="$1"
  "${KUBECTL}" -n "${NAMESPACE}" get secret "${KEYCLOAK_ROUTE_SECRET_NAME}" -o jsonpath='{.data.ca\.crt}' | base64 -d >"${output_file}"
}

route_resolve_args() {
  local connect_host=""
  connect_host="$(route_curl_connect_host || true)"
  if [[ -n "${connect_host}" ]]; then
    printf -- '--resolve\n%s\n' "${ROUTE_HOST}:443:${connect_host}"
  fi
}

oidc_resolve_args() {
  local route_connect_host=""
  local keycloak_connect_host=""

  route_connect_host="$(route_curl_connect_host || true)"
  keycloak_connect_host="$(curl_connect_host_for "${KEYCLOAK_ROUTE_HOST}" || true)"
  if [[ -n "${route_connect_host}" ]]; then
    printf -- '--resolve\n%s\n' "${ROUTE_HOST}:443:${route_connect_host}"
  fi
  if [[ -n "${keycloak_connect_host}" ]]; then
    printf -- '--resolve\n%s\n' "${KEYCLOAK_ROUTE_HOST}:443:${keycloak_connect_host}"
  fi
}

verify_route_browser_access() {
  local ca_file="$1"
  local browser_body=""
  local browser_status=""
  local -a route_connect_args=()

  browser_body="$(mktemp)"
  mapfile -t route_connect_args < <(route_resolve_args)

  browser_status="$(curl --silent --show-error --location \
    --output "${browser_body}" \
    --write-out '%{http_code}' \
    --cacert "${ca_file}" \
    "${route_connect_args[@]}" \
    "https://${ROUTE_HOST}/nifi/")"
  if [[ "${browser_status}" != "200" ]]; then
    echo "expected browser access through the Route to return HTTP 200, got ${browser_status}" >&2
    rm -f "${browser_body}"
    return 1
  fi
  if ! grep -qi "nifi" "${browser_body}"; then
    echo "expected browser response through the Route to look like a NiFi UI page" >&2
    rm -f "${browser_body}"
    return 1
  fi

  rm -f "${browser_body}"
}

fetch_route_token() {
  local username="$1"
  local password="$2"
  local ca_file="$3"
  local -a route_connect_args=()

  mapfile -t route_connect_args < <(route_resolve_args)
  curl --silent --show-error --fail \
    --cacert "${ca_file}" \
    -H 'Content-Type: application/x-www-form-urlencoded; charset=UTF-8' \
    "${route_connect_args[@]}" \
    --data-urlencode "username=${username}" \
    --data-urlencode "password=${password}" \
    "https://${ROUTE_HOST}/nifi-api/access/token"
}

route_api_status_with_token() {
  local token="$1"
  local path="$2"
  local ca_file="$3"
  local body_file="$4"
  local -a route_connect_args=()

  mapfile -t route_connect_args < <(route_resolve_args)
  curl --silent --show-error \
    --output "${body_file}" \
    --write-out '%{http_code}' \
    --cacert "${ca_file}" \
    "${route_connect_args[@]}" \
    -H "Authorization: Bearer ${token}" \
    "https://${ROUTE_HOST}${path}"
}

ldap_expect_api_status() {
  local username="$1"
  local password="$2"
  local path="$3"
  local expected="$4"
  local ca_file="$5"
  local attempt=""
  local token=""
  local body_file=""
  local code=""
  local last_code=""

  body_file="$(mktemp)"
  for attempt in $(seq 1 12); do
    token="$(fetch_route_token "${username}" "${password}" "${ca_file}")"
    code="$(route_api_status_with_token "${token}" "${path}" "${ca_file}" "${body_file}")"
    if [[ "${code}" == "${expected}" ]]; then
      rm -f "${body_file}"
      return 0
    fi
    last_code="${code}"
    sleep 5
  done

  rm -f "${body_file}"
  echo "expected ${expected} from ${path} for LDAP user ${username}, got ${last_code}" >&2
  return 1
}

ldap_fetch_api_body_with_retry() {
  local username="$1"
  local password="$2"
  local path="$3"
  local expected="$4"
  local ca_file="$5"
  local body_file="$6"
  local attempt=""
  local token=""
  local code=""
  local last_code=""

  for attempt in $(seq 1 12); do
    token="$(fetch_route_token "${username}" "${password}" "${ca_file}")"
    code="$(route_api_status_with_token "${token}" "${path}" "${ca_file}" "${body_file}")"
    if [[ "${code}" == "${expected}" ]]; then
      return 0
    fi
    last_code="${code}"
    sleep 5
  done

  echo "expected ${expected} from ${path} for LDAP user ${username}, got ${last_code}" >&2
  return 1
}

verify_route_external_access() {
  local ca_file=""
  local username=""
  local password=""
  local token=""
  local current_user_json=""
  local current_user_identity=""
  local body_file=""

  ca_file="$(mktemp)"
  body_file="$(mktemp)"
  write_route_ca_file "${ca_file}"
  username="$("${KUBECTL}" -n "${NAMESPACE}" get secret "${AUTH_SECRET}" -o jsonpath='{.data.username}' | base64 -d)"
  password="$("${KUBECTL}" -n "${NAMESPACE}" get secret "${AUTH_SECRET}" -o jsonpath='{.data.password}' | base64 -d)"

  verify_route_browser_access "${ca_file}"
  token="$(fetch_route_token "${username}" "${password}" "${ca_file}")"
  if [[ "$(route_api_status_with_token "${token}" "/nifi-api/flow/current-user" "${ca_file}" "${body_file}")" != "200" ]]; then
    echo "expected authenticated current-user identity through the Route" >&2
    rm -f "${ca_file}" "${body_file}"
    return 1
  fi
  current_user_json="$(<"${body_file}")"
  current_user_identity="$(jq -r '.identity // empty' <<<"${current_user_json}")"
  if [[ -z "${current_user_identity}" ]]; then
    echo "expected authenticated current-user identity through the Route, got empty payload" >&2
    rm -f "${ca_file}" "${body_file}"
    return 1
  fi

  rm -f "${ca_file}" "${body_file}"
}

wait_for_oidc_auth_config() {
  local deadline=$(( $(date +%s) + ROUTE_TIMEOUT_SECONDS ))
  local ca_file=""
  local body_file=""
  local -a resolve_args=()

  ca_file="$(mktemp)"
  body_file="$(mktemp)"
  write_route_ca_file "${ca_file}"
  mapfile -t resolve_args < <(route_resolve_args)

  while true; do
    if curl --silent --show-error --fail \
      --output "${body_file}" \
      --cacert "${ca_file}" \
      "${resolve_args[@]}" \
      "https://${ROUTE_HOST}/nifi-api/authentication/configuration" >/dev/null 2>&1; then
      if jq -e '.authenticationConfiguration.loginUri' <"${body_file}" >/dev/null 2>&1; then
        rm -f "${ca_file}" "${body_file}"
        return 0
      fi
    fi
    if (( $(date +%s) >= deadline )); then
      rm -f "${ca_file}" "${body_file}"
      echo "timed out waiting for OIDC authentication configuration through the Route" >&2
      return 1
    fi
    sleep 5
  done
}

verify_oidc_runtime_wiring() {
  local proxy_line=""

  phase "Verifying OIDC runtime wiring"
  proxy_line="$(nifi_exec sh -ec "grep '^nifi\\.web\\.proxy\\.host=' /opt/nifi/nifi-current/conf/nifi.properties" || true)"
  if [[ "${proxy_line}" != *"${ROUTE_HOST}"* ]]; then
    echo "expected NiFi proxy host to include ${ROUTE_HOST}, got ${proxy_line:-<empty>}" >&2
    return 1
  fi

  nifi_exec sh -ec "grep -Fqx 'nifi.security.user.oidc.discovery.url=http://keycloak.${NAMESPACE}.svc.cluster.local:8080/realms/nifi/.well-known/openid-configuration' /opt/nifi/nifi-current/conf/nifi.properties"
  nifi_exec sh -ec "grep -Fqx 'nifi.security.user.oidc.client.id=${OIDC_CLIENT_ID}' /opt/nifi/nifi-current/conf/nifi.properties"
  nifi_exec sh -ec "grep -Fqx 'nifi.security.user.oidc.claim.identifying.user=email' /opt/nifi/nifi-current/conf/nifi.properties"
  nifi_exec sh -ec "grep -Fqx 'nifi.security.user.oidc.claim.groups=groups' /opt/nifi/nifi-current/conf/nifi.properties"
  nifi_exec sh -ec "grep -Fq 'nifi-platform-admins' /opt/nifi/nifi-current/conf/users.xml"
  nifi_exec sh -ec "grep -Fq 'nifi-viewers' /opt/nifi/nifi-current/conf/users.xml"
  nifi_exec sh -ec "grep -Fq 'nifi-editors' /opt/nifi/nifi-current/conf/users.xml"
  nifi_exec sh -ec "grep -Fq 'nifi-version-managers' /opt/nifi/nifi-current/conf/users.xml"
}

oidc_login_and_get_cookiejar() {
  local username="$1"
  local password="$2"
  local cookiejar="$3"
  local combined_ca="$4"
  local attempt=""
  local tmpdir=""
  local auth_json=""
  local login_uri=""
  local login_page=""
  local login_page_url=""
  local form_json=""
  local form_action=""
  local final_url=""
  local last_error="OIDC login flow did not stabilize"
  local -a resolve_args=()
  local -a post_args=()

  mapfile -t resolve_args < <(oidc_resolve_args)
  for attempt in $(seq 1 20); do
    rm -f "${cookiejar}"
    : > "${cookiejar}"
    tmpdir="$(mktemp -d)"
    login_page="${tmpdir}/login.html"
    post_args=()

    if ! auth_json="$(curl --silent --show-error --fail \
      --cacert "${combined_ca}" \
      "${resolve_args[@]}" \
      "https://${ROUTE_HOST}/nifi-api/authentication/configuration" 2>&1)"; then
      last_error="${auth_json}"
      rm -rf "${tmpdir}"
      sleep 5
      continue
    fi
    login_uri="$(jq -r '.authenticationConfiguration.loginUri // empty' <<<"${auth_json}")"
    if [[ -z "${login_uri}" ]]; then
      last_error="NiFi did not advertise an OIDC login URI"
      rm -rf "${tmpdir}"
      sleep 5
      continue
    fi

    if ! login_page_url="$(curl --silent --show-error --fail --location \
      --write-out '%{url_effective}' \
      --cacert "${combined_ca}" \
      --cookie-jar "${cookiejar}" \
      --cookie "${cookiejar}" \
      "${resolve_args[@]}" \
      --output "${login_page}" \
      "${login_uri}" 2>&1)"; then
      last_error="${login_page_url}"
      rm -rf "${tmpdir}"
      sleep 5
      continue
    fi
    if [[ "${login_page_url}" != *"${KEYCLOAK_ROUTE_HOST}"* ]]; then
      last_error="expected OIDC login redirect to reach ${KEYCLOAK_ROUTE_HOST}, got ${login_page_url}"
      rm -rf "${tmpdir}"
      sleep 5
      continue
    fi
    if ! grep -q 'kc-form-login' "${login_page}" && [[ "${login_page_url}" != *"login-actions/authenticate"* ]]; then
      last_error="expected the OIDC login flow to reach the Keycloak login form, got ${login_page_url}"
      rm -rf "${tmpdir}"
      sleep 5
      continue
    fi

    form_json="$(python3 - "${login_page}" <<'PY'
import html
import json
import re
import sys

body = open(sys.argv[1], "r", encoding="utf-8", errors="ignore").read()
action_match = re.search(r'<form[^>]+id="kc-form-login"[^>]+action="([^"]+)"', body)
if not action_match:
    raise SystemExit("could not find Keycloak login form action")
fields = []
for match in re.finditer(r'<input[^>]*name="([^"]+)"[^>]*value="([^"]*)"', body):
    fields.append({"name": match.group(1), "value": html.unescape(match.group(2))})
print(json.dumps({"action": html.unescape(action_match.group(1)), "fields": fields}))
PY
)"
    form_action="$(jq -r '.action' <<<"${form_json}")"
    while IFS=$'\t' read -r name value; do
      if [[ "${name}" == "username" || "${name}" == "password" ]]; then
        continue
      fi
      post_args+=(--data-urlencode "${name}=${value}")
    done < <(jq -r '.fields[] | [.name, .value] | @tsv' <<<"${form_json}")
    post_args+=(--data-urlencode "username=${username}" --data-urlencode "password=${password}")

    if ! final_url="$(curl --silent --show-error --fail --location \
      --write-out '%{url_effective}' \
      --output "${tmpdir}/post-login.html" \
      --cacert "${combined_ca}" \
      --cookie-jar "${cookiejar}" \
      --cookie "${cookiejar}" \
      "${resolve_args[@]}" \
      "${post_args[@]}" \
      "${form_action}" 2>&1)"; then
      last_error="${final_url}"
      rm -rf "${tmpdir}"
      sleep 5
      continue
    fi
    rm -rf "${tmpdir}"

    if [[ "${final_url}" == https://${ROUTE_HOST}/* || "${final_url}" == https://${ROUTE_HOST}:443/* ]]; then
      return 0
    fi

    last_error="expected OIDC login to return to https://${ROUTE_HOST}/..., got ${final_url}"
    sleep 5
  done

  echo "${last_error}" >&2
  return 1
}

route_api_status_with_cookiejar() {
  local cookiejar="$1"
  local path="$2"
  local ca_file="$3"
  local body_file="$4"
  local -a resolve_args=()

  mapfile -t resolve_args < <(oidc_resolve_args)
  curl --silent --show-error \
    --output "${body_file}" \
    --write-out '%{http_code}' \
    --cacert "${ca_file}" \
    --cookie "${cookiejar}" \
    "${resolve_args[@]}" \
    "https://${ROUTE_HOST}${path}"
}

request_token_from_cookiejar() {
  local cookiejar="$1"
  awk '$6=="__Secure-Request-Token" {print $7; exit}' "${cookiejar}"
}

oidc_expect_api_status() {
  local username="$1"
  local password="$2"
  local path="$3"
  local expected="$4"
  local combined_ca="$5"
  local attempt=""
  local cookiejar=""
  local body_file=""
  local code=""
  local last_code=""

  for attempt in $(seq 1 6); do
    cookiejar="$(mktemp)"
    body_file="$(mktemp)"
    oidc_login_and_get_cookiejar "${username}" "${password}" "${cookiejar}" "${combined_ca}"
    code="$(route_api_status_with_cookiejar "${cookiejar}" "${path}" "${combined_ca}" "${body_file}")"
    rm -f "${cookiejar}" "${body_file}"
    if [[ "${code}" == "${expected}" ]]; then
      return 0
    fi
    last_code="${code}"
    sleep 3
  done

  echo "expected ${expected} from ${path} for ${username}, got ${last_code}" >&2
  return 1
}

oidc_fetch_api_body_with_retry() {
  local username="$1"
  local password="$2"
  local path="$3"
  local expected="$4"
  local combined_ca="$5"
  local body_file="$6"
  local attempt=""
  local cookiejar=""
  local code=""
  local last_code=""

  for attempt in $(seq 1 6); do
    cookiejar="$(mktemp)"
    : > "${body_file}"
    oidc_login_and_get_cookiejar "${username}" "${password}" "${cookiejar}" "${combined_ca}"
    code="$(route_api_status_with_cookiejar "${cookiejar}" "${path}" "${combined_ca}" "${body_file}")"
    rm -f "${cookiejar}"
    if [[ "${code}" == "${expected}" ]]; then
      return 0
    fi
    last_code="${code}"
    sleep 3
  done

  echo "expected ${expected} from ${path} for ${username}, got ${last_code}" >&2
  return 1
}

oidc_create_and_delete_root_child() {
  local username="$1"
  local password="$2"
  local combined_ca="$3"
  local proof_label="$4"
  local cookiejar=""
  local root_body=""
  local create_body=""
  local delete_body=""
  local root_id=""
  local pg_id=""
  local revision_version=""
  local client_id=""
  local request_token=""
  local code=""
  local payload=""
  local -a resolve_args=()

  cookiejar="$(mktemp)"
  root_body="$(mktemp)"
  create_body="$(mktemp)"
  delete_body="$(mktemp)"
  mapfile -t resolve_args < <(oidc_resolve_args)
  oidc_login_and_get_cookiejar "${username}" "${password}" "${cookiejar}" "${combined_ca}"
  request_token="$(request_token_from_cookiejar "${cookiejar}")"
  if [[ -z "${request_token}" ]]; then
    echo "expected OIDC login for ${username} to yield a NiFi Request-Token cookie" >&2
    rm -f "${cookiejar}" "${root_body}" "${create_body}" "${delete_body}"
    return 1
  fi

  code="$(route_api_status_with_cookiejar "${cookiejar}" "/nifi-api/flow/process-groups/root" "${combined_ca}" "${root_body}")"
  if [[ "${code}" != "200" ]]; then
    echo "expected ${username} to read the root process group, got ${code}" >&2
    rm -f "${cookiejar}" "${root_body}" "${create_body}" "${delete_body}"
    return 1
  fi
  root_id="$(jq -r '.processGroupFlow.id // empty' <"${root_body}")"
  if [[ -z "${root_id}" ]]; then
    echo "could not determine the root process group id for ${username}" >&2
    rm -f "${cookiejar}" "${root_body}" "${create_body}" "${delete_body}"
    return 1
  fi

  payload="$(jq -n --arg name "proof-${proof_label}" '{revision:{version:0},component:{name:$name,position:{x:0.0,y:0.0}}}')"
  code="$(curl --silent --show-error \
    --output "${create_body}" \
    --write-out '%{http_code}' \
    --cacert "${combined_ca}" \
    --cookie "${cookiejar}" \
    "${resolve_args[@]}" \
    -H 'Content-Type: application/json' \
    -H "Request-Token: ${request_token}" \
    --data "${payload}" \
    "https://${ROUTE_HOST}/nifi-api/process-groups/${root_id}/process-groups")"
  if [[ "${code}" != "201" && "${code}" != "200" ]]; then
    echo "expected ${username} to create a child process group, got ${code}" >&2
    rm -f "${cookiejar}" "${root_body}" "${create_body}" "${delete_body}"
    return 1
  fi

  pg_id="$(jq -r '.component.id // empty' <"${create_body}")"
  revision_version="$(jq -r '.revision.version // empty' <"${create_body}")"
  client_id="$(jq -r '.revision.clientId // empty' <"${create_body}")"
  code="$(curl --silent --show-error \
    --output "${delete_body}" \
    --write-out '%{http_code}' \
    --cacert "${combined_ca}" \
    --cookie "${cookiejar}" \
    "${resolve_args[@]}" \
    -X DELETE \
    -H "Request-Token: ${request_token}" \
    "https://${ROUTE_HOST}/nifi-api/process-groups/${pg_id}?version=${revision_version}&clientId=${client_id}&disconnectedNodeAcknowledged=false")"
  rm -f "${cookiejar}" "${root_body}" "${create_body}" "${delete_body}"
  if [[ "${code}" != "200" ]]; then
    echo "expected ${username} to delete the proof child process group, got ${code}" >&2
    return 1
  fi
}

verify_oidc_external_access() {
  local nifi_ca=""
  local keycloak_ca=""
  local combined_ca=""
  local cookiejar=""
  local groups_body=""

  phase "Verifying OIDC browser login and bundle behavior through Routes"
  nifi_ca="$(mktemp)"
  keycloak_ca="$(mktemp)"
  combined_ca="$(mktemp)"
  cookiejar="$(mktemp)"
  groups_body="$(mktemp)"

  write_route_ca_file "${nifi_ca}"
  write_keycloak_ca_file "${keycloak_ca}"
  cat "${nifi_ca}" "${keycloak_ca}" >"${combined_ca}"

  wait_for_oidc_auth_config
  oidc_expect_api_status "alice" "${OIDC_ALICE_PASSWORD}" "/nifi-api/tenants/user-groups" "200" "${combined_ca}"
  oidc_fetch_api_body_with_retry "alice" "${OIDC_ALICE_PASSWORD}" "/nifi-api/tenants/user-groups" "200" "${combined_ca}" "${groups_body}"
  if ! jq -e '.userGroups[]? | select(.component.identity=="nifi-platform-admins" or .component.identity=="nifi-viewers" or .component.identity=="nifi-editors" or .component.identity=="nifi-version-managers")' <"${groups_body}" >/dev/null 2>&1; then
    echo "expected seeded OIDC policy groups to be visible through /nifi-api/tenants/user-groups" >&2
    rm -f "${nifi_ca}" "${keycloak_ca}" "${combined_ca}" "${cookiejar}" "${groups_body}"
    return 1
  fi

  oidc_expect_api_status "alice" "${OIDC_ALICE_PASSWORD}" "/nifi-api/controller/config" "200" "${combined_ca}"
  oidc_expect_api_status "bob" "${OIDC_BOB_PASSWORD}" "/nifi-api/controller/config" "403" "${combined_ca}"
  oidc_expect_api_status "dora" "${OIDC_DORA_PASSWORD}" "/nifi-api/controller/config" "200" "${combined_ca}"
  oidc_expect_api_status "victor" "${OIDC_VICTOR_PASSWORD}" "/nifi-api/controller/config" "200" "${combined_ca}"
  oidc_create_and_delete_root_child "dora" "${OIDC_DORA_PASSWORD}" "${combined_ca}" "editor"
  oidc_create_and_delete_root_child "victor" "${OIDC_VICTOR_PASSWORD}" "${combined_ca}" "flow-version-manager"

  rm -f "${nifi_ca}" "${keycloak_ca}" "${combined_ca}" "${cookiejar}" "${groups_body}"
}

verify_ldap_runtime_wiring() {
  phase "Verifying LDAP runtime wiring"
  nifi_exec sh -ec '
    grep -q "ldap-provider" /opt/nifi/nifi-current/conf/login-identity-providers.xml
    grep -q "ldap-user-group-provider" /opt/nifi/nifi-current/conf/authorizers.xml
    grep -q "Initial Admin Identity\">alice<" /opt/nifi/nifi-current/conf/authorizers.xml
    grep -q "<property name=\"Identity Strategy\">USE_USERNAME</property>" /opt/nifi/nifi-current/conf/login-identity-providers.xml
  '
}

verify_ldap_external_access() {
  local ca_file=""

  phase "Verifying LDAP login and documented bootstrap-admin identity behavior through the Route"
  ca_file="$(mktemp)"
  write_route_ca_file "${ca_file}"

  verify_route_browser_access "${ca_file}"

  ldap_expect_api_status "alice" "${LDAP_USER_PASSWORD}" "/nifi-api/controller/config" "200" "${ca_file}"
  ldap_expect_api_status "bob" "${LDAP_USER_PASSWORD}" "/nifi-api/controller/config" "403" "${ca_file}"
  ldap_expect_api_status "charlie" "${LDAP_USER_PASSWORD}" "/nifi-api/flow/process-groups/root" "403" "${ca_file}"

  rm -f "${ca_file}"
}

verify_route_runtime() {
  if ! route_proof_enabled; then
    return 0
  fi

  phase "Verifying OpenShift Route runtime behavior"
  "${KUBECTL}" -n "${NAMESPACE}" get route "${ROUTE_NAME}" >/dev/null
  wait_for_route_admitted "${ROUTE_TIMEOUT_SECONDS}"
  verify_route_service_mapping
  verify_route_proxy_configuration

  if oidc_proof_enabled; then
    verify_oidc_runtime_wiring
    wait_for_specific_route_admitted "${KEYCLOAK_ROUTE_NAME}" "${ROUTE_TIMEOUT_SECONDS}"
    verify_oidc_external_access
    info "route host: ${ROUTE_HOST}"
    info "OIDC managed install, external Route login, and named admin/editor/flow-version-manager bundle behavior passed"
    return 0
  fi

  if ldap_proof_enabled; then
    verify_ldap_runtime_wiring
    verify_ldap_external_access
    info "route host: ${ROUTE_HOST}"
    info "LDAP managed install, external Route login, and documented bootstrap-admin identity path passed"
    return 0
  fi

  verify_route_external_access
  info "route host: ${ROUTE_HOST}"
  info "route admission, service mapping, proxy-host wiring, and secure browser/API access passed"
}

dump_diagnostics() {
  set +e
  local current_context=""
  echo
  echo "==> OpenShift platform managed diagnostics after failure at +$(elapsed)s"
  if ! current_context="$("${KUBECTL}" config current-context 2>/dev/null)"; then
    echo "No kube context is configured in this environment."
    echo "Set KUBECONFIG or log in with oc before rerunning the OpenShift proof."
    return
  fi
  echo "Current context: ${current_context}"
  "${OC}" whoami || true
  "${OC}" project -q || true
  "${HELM}" -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  "${KUBECTL}" get crd nificlusters.platform.nifi.io -o yaml || true
  print_project_state "${NAMESPACE}"
  print_project_state "${CONTROLLER_NAMESPACE}"
  resolve_nifi_resource_name
  "${KUBECTL}" -n "${NAMESPACE}" get nificluster "${NIFI_RESOURCE_NAME}" -o yaml || true
  "${KUBECTL}" -n "${NAMESPACE}" describe nificluster "${NIFI_RESOURCE_NAME}" || true
  "${KUBECTL}" -n "${NAMESPACE}" get statefulset "${NIFI_RESOURCE_NAME}" -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get pods -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get pvc -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get route -o wide || true
  dump_route_diagnostics
  dump_auth_helper_diagnostics
  "${KUBECTL}" -n "${NAMESPACE}" get nificluster "${NIFI_RESOURCE_NAME}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  "${KUBECTL}" -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  "${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" get deployment,pod -o wide || true
  "${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  "${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
}

print_openshift_failure_help() {
  cat <<EOF >&2

OpenShift managed proof failed.

Most useful debug commands:
  helm -n ${NAMESPACE} status ${HELM_RELEASE}
  kubectl -n ${NAMESPACE} get nificluster ${NIFI_RESOURCE_NAME} -o yaml
  kubectl -n ${NAMESPACE} get statefulset,pod,pvc
  kubectl -n ${NAMESPACE} get route ${ROUTE_NAME} -o yaml
  kubectl -n ${NAMESPACE} describe route ${ROUTE_NAME}
  kubectl -n ${NAMESPACE} get events --sort-by=.lastTimestamp | tail -n 50
  kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200
EOF
}

trap 'dump_diagnostics; print_openshift_failure_help; exit 1' ERR

helm_args=(
  upgrade
  --install
  "${HELM_RELEASE}"
  "${ROOT_DIR}/charts/nifi-platform"
  --namespace "${NAMESPACE}"
  --create-namespace
  -f "${ROOT_DIR}/${BASE_VALUES_FILE}"
  -f "${ROOT_DIR}/${OPENSHIFT_VALUES_FILE}"
)

if [[ "${CONTROLLER_NAMESPACE}" != "${NAMESPACE}" ]] && "${KUBECTL}" get namespace "${CONTROLLER_NAMESPACE}" >/dev/null 2>&1; then
  echo "Reusing existing controller namespace ${CONTROLLER_NAMESPACE}; disabling controller.namespace.create for this proof run."
  helm_args+=(--set controller.namespace.create=false)
fi

if [[ -n "${CONTROLLER_IMAGE_REPOSITORY}" ]]; then
  helm_args+=(--set "controller.image.repository=${CONTROLLER_IMAGE_REPOSITORY}")
fi
if [[ -n "${CONTROLLER_IMAGE_TAG}" ]]; then
  helm_args+=(--set "controller.image.tag=${CONTROLLER_IMAGE_TAG}")
fi
if [[ -n "${CONTROLLER_IMAGE_REPOSITORY}" || -n "${CONTROLLER_IMAGE_TAG}" ]]; then
  helm_args+=(--set "controller.image.pullPolicy=${CONTROLLER_IMAGE_PULL_POLICY}")
fi
if [[ -n "${NIFI_IMAGE_REPOSITORY}" ]]; then
  helm_args+=(--set "nifi.image.repository=${NIFI_IMAGE_REPOSITORY}")
fi
if [[ -n "${NIFI_IMAGE_TAG}" ]]; then
  helm_args+=(--set "nifi.image.tag=${NIFI_IMAGE_TAG}")
fi
if [[ -n "${NIFI_IMAGE_REPOSITORY}" || -n "${NIFI_IMAGE_TAG}" ]]; then
  helm_args+=(--set "nifi.image.pullPolicy=${NIFI_IMAGE_PULL_POLICY}")
fi
phase "Checking OpenShift managed proof prerequisites"
check_openshift_prereqs
auth_proof_mode >/dev/null
resolve_nifi_resource_name
if [[ -z "${ROUTE_NAME}" ]]; then
  ROUTE_NAME="${NIFI_RESOURCE_NAME}"
fi
if [[ -z "${NIFI_SERVICE_ACCOUNT}" ]]; then
  NIFI_SERVICE_ACCOUNT="${NIFI_RESOURCE_NAME}"
fi
"${KUBECTL}" config current-context >/dev/null
"${OC}" whoami >/dev/null

if route_proof_enabled || [[ "$(auth_proof_mode)" != "none" ]]; then
  "${KUBECTL}" create namespace "${NAMESPACE}" --dry-run=client -o yaml | "${KUBECTL}" apply -f - >/dev/null
fi

if route_proof_enabled; then
  resolve_route_host
  if oidc_proof_enabled; then
    resolve_keycloak_route_host
  fi
  prepare_route_tls_secret
  info "route proof enabled with host ${ROUTE_HOST}"
  helm_args+=(
    --set "nifi.openshift.route.enabled=true"
    --set-string "nifi.openshift.route.host=${ROUTE_HOST}"
    --set-string "nifi.openshift.route.tls.termination=passthrough"
    --set-string "nifi.openshift.route.tls.insecureEdgeTerminationPolicy=None"
    --set-string "nifi.web.proxyHosts[0]=${ROUTE_HOST}"
  )
fi

if oidc_proof_enabled; then
  bootstrap_keycloak
  helm_args+=(-f "${ROOT_DIR}/examples/openshift/oidc-managed-values.yaml")
  helm_args+=(--set-string "nifi.auth.oidc.discoveryUrl=http://keycloak.${NAMESPACE}.svc.cluster.local:8080/realms/nifi/.well-known/openid-configuration")
elif ldap_proof_enabled; then
  bootstrap_ldap
  helm_args+=(-f "${ROOT_DIR}/examples/openshift/ldap-managed-values.yaml")
  helm_args+=(--set-string "nifi.auth.ldap.url=ldap://ldap.${NAMESPACE}.svc.cluster.local:389")
fi

phase "Rendering and installing the OpenShift managed platform chart"
"${HELM}" dependency build "${ROOT_DIR}/charts/nifi-platform" >/dev/null
"${HELM}" "${helm_args[@]}"

phase "Applying the namespace-scoped OpenShift SCC prerequisite for NiFi"
"${OC}" adm policy add-scc-to-user anyuid "system:serviceaccount:${NAMESPACE}:${NIFI_SERVICE_ACCOUNT}" >/dev/null

phase "Verifying platform resources and controller rollout"
"${KUBECTL}" get crd nificlusters.platform.nifi.io >/dev/null
"${HELM}" -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
"${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=10m
"${KUBECTL}" -n "${NAMESPACE}" get nificluster "${NIFI_RESOURCE_NAME}" >/dev/null
"${KUBECTL}" -n "${NAMESPACE}" get statefulset "${NIFI_RESOURCE_NAME}" >/dev/null

if [[ "$(auth_proof_mode)" != "none" ]]; then
  phase "Refreshing NiFi pods onto the auth proof template"
  refresh_nifi_pods_for_auth_proof 180
fi

phase "Verifying secure NiFi health and controller management"
if oidc_proof_enabled || ldap_proof_enabled; then
  wait_for_nifi_pod_ready "${HEALTH_TIMEOUT_SECONDS}"
else
  bash "${ROOT_DIR}/hack/check-nifi-health.sh" \
    --namespace "${NAMESPACE}" \
    --statefulset "${NIFI_RESOURCE_NAME}" \
    --auth-secret "${AUTH_SECRET}" \
    --timeout "${HEALTH_TIMEOUT_SECONDS}"
fi
if controller_health_gate_required; then
  wait_for_condition_true TargetResolved "${HEALTH_TIMEOUT_SECONDS}"
  wait_for_condition_true Available "${HEALTH_TIMEOUT_SECONDS}"
else
  info "skipping controller Available/TargetResolved proof gate for ${AUTH_PROOF_MODE}; runtime auth proof uses pod readiness plus external Route checks"
fi
verify_route_runtime

if oidc_proof_enabled; then
  print_success_footer "OpenShift managed OIDC proof completed" \
    "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
    "kubectl -n ${NAMESPACE} get route ${ROUTE_NAME} -o yaml" \
    "kubectl -n ${NAMESPACE} get route ${KEYCLOAK_ROUTE_NAME} -o yaml" \
    "kubectl -n ${NAMESPACE} exec ${NIFI_RESOURCE_NAME}-0 -c nifi -- grep '^nifi\\.security\\.user\\.oidc' /opt/nifi/nifi-current/conf/nifi.properties" \
    "kubectl -n ${NAMESPACE} logs deployment/${KEYCLOAK_DEPLOYMENT} --tail=200" \
    "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
elif ldap_proof_enabled; then
  print_success_footer "OpenShift managed LDAP proof completed" \
    "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
    "kubectl -n ${NAMESPACE} get route ${ROUTE_NAME} -o yaml" \
    "kubectl -n ${NAMESPACE} exec ${NIFI_RESOURCE_NAME}-0 -c nifi -- grep 'ldap' /opt/nifi/nifi-current/conf/login-identity-providers.xml" \
    "kubectl -n ${NAMESPACE} logs deployment/${LDAP_DEPLOYMENT} --tail=200" \
    "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
elif route_proof_enabled; then
  print_success_footer "OpenShift managed platform Route proof completed" \
    "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
    "kubectl -n ${NAMESPACE} get route ${ROUTE_NAME} -o yaml" \
    "kubectl -n ${NAMESPACE} describe route ${ROUTE_NAME}" \
    "kubectl -n ${NAMESPACE} get statefulset,pod,pvc" \
    "kubectl -n ${NAMESPACE} exec ${NIFI_RESOURCE_NAME}-0 -c nifi -- grep '^nifi\\.web\\.proxy\\.host=' /opt/nifi/nifi-current/conf/nifi.properties" \
    "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
else
  print_success_footer "OpenShift managed platform baseline proof completed" \
    "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
    "kubectl -n ${NAMESPACE} get nificluster ${NIFI_RESOURCE_NAME} -o yaml" \
    "kubectl -n ${NAMESPACE} get statefulset,pod,pvc" \
    "kubectl -n ${NAMESPACE} get events --sort-by=.lastTimestamp | tail -n 50" \
    "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
fi
