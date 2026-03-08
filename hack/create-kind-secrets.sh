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
O = Nifi Made Simple

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
  -subj "/CN=${SERVICE_NAME}-kind-ca/O=Nifi Made Simple" \
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

if command -v keytool >/dev/null 2>&1; then
  keytool -importcert \
    -alias nifi-kind-ca \
    -file "${tmpdir}/ca.crt" \
    -keystore "${tmpdir}/truststore.p12" \
    -storetype PKCS12 \
    -storepass "${SECRET_PASSWORD}" \
    -noprompt >/dev/null 2>&1
else
  docker run --rm \
    -u "$(id -u):$(id -g)" \
    -e KEYSTORE_PASSWORD="${SECRET_PASSWORD}" \
    -v "${tmpdir}:/work" \
    --entrypoint /bin/sh \
    apache/nifi:2.0.0 \
    -ec 'keytool -importcert -alias nifi-kind-ca -file /work/ca.crt -keystore /work/truststore.p12 -storetype PKCS12 -storepass "${KEYSTORE_PASSWORD}" -noprompt >/dev/null 2>&1'
fi

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

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
