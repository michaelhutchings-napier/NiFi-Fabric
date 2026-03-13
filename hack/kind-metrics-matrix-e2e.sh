#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

FAST_PROFILE="${FAST_PROFILE:-true}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
NATIVE_KIND_CLUSTER_NAME="${NATIVE_KIND_CLUSTER_NAME:-nifi-fabric-metrics-native-api}"
EXPORTER_KIND_CLUSTER_NAME="${EXPORTER_KIND_CLUSTER_NAME:-nifi-fabric-metrics-exporter}"
START_EPOCH="$(date +%s)"

elapsed() {
  echo "$(( $(date +%s) - START_EPOCH ))"
}

run_mode() {
  local mode="$1"
  local cluster_name="$2"
  local script_name="$3"

  phase "Running metrics runtime proof for ${mode}"
  KIND_CLUSTER_NAME="${cluster_name}" FAST_PROFILE="${FAST_PROFILE}" SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP}" \
    bash "${ROOT_DIR}/hack/${script_name}"
}

phase "Checking prerequisites"
check_prereqs

run_mode "nativeApi" "${NATIVE_KIND_CLUSTER_NAME}" "kind-metrics-native-api-e2e.sh"
run_mode "exporter" "${EXPORTER_KIND_CLUSTER_NAME}" "kind-metrics-exporter-e2e.sh"

print_success_footer "metrics runtime proof matrix completed in +$(elapsed)s" \
  "make kind-metrics-fast-e2e-reuse" \
  "kubectl get servicemonitor -A | rg 'flow|exporter'" \
  "kubectl -n nifi get service nifi-metrics -o yaml" \
  "kubectl -n nifi get secret nifi-metrics-auth -o yaml" \
  "kubectl -n nifi exec metrics-exporter-probe -- sh -ec 'curl --silent --show-error --fail http://nifi-metrics.nifi.svc.cluster.local:9090/metrics | head'"
