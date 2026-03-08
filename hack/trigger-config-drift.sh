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

kubectl -n "${namespace}" get configmap "${configmap}" -o json >"${tmp}"

python3 - "${tmp}" <<'PY'
import json
import sys
import time
from pathlib import Path

path = Path(sys.argv[1])
payload = json.loads(path.read_text())
key = "nifi.properties.overrides"
current = payload.setdefault("data", {}).get(key, "")
marker = f"# platform.nifi.io drift-marker={time.time_ns()}"
if current and not current.endswith("\n"):
    current += "\n"
payload["data"][key] = current + marker + "\n"
path.write_text(json.dumps(payload))
PY

kubectl -n "${namespace}" apply -f "${tmp}" >/dev/null
echo "config drift marker appended to ConfigMap/${configmap} in namespace ${namespace}"
