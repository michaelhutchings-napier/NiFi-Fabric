#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-flow-registry-github}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-nifi-system}"
CONTROLLER_DEPLOYMENT="${CONTROLLER_DEPLOYMENT:-nifi-fabric-controller-manager}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.8.0}"
FLOW_REGISTRY_SECRET_NAME="${FLOW_REGISTRY_SECRET_NAME:-github-flow-registry}"
FLOW_REGISTRY_CLIENT_NAME="${FLOW_REGISTRY_CLIENT_NAME:-github-flows-kind}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"
FAST_VALUES_FILE="${FAST_VALUES_FILE:-examples/test-fast-values.yaml}"

run_make() {
  make -C "${ROOT_DIR}" "$@"
}

require_local_prereqs() {
  check_prereqs
  require_command curl
  require_command jq
  require_command python3
}

require_reuse_prereqs() {
  kubectl -n "${NAMESPACE}" get secret nifi-tls >/dev/null
  kubectl -n "${NAMESPACE}" get secret nifi-auth >/dev/null
}

ensure_fresh_cluster() {
  kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  run_make kind-up KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
}

dump_diagnostics() {
  set +e
  echo
  echo "==> GitHub Flow Registry evaluator diagnostics"
  kubectl config current-context || true
  kubectl get namespace "${NAMESPACE}" || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,UID:.metadata.uid,REV:.metadata.labels.controller-revision-hash || true
  kubectl -n "${NAMESPACE}" get configmap "${HELM_RELEASE}-flow-registry-clients" -o jsonpath='{.data.clients\.json}' || true
  printf '\n' || true
  kubectl -n "${NAMESPACE}" get secret "${FLOW_REGISTRY_SECRET_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get svc github-mock -o wide || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${CONTROLLER_NAMESPACE}" logs deployment/"${CONTROLLER_DEPLOYMENT}" --tail=300 || true
  kubectl -n "${NAMESPACE}" logs deployment/github-mock --tail=300 || true
}

trap 'dump_diagnostics; print_failure_help "${NAMESPACE}" "${HELM_RELEASE}" "${CONTROLLER_NAMESPACE}" "${CONTROLLER_DEPLOYMENT}"; exit 1' ERR

helm_values_args=(
  -f "${ROOT_DIR}/examples/managed/values.yaml"
  -f "${ROOT_DIR}/examples/nifi-2.8.0-values.yaml"
  -f "${ROOT_DIR}/examples/github-flow-registry-kind-values.yaml"
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${ROOT_DIR}/${FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

phase "Checking prerequisites"
require_local_prereqs

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing kind cluster for NiFi ${NIFI_IMAGE##*:} GitHub Flow Registry proof"
  configure_kind_cluster_access "${KIND_CLUSTER_NAME}"
  require_reuse_prereqs
else
  phase "Creating fresh kind cluster for NiFi ${NIFI_IMAGE##*:} GitHub Flow Registry proof"
  ensure_fresh_cluster
  configure_kind_cluster_access "${KIND_CLUSTER_NAME}"
fi

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing NiFi image and Secrets"
else
  phase "Loading NiFi image into kind"
  run_make kind-load-nifi-image KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NIFI_IMAGE="${NIFI_IMAGE}"

  phase "Creating TLS and auth Secrets"
  run_make kind-secrets KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}" NIFI_IMAGE="${NIFI_IMAGE}"
fi

phase "Creating GitHub Flow Registry token Secret"
kubectl -n "${NAMESPACE}" create secret generic "${FLOW_REGISTRY_SECRET_NAME}" \
  --from-literal=token=dummytoken \
  --dry-run=client -o yaml | kubectl apply -f -

phase "Deploying GitHub-compatible evaluator service"
NAMESPACE="${NAMESPACE}" bash "${ROOT_DIR}/hack/deploy-github-flow-registry-mock.sh"

if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  phase "Reusing existing CRD and controller"
else
  phase "Installing CRD"
  run_make install-crd

  phase "Building and loading controller image"
  run_make docker-build-controller
  run_make kind-load-controller KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"

  phase "Deploying controller"
  run_make deploy-controller
  kubectl -n "${CONTROLLER_NAMESPACE}" rollout status deployment/"${CONTROLLER_DEPLOYMENT}" --timeout=5m
fi

phase "Installing managed NiFi ${NIFI_IMAGE##*:} release with GitHub Flow Registry overlay${profile_label}"
helm upgrade --install "${HELM_RELEASE}" "${ROOT_DIR}/charts/nifi" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}"

phase "Applying NiFiCluster"
kubectl apply -f "${ROOT_DIR}/examples/managed/nificluster.yaml"

phase "Verifying base NiFi cluster health"
run_make kind-health KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" NAMESPACE="${NAMESPACE}" HELM_RELEASE="${HELM_RELEASE}"

phase "Creating and exercising the GitHub Flow Registry client"
bash "${ROOT_DIR}/hack/prove-github-flow-registry-client.sh" \
  --namespace "${NAMESPACE}" \
  --release "${HELM_RELEASE}" \
  --client-name "${FLOW_REGISTRY_CLIENT_NAME}" \
  --expect-bucket team-a \
  --expect-bucket team-b

print_success_footer "NiFi ${NIFI_IMAGE##*:} GitHub Flow Registry client runtime proof completed" \
  "make kind-health KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}" \
  "kubectl -n ${NAMESPACE} get configmap ${HELM_RELEASE}-flow-registry-clients -o jsonpath='{.data.clients\\.json}'" \
  "kubectl -n ${NAMESPACE} logs deployment/github-mock --tail=100" \
  "kubectl -n ${CONTROLLER_NAMESPACE} logs deployment/${CONTROLLER_DEPLOYMENT} --tail=200"
