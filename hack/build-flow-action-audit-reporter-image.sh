#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUNDLE_DIR="${ROOT_DIR}/extensions/nifi-flow-action-audit-reporter-bundle"
REPORTER_VERSION="$("${ROOT_DIR}/hack/flow-action-audit-reporter-version.sh")"
IMAGE_REPOSITORY="${FLOW_ACTION_AUDIT_REPORTER_IMAGE_REPOSITORY:-nifi-flow-action-audit-reporter}"
IMAGE_TAG="${IMAGE_TAG:-${IMAGE_REPOSITORY}:${REPORTER_VERSION}}"
NAR_PATH="${BUNDLE_DIR}/nifi-flow-action-audit-reporter-nar/target/nifi-flow-action-audit-reporter-nar-${REPORTER_VERSION}.nar"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required to build the flow-action audit reporter image" >&2
  exit 1
fi

if [[ "${FLOW_ACTION_AUDIT_REPORTER_SKIP_NAR_BUILD:-false}" != "true" ]]; then
  bash "${ROOT_DIR}/hack/build-flow-action-audit-reporter-nar.sh" >/dev/null
fi

if [[ ! -f "${NAR_PATH}" ]]; then
  echo "expected flow-action audit reporter NAR at ${NAR_PATH}" >&2
  exit 1
fi

docker build \
  -t "${IMAGE_TAG}" \
  -f "${BUNDLE_DIR}/Dockerfile" \
  "${BUNDLE_DIR}"

printf '%s\n' "${IMAGE_TAG}"
