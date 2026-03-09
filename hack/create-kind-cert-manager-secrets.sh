#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${1:-nifi}"
AUTH_SECRET_NAME="${2:-nifi-auth}"
TLS_PARAMS_SECRET_NAME="${3:-nifi-tls-params}"
ADMIN_USERNAME="${NIFI_KIND_USERNAME:-admin}"
ADMIN_PASSWORD="${NIFI_KIND_PASSWORD:-ChangeMeChangeMe1!}"
PKCS12_PASSWORD="${NIFI_KIND_KEYSTORE_PASSWORD:-ChangeMeChangeMe1!}"
SENSITIVE_PROPS_KEY="${NIFI_SENSITIVE_PROPS_KEY:-changeit-change-me}"

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

kubectl -n "${NAMESPACE}" create secret generic "${AUTH_SECRET_NAME}" \
  --from-literal=username="${ADMIN_USERNAME}" \
  --from-literal=password="${ADMIN_PASSWORD}" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "${NAMESPACE}" create secret generic "${TLS_PARAMS_SECRET_NAME}" \
  --from-literal=pkcs12Password="${PKCS12_PASSWORD}" \
  --from-literal=sensitivePropsKey="${SENSITIVE_PROPS_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Created auth Secret ${AUTH_SECRET_NAME} and TLS params Secret ${TLS_PARAMS_SECRET_NAME} in namespace ${NAMESPACE}"
