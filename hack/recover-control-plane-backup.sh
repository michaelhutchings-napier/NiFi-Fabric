#!/usr/bin/env bash

set -euo pipefail

backup_dir=""
chart=""
release=""
namespace=""
values_mode="resolved"
dry_run="false"

usage() {
  cat <<'EOF'
Usage:
  bash hack/recover-control-plane-backup.sh \
    --backup-dir <backup-dir> \
    [--chart charts/nifi-platform] \
    [--release <helm-release>] \
    [--namespace <namespace>] \
    [--values-mode user|resolved] \
    [--dry-run]

This restores the declarative control plane from an export bundle.

It does not restore:
  - PVC snapshots
  - queue contents
  - Secret values from escrow
  - cert-manager, trust-manager, or IdP external dependencies
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --backup-dir)
      backup_dir="${2:-}"
      shift 2
      ;;
    --chart)
      chart="${2:-}"
      shift 2
      ;;
    --release)
      release="${2:-}"
      shift 2
      ;;
    --namespace)
      namespace="${2:-}"
      shift 2
      ;;
    --values-mode)
      values_mode="${2:-}"
      shift 2
      ;;
    --dry-run)
      dry_run="true"
      shift 1
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

if [[ -z "${backup_dir}" ]]; then
  usage >&2
  exit 1
fi

if [[ "${values_mode}" != "user" && "${values_mode}" != "resolved" ]]; then
  echo "--values-mode must be one of: user, resolved" >&2
  exit 1
fi

for cmd in helm python3; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
done

metadata_path="${backup_dir}/backup-metadata.json"
inventory_path="${backup_dir}/reference-inventory.json"
values_path="${backup_dir}/helm-values.${values_mode}.yaml"

if [[ ! -f "${metadata_path}" ]]; then
  echo "missing backup metadata: ${metadata_path}" >&2
  exit 1
fi
if [[ ! -f "${values_path}" ]]; then
  echo "missing Helm values snapshot: ${values_path}" >&2
  exit 1
fi

mapfile -t defaults < <(
  python3 - "${metadata_path}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
release = payload.get("release", {})
print(release.get("name", ""))
print(release.get("namespace", ""))
print(release.get("chartHint", ""))
PY
)

release="${release:-${defaults[0]:-}}"
namespace="${namespace:-${defaults[1]:-}}"
chart="${chart:-${defaults[2]:-}}"

if [[ -z "${release}" || -z "${namespace}" || -z "${chart}" ]]; then
  echo "backup metadata does not provide enough defaults; set --chart, --release, and --namespace explicitly" >&2
  exit 1
fi

if [[ ! -f "${inventory_path}" ]]; then
  echo "missing reference inventory: ${inventory_path}" >&2
  exit 1
fi

echo "==> Control-plane recovery preflight"
echo "release: ${release}"
echo "namespace: ${namespace}"
echo "chart: ${chart}"
echo "values snapshot: ${values_path}"
echo
echo "Restore or recreate the operator-owned references listed below before relying on the recovered deployment:"
python3 - "${inventory_path}" <<'PY'
import json
import sys

inventory = json.load(open(sys.argv[1], encoding="utf-8"))
refs = inventory.get("references", [])
if not refs:
    print("- none detected")
else:
    for ref in refs:
        purposes = ", ".join(ref.get("purposes", []))
        exists = "present" if ref.get("exists") else "missing-at-export-time"
        print(f"- {ref['kind']}/{ref['namespace']}/{ref['name']} [{exists}]: {purposes}")
PY
echo

helm_cmd=(
  helm upgrade --install "${release}" "${chart}"
  --namespace "${namespace}"
  --create-namespace
  -f "${values_path}"
)

if [[ "${dry_run}" == "true" ]]; then
  helm_cmd+=(--dry-run --debug)
fi

echo "==> Running Helm recovery command"
printf ' %q' "${helm_cmd[@]}"
echo
"${helm_cmd[@]}"

echo
echo "PASS: control-plane recovery command completed"
echo "Next checks:"
echo "  kubectl -n ${namespace} get pods,svc,pvc"
echo "  helm -n ${namespace} status ${release}"
echo "  kubectl -n ${namespace} get nificluster -l app.kubernetes.io/instance=${release} -o yaml 2>/dev/null || true"
echo
echo "Still operator-owned:"
echo "  - Secret escrow and external secret-manager recovery"
echo "  - cert-manager, trust-manager, and IdP dependencies"
echo "  - PVC snapshot restore and any data-plane recovery"
