#!/usr/bin/env bash

set -euo pipefail

KUBECTL="${KUBECTL:-kubectl}"
NAMESPACE="${NAMESPACE:-nifi}"
RELEASE="${RELEASE:-nifi}"
STATEFULSET_NAME="${STATEFULSET_NAME:-}"
CLUSTER_NAME="${CLUSTER_NAME:-}"
MANAGED="${MANAGED:-auto}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-nifi-controller-manager}"
AUTH_SECRET="${AUTH_SECRET:-nifi-auth}"
TLS_SECRET="${TLS_SECRET:-nifi-tls}"
TLS_PARAMS_SECRET="${TLS_PARAMS_SECRET:-}"
CERTIFICATE_NAME="${CERTIFICATE_NAME:-}"
SERVICE_NAME="${SERVICE_NAME:-}"

FAILURES=0
WARNINGS=0

usage() {
  cat <<'EOF'
Usage: hack/first-day-check.sh [--namespace ns] [--release name] [--statefulset name] [--cluster-name name]
                               [--managed auto|true|false]
                               [--controller-namespace ns] [--controller-deployment name]
                               [--service name]
                               [--auth-secret name] [--tls-secret name]
                               [--tls-params-secret name] [--certificate name]

Runs a lightweight day-1 check for a NiFi-Fabric install and prints a short pass/fail summary.

Examples:
  bash hack/first-day-check.sh --namespace nifi --release nifi --statefulset nifi --cluster-name nifi --managed true \
    --controller-namespace nifi-system --service nifi --tls-params-secret nifi-tls-params --certificate nifi

  make first-day-check NAMESPACE=nifi HELM_RELEASE=nifi STATEFULSET_NAME=nifi CLUSTER_NAME=nifi MANAGED=true \
    CONTROLLER_NAMESPACE=nifi-system SERVICE_NAME=nifi TLS_PARAMS_SECRET=nifi-tls-params CERTIFICATE=nifi
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

resource_type_available() {
  local name="$1"
  "${KUBECTL}" api-resources --verbs=list -o name 2>/dev/null | grep -Fxq "${name}"
}

print_pass() {
  printf 'PASS  %s\n' "$1"
}

print_fail() {
  printf 'FAIL  %s\n' "$1"
  FAILURES=$((FAILURES + 1))
}

print_warn() {
  printf 'WARN  %s\n' "$1"
  WARNINGS=$((WARNINGS + 1))
}

bool_from_string() {
  case "$1" in
    true|false)
      printf '%s' "$1"
      ;;
    *)
      printf 'false'
      ;;
  esac
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="$2"
      shift 2
      ;;
    --release)
      RELEASE="$2"
      shift 2
      ;;
    --statefulset)
      STATEFULSET_NAME="$2"
      shift 2
      ;;
    --cluster-name)
      CLUSTER_NAME="$2"
      shift 2
      ;;
    --managed)
      MANAGED="$2"
      shift 2
      ;;
    --controller-namespace)
      CONTROLLER_NAMESPACE="$2"
      shift 2
      ;;
    --controller-deployment)
      CONTROLLER_DEPLOYMENT="$2"
      shift 2
      ;;
    --service)
      SERVICE_NAME="$2"
      shift 2
      ;;
    --auth-secret)
      AUTH_SECRET="$2"
      shift 2
      ;;
    --tls-secret)
      TLS_SECRET="$2"
      shift 2
      ;;
    --tls-params-secret)
      TLS_PARAMS_SECRET="$2"
      shift 2
      ;;
    --certificate)
      CERTIFICATE_NAME="$2"
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

require_command "${KUBECTL}"

if [[ -z "${STATEFULSET_NAME}" ]]; then
  STATEFULSET_NAME="${RELEASE}"
fi
if [[ -z "${CLUSTER_NAME}" ]]; then
  CLUSTER_NAME="${RELEASE}"
fi

if [[ "${MANAGED}" == "auto" ]]; then
  if resource_type_available "nificlusters.platform.nifi.io" && "${KUBECTL}" -n "${NAMESPACE}" get nificluster "${CLUSTER_NAME}" >/dev/null 2>&1; then
    MANAGED="true"
  else
    MANAGED="false"
  fi
fi
MANAGED="$(bool_from_string "${MANAGED}")"

if [[ -z "${CERTIFICATE_NAME}" ]] && resource_type_available "certificates.cert-manager.io"; then
  if "${KUBECTL}" -n "${NAMESPACE}" get certificate "${RELEASE}" >/dev/null 2>&1; then
    CERTIFICATE_NAME="${RELEASE}"
  fi
fi

echo "First-day check for release ${RELEASE} in namespace ${NAMESPACE}"
echo "Managed mode: ${MANAGED}"
echo

if "${KUBECTL}" -n "${NAMESPACE}" get statefulset "${STATEFULSET_NAME}" >/dev/null 2>&1; then
  desired_replicas="$("${KUBECTL}" -n "${NAMESPACE}" get statefulset "${STATEFULSET_NAME}" -o jsonpath='{.spec.replicas}')"
  ready_replicas="$("${KUBECTL}" -n "${NAMESPACE}" get statefulset "${STATEFULSET_NAME}" -o jsonpath='{.status.readyReplicas}')"
  service_name="$("${KUBECTL}" -n "${NAMESPACE}" get statefulset "${STATEFULSET_NAME}" -o jsonpath='{.spec.serviceName}')"
  if [[ -n "${SERVICE_NAME}" ]]; then
    service_name="${SERVICE_NAME}"
  fi
  ready_replicas="${ready_replicas:-0}"
  if [[ "${ready_replicas}" == "${desired_replicas}" ]]; then
    print_pass "StatefulSet/${STATEFULSET_NAME} is Ready (${ready_replicas}/${desired_replicas})"
  else
    print_fail "StatefulSet/${STATEFULSET_NAME} is not Ready yet (${ready_replicas}/${desired_replicas})"
  fi
  if [[ -n "${service_name}" ]] && "${KUBECTL}" -n "${NAMESPACE}" get service "${service_name}" >/dev/null 2>&1; then
    print_pass "Service/${service_name} exists"
  else
    print_fail "The headless Service for StatefulSet/${STATEFULSET_NAME} is missing"
  fi
else
  print_fail "StatefulSet/${STATEFULSET_NAME} was not found"
fi

if [[ "${MANAGED}" == "true" ]]; then
  if resource_type_available "nificlusters.platform.nifi.io"; then
    if "${KUBECTL}" -n "${NAMESPACE}" get nificluster "${CLUSTER_NAME}" >/dev/null 2>&1; then
      available_status="$("${KUBECTL}" -n "${NAMESPACE}" get nificluster "${CLUSTER_NAME}" -o jsonpath='{range .status.conditions[?(@.type=="Available")]}{.status}{end}')"
      available_reason="$("${KUBECTL}" -n "${NAMESPACE}" get nificluster "${CLUSTER_NAME}" -o jsonpath='{range .status.conditions[?(@.type=="Available")]}{.reason}{end}')"
      available_reason="${available_reason:-unknown}"
      if [[ "${available_status}" == "True" ]]; then
        print_pass "NiFiCluster/${CLUSTER_NAME} reports Available=True (${available_reason})"
      else
        print_fail "NiFiCluster/${CLUSTER_NAME} is not Available yet (${available_reason})"
      fi
    else
      print_fail "NiFiCluster/${CLUSTER_NAME} was not found"
    fi
  else
    print_fail "NiFiCluster CRD is not installed"
  fi

  if "${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" get deployment "${CONTROLLER_DEPLOYMENT}" >/dev/null 2>&1; then
    controller_ready="$("${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" get deployment "${CONTROLLER_DEPLOYMENT}" -o jsonpath='{.status.readyReplicas}')"
    controller_desired="$("${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" get deployment "${CONTROLLER_DEPLOYMENT}" -o jsonpath='{.status.replicas}')"
    controller_ready="${controller_ready:-0}"
    controller_desired="${controller_desired:-0}"
    if [[ "${controller_ready}" == "${controller_desired}" && "${controller_desired}" != "0" ]]; then
      print_pass "Deployment/${CONTROLLER_DEPLOYMENT} is Ready in namespace ${CONTROLLER_NAMESPACE}"
    else
      print_fail "Deployment/${CONTROLLER_DEPLOYMENT} is not Ready in namespace ${CONTROLLER_NAMESPACE} (${controller_ready}/${controller_desired})"
    fi
  else
    print_fail "Deployment/${CONTROLLER_DEPLOYMENT} was not found in namespace ${CONTROLLER_NAMESPACE}"
  fi
fi

if [[ -n "${TLS_SECRET}" ]]; then
  if "${KUBECTL}" -n "${NAMESPACE}" get secret "${TLS_SECRET}" >/dev/null 2>&1; then
    print_pass "Secret/${TLS_SECRET} exists"
  else
    print_fail "Secret/${TLS_SECRET} was not found"
  fi
fi

if [[ -n "${TLS_PARAMS_SECRET}" ]]; then
  if "${KUBECTL}" -n "${NAMESPACE}" get secret "${TLS_PARAMS_SECRET}" >/dev/null 2>&1; then
    print_pass "Secret/${TLS_PARAMS_SECRET} exists"
  else
    print_fail "Secret/${TLS_PARAMS_SECRET} was not found"
  fi
fi

if [[ -n "${AUTH_SECRET}" ]]; then
  if "${KUBECTL}" -n "${NAMESPACE}" get secret "${AUTH_SECRET}" >/dev/null 2>&1; then
    print_pass "Secret/${AUTH_SECRET} exists"
  else
    print_fail "Secret/${AUTH_SECRET} was not found"
  fi
fi

if [[ -n "${CERTIFICATE_NAME}" ]]; then
  if resource_type_available "certificates.cert-manager.io"; then
    if "${KUBECTL}" -n "${NAMESPACE}" get certificate "${CERTIFICATE_NAME}" >/dev/null 2>&1; then
      certificate_ready="$("${KUBECTL}" -n "${NAMESPACE}" get certificate "${CERTIFICATE_NAME}" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}')"
      if [[ "${certificate_ready}" == "True" ]]; then
        print_pass "Certificate/${CERTIFICATE_NAME} is Ready"
      else
        print_fail "Certificate/${CERTIFICATE_NAME} is not Ready yet"
      fi
    else
      print_fail "Certificate/${CERTIFICATE_NAME} was not found"
    fi
  else
    print_warn "cert-manager CRDs are not installed, so Certificate/${CERTIFICATE_NAME} could not be checked"
  fi
fi

echo
echo "Recommended next commands:"
echo "  kubectl -n ${NAMESPACE} get statefulset,pods,svc,secret"
if [[ "${MANAGED}" == "true" ]]; then
  echo "  kubectl -n ${NAMESPACE} get nificluster ${CLUSTER_NAME} -o yaml"
  echo "  kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=100"
fi
if [[ -n "${AUTH_SECRET}" ]]; then
  echo "  kubectl -n ${NAMESPACE} get secret ${AUTH_SECRET} -o jsonpath='{.data.username}' | base64 -d; echo"
  echo "  kubectl -n ${NAMESPACE} get secret ${AUTH_SECRET} -o jsonpath='{.data.password}' | base64 -d; echo"
fi
if [[ -n "${SERVICE_NAME}" ]]; then
  echo "  kubectl -n ${NAMESPACE} port-forward svc/${SERVICE_NAME} 8443:8443"
else
  echo "  kubectl -n ${NAMESPACE} port-forward svc/${RELEASE} 8443:8443"
fi

echo
if (( FAILURES == 0 )); then
  echo "PASS: first-day checks completed successfully"
  if (( WARNINGS > 0 )); then
    echo "Warnings: ${WARNINGS}"
  fi
  exit 0
fi

echo "FAIL: first-day checks found ${FAILURES} issue(s)"
if (( WARNINGS > 0 )); then
  echo "Warnings: ${WARNINGS}"
fi
exit 1
