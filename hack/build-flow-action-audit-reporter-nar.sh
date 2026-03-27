#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUNDLE_DIR="${ROOT_DIR}/extensions/nifi-flow-action-audit-reporter-bundle"
REPORTER_VERSION="$("${ROOT_DIR}/hack/flow-action-audit-reporter-version.sh")"
OUTPUT_DIR="${FLOW_ACTION_AUDIT_REPORTER_OUTPUT_DIR:-}"
MAVEN_HOME_DIR="${ROOT_DIR}/.tmp/maven-home"
MAVEN_REPO_DIR="${ROOT_DIR}/.tmp/maven-repository"
MAVEN_CONFIG_DIR="${ROOT_DIR}/.tmp/maven-config"

mkdir -p "${MAVEN_HOME_DIR}" "${MAVEN_REPO_DIR}" "${MAVEN_CONFIG_DIR}"

run_maven() {
  local -a args=("$@")
  if command -v mvn >/dev/null 2>&1; then
    (
      cd "${BUNDLE_DIR}" &&
      HOME="${MAVEN_HOME_DIR}" \
      MAVEN_CONFIG="${MAVEN_CONFIG_DIR}" \
      mvn -Dmaven.repo.local="${MAVEN_REPO_DIR}" "${args[@]}"
    )
    return
  fi

  if command -v docker >/dev/null 2>&1; then
    docker run --rm \
      --user "$(id -u):$(id -g)" \
      -v "${ROOT_DIR}:${ROOT_DIR}" \
      -e HOME="${MAVEN_HOME_DIR}" \
      -e MAVEN_CONFIG="${MAVEN_CONFIG_DIR}" \
      -w "${BUNDLE_DIR}" \
      maven:3.9.9-eclipse-temurin-21 \
      mvn -Dmaven.repo.local="${MAVEN_REPO_DIR}" "${args[@]}"
    return
  fi

  echo "mvn or docker is required to build the flow-action audit reporter NAR" >&2
  exit 1
}

run_maven -Drevision="${REPORTER_VERSION}" -DskipTests package "$@"

artifact_path="${BUNDLE_DIR}/nifi-flow-action-audit-reporter-nar/target/nifi-flow-action-audit-reporter-nar-${REPORTER_VERSION}.nar"
if [[ ! -f "${artifact_path}" ]]; then
  echo "expected flow-action audit reporter NAR at ${artifact_path}" >&2
  exit 1
fi

if [[ -n "${OUTPUT_DIR}" ]]; then
  mkdir -p "${OUTPUT_DIR}"
  cp "${artifact_path}" "${OUTPUT_DIR}/"
fi

printf '%s\n' "${artifact_path}"
