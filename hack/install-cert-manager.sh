#!/usr/bin/env bash

set -euo pipefail

CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE:-cert-manager}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.19.2}"
CERT_MANAGER_RELEASE_NAME="${CERT_MANAGER_RELEASE_NAME:-cert-manager}"
CERT_MANAGER_REPO_NAME="${CERT_MANAGER_REPO_NAME:-jetstack}"
CERT_MANAGER_REPO_URL="${CERT_MANAGER_REPO_URL:-https://charts.jetstack.io}"

helm repo add "${CERT_MANAGER_REPO_NAME}" "${CERT_MANAGER_REPO_URL}" --force-update >/dev/null
helm repo update "${CERT_MANAGER_REPO_NAME}" >/dev/null

helm upgrade --install "${CERT_MANAGER_RELEASE_NAME}" "${CERT_MANAGER_REPO_NAME}/cert-manager" \
  --namespace "${CERT_MANAGER_NAMESPACE}" \
  --create-namespace \
  --version "${CERT_MANAGER_VERSION}" \
  --set crds.enabled=true \
  --wait \
  --timeout 10m

kubectl rollout status -n "${CERT_MANAGER_NAMESPACE}" deployment/cert-manager --timeout=5m
kubectl rollout status -n "${CERT_MANAGER_NAMESPACE}" deployment/cert-manager-webhook --timeout=5m
kubectl rollout status -n "${CERT_MANAGER_NAMESPACE}" deployment/cert-manager-cainjector --timeout=5m
