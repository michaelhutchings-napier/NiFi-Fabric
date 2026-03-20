#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${NAMESPACE:-nifi}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-nifi-registry}"
SERVICE_NAME="${SERVICE_NAME:-nifi-registry}"
IMAGE="${IMAGE:-apache/nifi-registry:1.28.0}"

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${DEPLOYMENT_NAME}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${DEPLOYMENT_NAME}
  template:
    metadata:
      labels:
        app: ${DEPLOYMENT_NAME}
    spec:
      containers:
      - name: nifi-registry
        image: ${IMAGE}
        imagePullPolicy: IfNotPresent
        ports:
        - name: http
          containerPort: 18080
        readinessProbe:
          httpGet:
            path: /nifi-registry
            port: http
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 5
          failureThreshold: 24
        livenessProbe:
          httpGet:
            path: /nifi-registry
            port: http
          initialDelaySeconds: 20
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 12
---
apiVersion: v1
kind: Service
metadata:
  name: ${SERVICE_NAME}
spec:
  selector:
    app: ${DEPLOYMENT_NAME}
  ports:
  - name: http
    port: 18080
    targetPort: http
EOF

kubectl -n "${NAMESPACE}" rollout status deployment/"${DEPLOYMENT_NAME}" --timeout=5m
