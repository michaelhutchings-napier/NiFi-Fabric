#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUNDLE_DIR="${ROOT_DIR}/extensions/nifi-flow-action-audit-reporter-bundle"
IMAGE_TAG="${IMAGE_TAG:-nifi-flow-action-audit-reporter:0.0.1-SNAPSHOT}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required to build the flow-action audit reporter image" >&2
  exit 1
fi

bash "${ROOT_DIR}/hack/build-flow-action-audit-reporter-nar.sh"

docker build \
  -t "${IMAGE_TAG}" \
  -f "${BUNDLE_DIR}/Dockerfile" \
  "${BUNDLE_DIR}"
