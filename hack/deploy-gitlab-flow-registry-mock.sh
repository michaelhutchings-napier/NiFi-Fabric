#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAMESPACE="${NAMESPACE:-nifi}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-gitlab-mock}"
SERVICE_NAME="${SERVICE_NAME:-gitlab-mock}"
IMAGE="${IMAGE:-${NIFI_IMAGE:-apache/nifi:2.8.0}}"
CONFIG_SHA="$(sha256sum "${ROOT_DIR}/hack/gitlab-flow-registry-mock.py" | awk '{print $1}')"

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "${NAMESPACE}" create configmap "${DEPLOYMENT_NAME}" \
  --from-file=server.py="${ROOT_DIR}/hack/gitlab-flow-registry-mock.py" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

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
      annotations:
        checksum/config: ${CONFIG_SHA}
      labels:
        app: ${DEPLOYMENT_NAME}
    spec:
      containers:
      - name: mock
        image: ${IMAGE}
        imagePullPolicy: IfNotPresent
        command: ["python3", "/app/server.py"]
        ports:
        - containerPort: 8080
          name: http
        volumeMounts:
        - name: app
          mountPath: /app
      volumes:
      - name: app
        configMap:
          name: ${DEPLOYMENT_NAME}
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
    port: 8080
    targetPort: 8080
EOF

kubectl -n "${NAMESPACE}" rollout status deployment/"${DEPLOYMENT_NAME}" --timeout=3m
