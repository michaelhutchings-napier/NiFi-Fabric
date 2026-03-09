#!/usr/bin/env bash

set -euo pipefail

CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE:-cert-manager}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.19.2}"

if kubectl -n "${CERT_MANAGER_NAMESPACE}" get deployment cert-manager >/dev/null 2>&1 \
  && kubectl -n "${CERT_MANAGER_NAMESPACE}" get deployment cert-manager-webhook >/dev/null 2>&1 \
  && kubectl -n "${CERT_MANAGER_NAMESPACE}" get deployment cert-manager-cainjector >/dev/null 2>&1; then
  echo "cert-manager is already installed in namespace ${CERT_MANAGER_NAMESPACE}"
else
  kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
fi

kubectl rollout status -n "${CERT_MANAGER_NAMESPACE}" deployment/cert-manager --timeout=5m
kubectl rollout status -n "${CERT_MANAGER_NAMESPACE}" deployment/cert-manager-webhook --timeout=5m
kubectl rollout status -n "${CERT_MANAGER_NAMESPACE}" deployment/cert-manager-cainjector --timeout=5m
