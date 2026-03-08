#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
secret=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      namespace="$2"
      shift 2
      ;;
    --secret)
      secret="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "${secret}" ]]; then
  echo "--secret is required" >&2
  exit 1
fi

kubectl -n "${namespace}" patch secret "${secret}" \
  --type merge \
  -p "{\"stringData\":{\"drift-marker\":\"$(date -u +%Y%m%dT%H%M%SZ)\"}}" >/dev/null

echo "TLS drift marker written to Secret/${secret} in namespace ${namespace}"
