#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
name=""
expected_phase=""
expected_ownership_state=""
expected_observed_version=""
expected_last_successful_version=""
expected_last_operation_substring=""
expect_no_retained_owned_imports="false"
declare -a expected_retained_owned_imports=()
declare -a expected_conditions=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      namespace="$2"
      shift 2
      ;;
    --name)
      name="$2"
      shift 2
      ;;
    --expected-phase)
      expected_phase="$2"
      shift 2
      ;;
    --expected-ownership-state)
      expected_ownership_state="$2"
      shift 2
      ;;
    --expected-observed-version)
      expected_observed_version="$2"
      shift 2
      ;;
    --expected-last-successful-version)
      expected_last_successful_version="$2"
      shift 2
      ;;
    --expected-retained-owned-import)
      expected_retained_owned_imports+=("$2")
      shift 2
      ;;
    --expect-no-retained-owned-imports)
      expect_no_retained_owned_imports="true"
      shift
      ;;
    --expect-condition)
      expected_conditions+=("$2")
      shift 2
      ;;
    --expected-last-operation-substring)
      expected_last_operation_substring="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "${name}" ]]; then
  echo "--name is required" >&2
  exit 1
fi

for cmd in kubectl python3; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
done

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

kubectl -n "${namespace}" get nifidataflow "${name}" -o json >"${tmpdir}/dataflow.json"

printf '%s\n' "${expected_retained_owned_imports[@]}" >"${tmpdir}/expected-retained.txt"
printf '%s\n' "${expected_conditions[@]}" >"${tmpdir}/expected-conditions.txt"

python3 - \
  "${tmpdir}/dataflow.json" \
  "${expected_phase}" \
  "${expected_ownership_state}" \
  "${expected_observed_version}" \
  "${expected_last_successful_version}" \
  "${expected_last_operation_substring}" \
  "${expect_no_retained_owned_imports}" \
  "${tmpdir}/expected-retained.txt" \
  "${tmpdir}/expected-conditions.txt" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
expected_phase = sys.argv[2]
expected_ownership_state = sys.argv[3]
expected_observed_version = sys.argv[4]
expected_last_successful_version = sys.argv[5]
expected_last_operation_substring = sys.argv[6]
expect_no_retained_owned_imports = sys.argv[7] == "true"
expected_retained_path = sys.argv[8]
expected_conditions_path = sys.argv[9]

status = payload.get("status") or {}

if expected_phase and (status.get("phase") or "") != expected_phase:
    raise SystemExit(f"expected status.phase {expected_phase!r}, got {(status.get('phase') or '')!r}")

ownership = status.get("ownership") or {}
if expected_ownership_state and (ownership.get("state") or "") != expected_ownership_state:
    raise SystemExit(
        f"expected status.ownership.state {expected_ownership_state!r}, got {(ownership.get('state') or '')!r}"
    )

if expected_observed_version and str(status.get("observedVersion") or "") != expected_observed_version:
    raise SystemExit(
        f"expected status.observedVersion {expected_observed_version!r}, got {str(status.get('observedVersion') or '')!r}"
    )

if expected_last_successful_version and str(status.get("lastSuccessfulVersion") or "") != expected_last_successful_version:
    raise SystemExit(
        "expected status.lastSuccessfulVersion "
        f"{expected_last_successful_version!r}, got {str(status.get('lastSuccessfulVersion') or '')!r}"
    )

last_operation = status.get("lastOperation") or {}
if expected_last_operation_substring and expected_last_operation_substring not in (last_operation.get("message") or ""):
    raise SystemExit(
        f"expected status.lastOperation.message to contain {expected_last_operation_substring!r}, got {(last_operation.get('message') or '')!r}"
    )

retained = status.get("warnings", {}).get("retainedOwnedImports") or []
retained_names = [str(entry.get("name") or "") for entry in retained if isinstance(entry, dict)]
expected_retained = [
    line.strip()
    for line in open(expected_retained_path, encoding="utf-8").read().splitlines()
    if line.strip()
]

if expect_no_retained_owned_imports and retained:
    raise SystemExit(f"expected no retained owned imports, got {retained!r}")

for expected_name in expected_retained:
    if expected_name not in retained_names:
        raise SystemExit(
            f"expected retained owned import {expected_name!r} in status.warnings.retainedOwnedImports, got {retained_names!r}"
        )

conditions_by_type = {}
for entry in status.get("conditions") or []:
    condition_type = entry.get("type")
    if condition_type:
        conditions_by_type[condition_type] = entry

for raw in open(expected_conditions_path, encoding="utf-8").read().splitlines():
    spec = raw.strip()
    if not spec:
        continue
    if "=" not in spec:
        raise SystemExit(f"invalid expected condition spec {spec!r}; want Type=Status[:Reason]")
    condition_type, rest = spec.split("=", 1)
    if ":" in rest:
        expected_status, expected_reason = rest.split(":", 1)
    else:
        expected_status, expected_reason = rest, ""
    condition = conditions_by_type.get(condition_type)
    if not condition:
        raise SystemExit(f"expected condition {condition_type!r} to exist")
    if expected_status and (condition.get("status") or "") != expected_status:
        raise SystemExit(
            f"expected condition {condition_type!r} status {expected_status!r}, got {(condition.get('status') or '')!r}"
        )
    if expected_reason and (condition.get("reason") or "") != expected_reason:
        raise SystemExit(
            f"expected condition {condition_type!r} reason {expected_reason!r}, got {(condition.get('reason') or '')!r}"
        )

print(
    json.dumps(
        {
            "name": payload.get("metadata", {}).get("name", ""),
            "observedVersion": status.get("observedVersion", ""),
            "ownershipState": ownership.get("state", ""),
            "phase": status.get("phase", ""),
            "lastSuccessfulVersion": status.get("lastSuccessfulVersion", ""),
            "retainedOwnedImports": retained_names,
        },
        indent=2,
        sort_keys=True,
    )
)
PY

echo
echo "SUCCESS: NiFiDataflow status proof completed"
echo "  resource: ${name}"
echo "  expected phase: ${expected_phase:-any}"
echo "  expected ownership: ${expected_ownership_state:-any}"
echo "  expected last successful version: ${expected_last_successful_version:-any}"
