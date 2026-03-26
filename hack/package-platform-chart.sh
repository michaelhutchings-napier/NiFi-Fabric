#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_CHARTS_DIR="${ROOT_DIR}/charts"
WORK_DIR="$(mktemp -d)"
OUTPUT_DIR="${OUTPUT_DIR:-${ROOT_DIR}/dist/charts}"

cleanup() {
  rm -rf "${WORK_DIR}"
}

trap cleanup EXIT

usage() {
  cat <<'EOF'
Package a fresh charts/nifi-platform archive without relying on any stale nested .tgz files.

Usage:
  hack/package-platform-chart.sh [--output-dir <path>]

Options:
  --output-dir <path>  Directory that will receive the packaged chart. Default: dist/charts
  -h, --help           Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir)
      OUTPUT_DIR="$2"
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

cp -R "${SOURCE_CHARTS_DIR}" "${WORK_DIR}/charts"
rm -rf "${WORK_DIR}/charts/nifi-platform/charts"
helm dependency build "${WORK_DIR}/charts/nifi-platform" >/dev/null

mkdir -p "${OUTPUT_DIR}"
helm package "${WORK_DIR}/charts/nifi-platform" --destination "${OUTPUT_DIR}" >/dev/null

echo "packaged fresh nifi-platform chart into ${OUTPUT_DIR}" >&2
