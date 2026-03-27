#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POM_FILE="${ROOT_DIR}/extensions/nifi-flow-action-audit-reporter-bundle/pom.xml"

if [[ -n "${FLOW_ACTION_AUDIT_REPORTER_VERSION:-}" ]]; then
  printf '%s\n' "${FLOW_ACTION_AUDIT_REPORTER_VERSION}"
  exit 0
fi

revision="$(sed -n 's|.*<revision>\(.*\)</revision>.*|\1|p' "${POM_FILE}" | head -n 1)"
if [[ -z "${revision}" ]]; then
  revision="$(sed -n 's|.*<version>\(.*\)</version>.*|\1|p' "${POM_FILE}" | head -n 1)"
fi

if [[ -z "${revision}" ]]; then
  echo "could not determine flow-action audit reporter version from ${POM_FILE}" >&2
  exit 1
fi

printf '%s\n' "${revision}"
