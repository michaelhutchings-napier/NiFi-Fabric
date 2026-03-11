#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${1:-nifi}"
RELEASE_NAME="${2:-nifi}"
TLS_SECRET_NAME="${3:-nifi-tls}"
AUTH_SECRET_NAME="${4:-nifi-auth}"
ADMIN_USERNAME="${NIFI_KIND_USERNAME:-admin}"
ADMIN_PASSWORD="${NIFI_KIND_PASSWORD:-ChangeMeChangeMe1!}"
SECRET_PASSWORD="${NIFI_KIND_KEYSTORE_PASSWORD:-ChangeMeChangeMe1!}"
SENSITIVE_PROPS_KEY="${NIFI_SENSITIVE_PROPS_KEY:-changeit-change-me}"
KEYTOOL_IMAGE="${NIFI_KIND_KEYTOOL_IMAGE:-${NIFI_IMAGE:-apache/nifi:2.0.0}}"
SERVICE_NAME="${RELEASE_NAME}"
HEADLESS_NAME="${RELEASE_NAME}-headless"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

cat >"${tmpdir}/openssl.cnf" <<EOF
[ req ]
distinguished_name = dn
prompt = no
req_extensions = req_ext

[ dn ]
CN = ${SERVICE_NAME}
O = NiFi-Fabric

[ req_ext ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = ${SERVICE_NAME}
DNS.2 = ${SERVICE_NAME}.${NAMESPACE}.svc
DNS.3 = ${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local
DNS.4 = ${HEADLESS_NAME}
DNS.5 = ${HEADLESS_NAME}.${NAMESPACE}.svc
DNS.6 = ${HEADLESS_NAME}.${NAMESPACE}.svc.cluster.local
DNS.7 = *.${HEADLESS_NAME}.${NAMESPACE}.svc
DNS.8 = *.${HEADLESS_NAME}.${NAMESPACE}.svc.cluster.local
EOF

openssl genrsa -out "${tmpdir}/ca.key" 2048 >/dev/null 2>&1
openssl req -x509 -new -nodes \
  -key "${tmpdir}/ca.key" \
  -sha256 \
  -days 365 \
  -subj "/CN=${SERVICE_NAME}-kind-ca/O=NiFi-Fabric" \
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
  -passout "pass:${SECRET_PASSWORD}" >/dev/null 2>&1

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

cluster_keytool_truststore() {
  local temp_name="nifi-kind-keytool-$$"

  kubectl -n "${NAMESPACE}" create configmap "${temp_name}" \
    --from-file=ca.crt="${tmpdir}/ca.crt" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  kubectl -n "${NAMESPACE}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${temp_name}
spec:
  restartPolicy: Never
  containers:
  - name: keytool
    image: ${KEYTOOL_IMAGE}
    imagePullPolicy: IfNotPresent
    command:
    - /bin/sh
    - -ec
    - |
      keytool -importcert \
        -alias nifi-kind-ca \
        -file /input/ca.crt \
        -keystore /output/truststore.p12 \
        -storetype PKCS12 \
        -storepass "${SECRET_PASSWORD}" \
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
    phase="$(kubectl -n "${NAMESPACE}" get pod "${temp_name}" -o jsonpath='{.status.phase}')"
    case "${phase}" in
      Running)
        if kubectl -n "${NAMESPACE}" exec "${temp_name}" -- test -f /output/.ready >/dev/null 2>&1; then
          break
        fi
        ;;
      Failed)
        kubectl -n "${NAMESPACE}" logs "${temp_name}" >&2 || true
        kubectl -n "${NAMESPACE}" delete pod "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
        kubectl -n "${NAMESPACE}" delete configmap "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
        return 1
        ;;
    esac
    if (( $(date +%s) >= deadline )); then
      kubectl -n "${NAMESPACE}" logs "${temp_name}" >&2 || true
      kubectl -n "${NAMESPACE}" delete pod "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
      kubectl -n "${NAMESPACE}" delete configmap "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
      echo "timed out waiting for in-cluster keytool truststore generation" >&2
      return 1
    fi
    sleep 2
  done

  kubectl -n "${NAMESPACE}" cp "${temp_name}:/output/truststore.p12" "${tmpdir}/truststore.p12" >/dev/null
  kubectl -n "${NAMESPACE}" delete pod "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n "${NAMESPACE}" delete configmap "${temp_name}" --ignore-not-found >/dev/null 2>&1 || true
}

if command -v keytool >/dev/null 2>&1; then
  keytool -importcert \
    -alias nifi-kind-ca \
    -file "${tmpdir}/ca.crt" \
    -keystore "${tmpdir}/truststore.p12" \
    -storetype PKCS12 \
    -storepass "${SECRET_PASSWORD}" \
    -noprompt >/dev/null 2>&1
else
  cluster_keytool_truststore
fi

kubectl -n "${NAMESPACE}" create secret generic "${TLS_SECRET_NAME}" \
  --from-file=keystore.p12="${tmpdir}/keystore.p12" \
  --from-file=truststore.p12="${tmpdir}/truststore.p12" \
  --from-file=ca.crt="${tmpdir}/ca.crt" \
  --from-literal=keystorePassword="${SECRET_PASSWORD}" \
  --from-literal=truststorePassword="${SECRET_PASSWORD}" \
  --from-literal=sensitivePropsKey="${SENSITIVE_PROPS_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "${NAMESPACE}" create secret generic "${AUTH_SECRET_NAME}" \
  --from-literal=username="${ADMIN_USERNAME}" \
  --from-literal=password="${ADMIN_PASSWORD}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Created TLS Secret ${TLS_SECRET_NAME} and auth Secret ${AUTH_SECRET_NAME} in namespace ${NAMESPACE}"
echo "Username: ${ADMIN_USERNAME}"
echo "Password: ${ADMIN_PASSWORD}"
