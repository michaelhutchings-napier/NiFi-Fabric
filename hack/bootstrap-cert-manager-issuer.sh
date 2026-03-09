#!/usr/bin/env bash

set -euo pipefail

CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE:-cert-manager}"
BOOTSTRAP_ISSUER_NAME="${BOOTSTRAP_ISSUER_NAME:-nifi-selfsigned-bootstrap}"
ROOT_CA_CERT_NAME="${ROOT_CA_CERT_NAME:-nifi-root-ca}"
CLUSTER_ISSUER_NAME="${CLUSTER_ISSUER_NAME:-nifi-ca}"

cat <<EOF | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: ${BOOTSTRAP_ISSUER_NAME}
  namespace: ${CERT_MANAGER_NAMESPACE}
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ${ROOT_CA_CERT_NAME}
  namespace: ${CERT_MANAGER_NAMESPACE}
spec:
  isCA: true
  commonName: ${ROOT_CA_CERT_NAME}
  secretName: ${ROOT_CA_CERT_NAME}
  duration: 8760h
  renewBefore: 720h
  issuerRef:
    name: ${BOOTSTRAP_ISSUER_NAME}
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: ${CLUSTER_ISSUER_NAME}
spec:
  ca:
    secretName: ${ROOT_CA_CERT_NAME}
EOF

kubectl wait -n "${CERT_MANAGER_NAMESPACE}" certificate/"${ROOT_CA_CERT_NAME}" --for=condition=Ready=True --timeout=5m
kubectl wait clusterissuer/"${CLUSTER_ISSUER_NAME}" --for=condition=Ready=True --timeout=5m
