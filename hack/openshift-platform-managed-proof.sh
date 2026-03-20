#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

OC="${OC:-oc}"
HELM="${HELM:-helm}"
KUBECTL="${KUBECTL:-kubectl}"

NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
AUTH_SECRET="${AUTH_SECRET:-nifi-auth}"
NIFI_SERVICE_ACCOUNT="${NIFI_SERVICE_ACCOUNT:-${HELM_RELEASE}}"
BASE_VALUES_FILE="${BASE_VALUES_FILE:-examples/platform-managed-values.yaml}"
OPENSHIFT_VALUES_FILE="${OPENSHIFT_VALUES_FILE:-examples/openshift/managed-values.yaml}"
HEALTH_TIMEOUT_SECONDS="${HEALTH_TIMEOUT_SECONDS:-900}"
START_EPOCH="$(date +%s)"

CONTROLLER_IMAGE_REPOSITORY="${CONTROLLER_IMAGE_REPOSITORY:-}"
CONTROLLER_IMAGE_TAG="${CONTROLLER_IMAGE_TAG:-}"
CONTROLLER_IMAGE_PULL_POLICY="${CONTROLLER_IMAGE_PULL_POLICY:-IfNotPresent}"
NIFI_IMAGE_REPOSITORY="${NIFI_IMAGE_REPOSITORY:-}"
NIFI_IMAGE_TAG="${NIFI_IMAGE_TAG:-}"
NIFI_IMAGE_PULL_POLICY="${NIFI_IMAGE_PULL_POLICY:-IfNotPresent}"

elapsed() {
  echo "$(( $(date +%s) - START_EPOCH ))"
}

require_condition_true() {
  local type="$1"
  local actual
  actual="$("${KUBECTL}" -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath="{.status.conditions[?(@.type==\"${type}\")].status}")"
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
}

print_project_state() {
  local namespace="$1"
  echo "-- namespace/project: ${namespace}"
  "${KUBECTL}" get namespace "${namespace}" -o wide || true
  "${OC}" get project "${namespace}" || true
  "${KUBECTL}" -n "${namespace}" get all || true
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
  "${KUBECTL}" -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  "${KUBECTL}" -n "${NAMESPACE}" describe nificluster "${HELM_RELEASE}" || true
  "${KUBECTL}" -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get pods -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get pvc -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get route -o wide || true
  "${KUBECTL}" -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  "${KUBECTL}" -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  "${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" get deployment,pod -o wide || true
  "${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  "${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
}

print_openshift_failure_help() {
  cat <<EOF >&2

OpenShift managed baseline proof failed.

Most useful debug commands:
  helm -n ${NAMESPACE} status ${HELM_RELEASE}
  kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml
  kubectl -n ${NAMESPACE} get statefulset,pod,pvc
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
"${KUBECTL}" config current-context >/dev/null
"${OC}" whoami >/dev/null

phase "Rendering and installing the OpenShift managed platform chart"
"${HELM}" dependency build "${ROOT_DIR}/charts/nifi-platform" >/dev/null
"${HELM}" "${helm_args[@]}"

phase "Applying the namespace-scoped OpenShift SCC prerequisite for NiFi"
"${OC}" adm policy add-scc-to-user anyuid "system:serviceaccount:${NAMESPACE}:${NIFI_SERVICE_ACCOUNT}" >/dev/null

phase "Verifying platform resources and controller rollout"
"${KUBECTL}" get crd nificlusters.platform.nifi.io >/dev/null
"${HELM}" -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
"${KUBECTL}" -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=10m
"${KUBECTL}" -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
"${KUBECTL}" -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

phase "Verifying secure NiFi health and controller management"
bash "${ROOT_DIR}/hack/check-nifi-health.sh" \
  --namespace "${NAMESPACE}" \
  --statefulset "${HELM_RELEASE}" \
  --auth-secret "${AUTH_SECRET}" \
  --timeout "${HEALTH_TIMEOUT_SECONDS}"
wait_for_condition_true TargetResolved "${HEALTH_TIMEOUT_SECONDS}"
wait_for_condition_true Available "${HEALTH_TIMEOUT_SECONDS}"

print_success_footer "OpenShift managed platform baseline proof completed" \
  "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get statefulset,pod,pvc" \
  "kubectl -n ${NAMESPACE} get events --sort-by=.lastTimestamp | tail -n 50" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
