#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
configmap=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      namespace="$2"
      shift 2
      ;;
    --configmap)
      configmap="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "${configmap}" ]]; then
  echo "--configmap is required" >&2
  exit 1
fi

tmp="$(mktemp)"
cleanup() {
  rm -f "${tmp}"
}
trap cleanup EXIT

python3 - "${tmp}" <<'PY'
import json
import sys
import time
from pathlib import Path

path = Path(sys.argv[1])
key = "platform.nifi.io.drift-marker"
payload = {"data": {key: str(time.time_ns())}}
path.write_text(json.dumps(payload))
PY

kubectl -n "${namespace}" patch configmap "${configmap}" \
  --type merge \
  --patch-file "${tmp}" >/dev/null
echo "config drift marker written to ConfigMap/${configmap} in namespace ${namespace}"
