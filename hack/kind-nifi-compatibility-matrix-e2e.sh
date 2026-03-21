#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/hack/install-common.sh"

FAST_PROFILE="${FAST_PROFILE:-true}"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
REUSE_SINGLE_KIND_CLUSTER="${REUSE_SINGLE_KIND_CLUSTER:-true}"
COMPATIBILITY_VERSIONS="${COMPATIBILITY_VERSIONS:-2.0.0 2.1.0 2.2.0 2.3.0 2.4.0 2.5.0 2.6.0 2.7.0 2.8.0}"
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
  local case_skip_kind_bootstrap

  if [[ "${REUSE_SINGLE_KIND_CLUSTER}" == "true" ]]; then
    cluster_name="$(cluster_name_for "$(printf '%s' "${COMPATIBILITY_VERSIONS}" | awk '{print $1}')")"
  else
    cluster_name="$(cluster_name_for "${version}")"
  fi

  case_skip_kind_bootstrap="${SKIP_KIND_BOOTSTRAP}"
  if [[ "${REUSE_SINGLE_KIND_CLUSTER}" == "true" && "${version}" != "$(printf '%s' "${COMPATIBILITY_VERSIONS}" | awk '{print $1}')" ]]; then
    case_skip_kind_bootstrap="true"
  fi

  phase "Running shared NiFi 2.x compatibility contract for ${version}"
  KIND_CLUSTER_NAME="${cluster_name}" \
    VERSION_LABEL="${version}" \
    NIFI_IMAGE="apache/nifi:${version}" \
    NIFI_IMAGE_REPOSITORY="apache/nifi" \
    NIFI_IMAGE_TAG="${version}" \
    FAST_PROFILE="${FAST_PROFILE}" \
    SKIP_KIND_BOOTSTRAP="${case_skip_kind_bootstrap}" \
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
