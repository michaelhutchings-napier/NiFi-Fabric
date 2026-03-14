#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="${ROOT_DIR}/charts/nifi-platform"

PROFILE="${PROFILE:-managed}"
NAMESPACE="${NAMESPACE:-nifi}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
OUTPUT_PATH="${OUTPUT_PATH:-}"
EXTRA_VALUES=()

usage() {
  cat <<'EOF'
Render a generated install bundle from charts/nifi-platform.

Usage:
  hack/render-platform-bundle.sh [options]

Options:
  --profile <managed|managed-cert-manager|standalone>  Bundle profile. Default: managed
  --namespace <name>                                   Kubernetes namespace. Default: nifi
  --release <name>                                     Helm release name. Default: nifi
  --output <path>                                      Write bundle to this file instead of stdout
  -f, --values <path>                                  Extra values file to append
  -h, --help                                           Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      PROFILE="$2"
      shift 2
      ;;
    --namespace)
      NAMESPACE="$2"
      shift 2
      ;;
    --release)
      HELM_RELEASE="$2"
      shift 2
      ;;
    --output)
      OUTPUT_PATH="$2"
      shift 2
      ;;
    -f|--values)
      EXTRA_VALUES+=("$2")
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

profile_values=()
case "${PROFILE}" in
  managed)
    profile_values+=("${ROOT_DIR}/examples/platform-managed-values.yaml")
    ;;
  managed-cert-manager)
    profile_values+=("${ROOT_DIR}/examples/platform-managed-cert-manager-values.yaml")
    ;;
  standalone)
    profile_values+=("${ROOT_DIR}/examples/platform-standalone-values.yaml")
    ;;
  *)
    echo "unsupported bundle profile: ${PROFILE}" >&2
    exit 1
    ;;
esac

helm dependency build "${CHART_DIR}" >/dev/null

helm_args=(
  template
  "${HELM_RELEASE}"
  "${CHART_DIR}"
  --namespace "${NAMESPACE}"
  --include-crds
)

for values_file in "${profile_values[@]}"; do
  helm_args+=(-f "${values_file}")
done
for values_file in "${EXTRA_VALUES[@]}"; do
  helm_args+=(-f "${values_file}")
done

if [[ -n "${OUTPUT_PATH}" ]]; then
  mkdir -p "$(dirname "${OUTPUT_PATH}")"
  helm "${helm_args[@]}" >"${OUTPUT_PATH}"
  echo "wrote ${PROFILE} platform bundle to ${OUTPUT_PATH}" >&2
else
  helm "${helm_args[@]}"
fi
