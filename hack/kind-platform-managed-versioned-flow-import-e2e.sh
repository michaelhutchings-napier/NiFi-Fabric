#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-platform-versioned-flow-import}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-${HELM_RELEASE}-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
PLATFORM_VALUES_FILE="${PLATFORM_VALUES_FILE:-examples/platform-managed-values.yaml}"
PLATFORM_FAST_VALUES_FILE="${PLATFORM_FAST_VALUES_FILE:-examples/platform-fast-values.yaml}"
FLOW_IMPORT_VALUES_FILE="${FLOW_IMPORT_VALUES_FILE:-examples/platform-managed-versioned-flow-import-values.yaml}"
FLOW_IMPORT_KIND_VALUES_FILE="${FLOW_IMPORT_KIND_VALUES_FILE:-examples/platform-managed-versioned-flow-import-kind-values.yaml}"
FLOW_REGISTRY_SECRET_NAME="${FLOW_REGISTRY_SECRET_NAME:-github-flow-registry}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"

run_make() {
  make -C "${ROOT_DIR}" "$@"
}

configure_kind_kubeconfig() {
  local kubeconfig_path="${TMPDIR:-/tmp}/${KIND_CLUSTER_NAME}.kubeconfig"
  kind get kubeconfig --name "${KIND_CLUSTER_NAME}" >"${kubeconfig_path}"
  export KUBECONFIG="${kubeconfig_path}"
}

dump_diagnostics() {
  set +e
  echo
  echo "==> versioned flow import runtime diagnostics after failure"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  helm -n "${NAMESPACE}" status "${HELM_RELEASE}" || true
  kubectl -n "${CONTROLLER_NAMESPACE}" get deployment,pod || true
  kubectl -n "${NAMESPACE}" get nificluster,statefulset,pod,secret,configmap || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 120 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs "${HELM_RELEASE}-0" -c nifi --tail=400 || true
  kubectl -n "${NAMESPACE}" logs deployment/github-mock --tail=200 || true
  kubectl -n "${NAMESPACE}" exec -i "${HELM_RELEASE}-0" -c nifi -- cat /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

helm_values_args=(
  -f "${ROOT_DIR}/${PLATFORM_VALUES_FILE}"
)

if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${PLATFORM_FAST_VALUES_FILE}")
fi

helm_values_args+=(
  -f "${ROOT_DIR}/${FLOW_IMPORT_VALUES_FILE}"
  -f "${ROOT_DIR}/${FLOW_IMPORT_KIND_VALUES_FILE}"
)

phase "Checking prerequisites"
check_prereqs
require_command curl
require_command jq
require_command python3

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for runtime-managed versioned flow import proof"
  configure_kind_kubeconfig
else
  phase "Creating fresh kind cluster for runtime-managed versioned flow import proof"
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
  configure_kind_kubeconfig

  phase "Loading NiFi image into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Creating TLS and auth Secrets"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
fi

phase "Creating GitHub Flow Registry token Secret"
kubectl -n "${NAMESPACE}" create secret generic "${FLOW_REGISTRY_SECRET_NAME}" \
  --from-literal=token=dummytoken \
  --dry-run=client -o yaml | kubectl apply -f -

phase "Deploying GitHub-compatible evaluator service"
NAMESPACE="${NAMESPACE}" NIFI_IMAGE="${NIFI_IMAGE}" bash "${ROOT_DIR}/hack/deploy-github-flow-registry-mock.sh"

phase "Installing product chart managed release without bounded flow import enabled yet"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}" \
  --set nifi.versionedFlowImports.enabled=false

phase "Verifying platform resources and controller rollout"
kubectl get crd nificlusters.platform.nifi.io >/dev/null
helm -n "${NAMESPACE}" status "${HELM_RELEASE}" >/dev/null
kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" >/dev/null
kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" >/dev/null

phase "Verifying initial cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

phase "Proving runtime-managed Parameter Context prerequisite"
parameter_context_expected_action="created"
if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  parameter_context_expected_action="created,unchanged,updated"
fi
bash "${ROOT_DIR}/hack/prove-parameter-contexts-runtime.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --auth-secret nifi-auth \
  --context-name payments-runtime \
  --expected-inline-parameter payments.api.baseUrl \
  --expected-inline-value https://payments.internal.example.com \
  --expected-sensitive-parameter "" \
  --expected-action "${parameter_context_expected_action}"

phase "Creating the live Flow Registry Client and seeding the selected versioned flow"
bash "${ROOT_DIR}/hack/prove-github-flow-registry-workflow.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --auth-secret nifi-auth \
  --client-name github-flows \
  --workflow-bucket team-a \
  --workflow-flow-name payments-api \
  --workflow-process-group-name "flow-import-seed-$(date +%s)"

phase "Enabling bounded runtime-managed versioned flow import"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi-platform" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

github_pat="$(kubectl -n "${NAMESPACE}" get secret "${FLOW_REGISTRY_SECRET_NAME}" -o jsonpath='{.data.token}' | base64 -d)"

phase "Restarting pod ${HELM_RELEASE}-0 so the upgraded bounded import bundle is mounted"
kubectl -n "${NAMESPACE}" delete pod "${HELM_RELEASE}-0" --wait=true
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

phase "Recreating the operator-owned live Flow Registry Client after pod restart"
bash "${ROOT_DIR}/hack/prove-github-flow-registry-client.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --auth-secret nifi-auth \
  --client-name github-flows \
  --expect-bucket team-a \
  --expect-bucket team-b

phase "Verifying bounded import bundle is mounted on pod ${HELM_RELEASE}-0"
kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- test -f /opt/nifi/fabric/versioned-flow-imports/config.json
kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- test -f /opt/nifi/fabric/versioned-flow-imports/bootstrap.py

phase "Running the bounded import bootstrap directly on pod ${HELM_RELEASE}-0"
kubectl -n "${NAMESPACE}" exec "${HELM_RELEASE}-0" -c nifi -- env \
  FLOW_REGISTRY_CLIENT_GITHUB_PAT_0="${github_pat}" \
  python3 /opt/nifi/fabric/versioned-flow-imports/bootstrap.py

phase "Proving runtime-managed bounded versioned flow import"
import_expected_action="created"
if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  import_expected_action="created,unchanged,updated"
fi
bash "${ROOT_DIR}/hack/prove-versioned-flow-import-runtime.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --auth-secret nifi-auth \
  --import-name payments-api \
  --expected-action "${import_expected_action}"

print_success_footer "platform chart runtime-managed versioned flow import proof completed" \
  "helm -n ${NAMESPACE} status ${HELM_RELEASE}" \
  "kubectl -n ${NAMESPACE} exec -i ${HELM_RELEASE}-0 -c nifi -- cat /opt/nifi/nifi-current/logs/versioned-flow-imports-bootstrap-status.json" \
  "kubectl -n ${NAMESPACE} get nificluster ${HELM_RELEASE} -o yaml" \
  "kubectl -n ${NAMESPACE} get pods,configmap,secret"
