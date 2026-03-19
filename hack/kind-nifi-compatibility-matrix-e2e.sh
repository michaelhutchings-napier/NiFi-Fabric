#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

FAST_PROFILE="${FAST_PROFILE:-true}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
COMPATIBILITY_VERSIONS="${COMPATIBILITY_VERSIONS:-2.0.0 2.8.0}"
START_EPOCH="$(date +%s)"

elapsed() {
  echo "$(( $(date +%s) - START_EPOCH ))"
}

cluster_name_for() {
  local version="$1"
  printf 'nifi-fabric-compat-%s' "${version//./-}"
}

run_case() {
  local version="$1"
  local cluster_name

  cluster_name="$(cluster_name_for "${version}")"

  phase "Running shared NiFi 2.x compatibility contract for ${version}"
  KIND_CLUSTER_NAME="${cluster_name}" \
    VERSION_LABEL="${version}" \
    NIFI_IMAGE="apache/nifi:${version}" \
    NIFI_IMAGE_REPOSITORY="apache/nifi" \
    NIFI_IMAGE_TAG="${version}" \
    FAST_PROFILE="${FAST_PROFILE}" \
    SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP}" \
    bash "${ROOT_DIR}/hack/kind-nifi-compatibility-contract.sh"
}

phase "Checking prerequisites"
check_prereqs

for version in ${COMPATIBILITY_VERSIONS}; do
  run_case "${version}"
done

print_success_footer "NiFi 2.x compatibility matrix completed in +$(elapsed)s" \
  "make kind-nifi-compatibility-fast-e2e-reuse" \
  "kubectl -n nifi get nificluster nifi -o yaml" \
  "kubectl -n nifi get servicemonitor" \
  "kubectl -n nifi get statefulset nifi -o jsonpath='{.spec.replicas}{\"\\n\"}'"
