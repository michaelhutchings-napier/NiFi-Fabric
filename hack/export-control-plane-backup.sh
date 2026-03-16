#!/usr/bin/env bash

set -euo pipefail

release=""
namespace=""
output_dir=""

usage() {
  cat <<'EOF'
Usage:
  bash hack/export-control-plane-backup.sh \
    --release <helm-release> \
    --namespace <namespace> \
    --output-dir <backup-dir>

This exports a thin control-plane backup bundle for a live NiFi-Fabric release.

Artifacts include:
  - user-supplied Helm values
  - fully resolved Helm values
  - rendered Helm manifest
  - managed object inventory
  - sanitized NiFiCluster intent snapshot when present
  - referenced Secret/ConfigMap inventory
  - sanitized live snapshots of referenced objects
  - a recovery checklist
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --release)
      release="${2:-}"
      shift 2
      ;;
    --namespace)
      namespace="${2:-}"
      shift 2
      ;;
    --output-dir)
      output_dir="${2:-}"
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

if [[ -z "${release}" || -z "${namespace}" || -z "${output_dir}" ]]; then
  usage >&2
  exit 1
fi

for cmd in helm kubectl python3; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
done

if ! helm -n "${namespace}" status "${release}" >/dev/null 2>&1; then
  echo "helm release ${release} not found in namespace ${namespace}" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

mkdir -p "${output_dir}"
mkdir -p "${output_dir}/referenced-resources"

helm -n "${namespace}" get values "${release}" -o yaml >"${output_dir}/helm-values.user.yaml"
helm -n "${namespace}" get values "${release}" -o json >"${tmpdir}/helm-values.user.json"
helm -n "${namespace}" get values "${release}" --all -o yaml >"${output_dir}/helm-values.resolved.yaml"
helm -n "${namespace}" get values "${release}" --all -o json >"${tmpdir}/helm-values.resolved.json"
helm -n "${namespace}" get manifest "${release}" >"${output_dir}/helm-manifest.yaml"
helm -n "${namespace}" status "${release}" -o json >"${tmpdir}/helm-status.json"

if ! kubectl create --dry-run=client -f "${output_dir}/helm-manifest.yaml" -o name >"${output_dir}/managed-objects.txt" 2>"${tmpdir}/managed-objects.err"; then
  {
    echo "# kubectl client-side inventory failed for the rendered manifest"
    cat "${tmpdir}/managed-objects.err"
  } >"${output_dir}/managed-objects.txt"
fi

if kubectl -n "${namespace}" get nificlusters.platform.nifi.io -l "app.kubernetes.io/instance=${release}" -o json >"${tmpdir}/nificlusters.json" 2>/dev/null; then
  python3 - "${tmpdir}/nificlusters.json" "${output_dir}/nificluster-intent.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
items = payload.get("items", [])
if not items:
    raise SystemExit(0)
item = items[0]
item.pop("status", None)
meta = item.setdefault("metadata", {})
for field in ("managedFields", "resourceVersion", "uid", "creationTimestamp", "generation"):
    meta.pop(field, None)
json.dump(item, open(sys.argv[2], "w", encoding="utf-8"), indent=2, sort_keys=True)
PY
fi

python3 - "${tmpdir}/helm-values.user.json" "${tmpdir}/helm-values.resolved.json" "${tmpdir}/helm-status.json" "${output_dir}" "${release}" "${namespace}" <<'PY'
import datetime as dt
import json
import os
import sys

user_values = json.load(open(sys.argv[1], encoding="utf-8"))
resolved_values = json.load(open(sys.argv[2], encoding="utf-8"))
status = json.load(open(sys.argv[3], encoding="utf-8"))
output_dir, release, namespace = sys.argv[4:]

def app_values(values):
    if isinstance(values, dict) and isinstance(values.get("nifi"), dict):
        return values["nifi"]
    return values if isinstance(values, dict) else {}

def add_ref(refs, kind, ref_namespace, name, purpose, source_path, capture_mode):
    if not name:
        return
    key = (kind, ref_namespace, name)
    entry = refs.setdefault(
        key,
        {
            "kind": kind,
            "namespace": ref_namespace,
            "name": name,
            "captureMode": capture_mode,
            "purposes": [],
            "sourcePaths": [],
        },
    )
    if purpose not in entry["purposes"]:
        entry["purposes"].append(purpose)
    if source_path not in entry["sourcePaths"]:
        entry["sourcePaths"].append(source_path)

def recursive_ref_scan(node, path, refs, default_namespace):
    if isinstance(node, dict):
        for key, value in node.items():
            child_path = f"{path}.{key}" if path else key
            if key == "secretRef" and isinstance(value, dict) and value.get("name"):
                add_ref(refs, "Secret", default_namespace, value["name"], "referenced Secret", child_path, "operator-managed-reference")
            elif key == "configMapRef" and isinstance(value, dict) and value.get("name"):
                add_ref(refs, "ConfigMap", default_namespace, value["name"], "referenced ConfigMap", child_path, "operator-managed-reference")
            else:
                recursive_ref_scan(value, child_path, refs, default_namespace)
    elif isinstance(node, list):
        for index, item in enumerate(node):
            recursive_ref_scan(item, f"{path}[{index}]", refs, default_namespace)

refs = {}
app = app_values(resolved_values)

auth = app.get("auth", {})
single_user = auth.get("singleUser", {})
add_ref(refs, "Secret", namespace, single_user.get("existingSecret"), "single-user credentials", "auth.singleUser.existingSecret", "operator-managed-reference")

tls = app.get("tls", {})
tls_mode = tls.get("mode", "externalSecret")
if tls_mode == "externalSecret":
    add_ref(refs, "Secret", namespace, tls.get("existingSecret", "nifi-tls"), "workload TLS Secret", "tls.existingSecret", "operator-managed-reference")
else:
    cert_manager = tls.get("certManager", {})
    add_ref(refs, "Secret", namespace, cert_manager.get("secretName", "nifi-tls"), "cert-manager target TLS Secret", "tls.certManager.secretName", "cluster-or-cert-manager-managed")

sensitive_props = tls.get("sensitiveProps", {})
if isinstance(sensitive_props.get("secretRef"), dict):
    add_ref(refs, "Secret", namespace, sensitive_props["secretRef"].get("name"), "sensitive properties key Secret", "tls.sensitiveProps.secretRef.name", "operator-managed-reference")

recursive_ref_scan(app, "", refs, namespace)

platform_chart_hint = "charts/nifi-platform" if isinstance(resolved_values.get("nifi"), dict) else "charts/nifi"

platform = resolved_values if isinstance(resolved_values, dict) else {}
trust_manager = platform.get("trustManager", {})
if isinstance(trust_manager, dict) and trust_manager.get("enabled"):
    sources = trust_manager.get("sources", {})
    for index, source in enumerate(sources.get("configMaps", []) or []):
        add_ref(refs, "ConfigMap", source.get("namespace") or "trust-manager-source-namespace", source.get("name"), "trust-manager source ConfigMap", f"trustManager.sources.configMaps[{index}].name", "operator-managed-reference")
    for index, source in enumerate(sources.get("secrets", []) or []):
        add_ref(refs, "Secret", source.get("namespace") or "trust-manager-source-namespace", source.get("name"), "trust-manager source Secret", f"trustManager.sources.secrets[{index}].name", "operator-managed-reference")
    mirror = trust_manager.get("mirrorTLSSecret", {})
    if mirror.get("enabled"):
        add_ref(
            refs,
            "Secret",
            mirror.get("trustNamespace", "cert-manager"),
            mirror.get("sourceSecretName") or tls.get("existingSecret", "nifi-tls"),
            "trust-manager mirror source Secret",
            "trustManager.mirrorTLSSecret.sourceSecretName",
            "operator-managed-reference",
        )

metadata = {
    "backupFormatVersion": 1,
    "createdAtUtc": dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat(),
    "release": {
        "name": release,
        "namespace": namespace,
        "chartHint": platform_chart_hint,
        "chart": status.get("chart"),
        "appVersion": status.get("app_version"),
    },
    "notes": [
        "helm-values.user.yaml is the GitOps-friendly intent snapshot when original overlays are unavailable",
        "helm-values.resolved.yaml is the full resolved recovery snapshot for practical reconstruction",
        "nificluster-intent.json is informational and should not replace the standard Helm-centered managed recovery path",
        "Secret data is intentionally not exported in cleartext by this bundle",
    ],
}

inventory = {
    "backupFormatVersion": 1,
    "release": metadata["release"],
    "references": sorted(refs.values(), key=lambda item: (item["kind"], item["namespace"], item["name"])),
}

with open(os.path.join(output_dir, "backup-metadata.json"), "w", encoding="utf-8") as handle:
    json.dump(metadata, handle, indent=2, sort_keys=True)
with open(os.path.join(output_dir, "reference-inventory.json"), "w", encoding="utf-8") as handle:
    json.dump(inventory, handle, indent=2, sort_keys=True)
PY

python3 - "${output_dir}/reference-inventory.json" "${output_dir}/recovery-checklist.md" <<'PY'
import json
import sys

inventory = json.load(open(sys.argv[1], encoding="utf-8"))
release = inventory["release"]["name"]
namespace = inventory["release"]["namespace"]
chart_hint = inventory["release"]["chartHint"]
refs = inventory.get("references", [])

with open(sys.argv[2], "w", encoding="utf-8") as handle:
    handle.write("# Control-Plane Recovery Checklist\n\n")
    handle.write("1. Restore or recreate operator-owned prerequisites before reinstalling the platform.\n")
    handle.write("2. Reinstall the release from the repo root using the recovery helper or the equivalent Helm command.\n")
    handle.write("3. Verify the referenced Secrets, ConfigMaps, cert-manager, and trust-manager dependencies are present.\n")
    handle.write("4. Validate the managed rollout after Helm reapply.\n\n")
    handle.write("Recommended recovery command from the repo root:\n\n")
    handle.write("```bash\n")
    handle.write(f"bash hack/recover-control-plane-backup.sh --backup-dir {sys.argv[1].rsplit('/', 1)[0]} --chart {chart_hint} --release {release} --namespace {namespace}\n")
    handle.write("```\n\n")
    handle.write("Primary operator-owned references to restore or verify first:\n\n")
    if refs:
        for ref in refs:
            purposes = ", ".join(ref.get("purposes", []))
            handle.write(f"- `{ref['kind']}/{ref['namespace']}/{ref['name']}`: {purposes}\n")
    else:
        handle.write("- no external Secret or ConfigMap references were detected from the Helm values snapshot\n")
    handle.write("\nNotes:\n\n")
    handle.write("- `helm-values.user.yaml` is the preferred human-maintained source when you already keep overlays in Git.\n")
    handle.write("- `helm-values.resolved.yaml` is the thin practical recovery fallback when the original overlay composition is unavailable.\n")
    handle.write("- `nificluster-intent.json` is an intent snapshot only. Standard managed recovery stays centered on `charts/nifi-platform`, not on manually applying live exported status back into the cluster.\n")
    handle.write("- Secret snapshots in `referenced-resources/` are metadata-only with redacted values.\n")
PY

python3 - "${output_dir}/reference-inventory.json" "${output_dir}/referenced-resources" <<'PY'
import json
import os
import subprocess
import sys

inventory = json.load(open(sys.argv[1], encoding="utf-8"))
target_dir = sys.argv[2]

for entry in inventory.get("references", []):
    kind = entry["kind"]
    namespace = entry["namespace"]
    name = entry["name"]
    safe_name = f"{kind.lower()}-{namespace.replace('/', '_')}-{name.replace('/', '_')}.json"
    output_path = os.path.join(target_dir, safe_name)
    try:
        payload = subprocess.check_output(
            ["kubectl", "-n", namespace, "get", kind.lower(), name, "-o", "json"],
            text=True,
            stderr=subprocess.DEVNULL,
        )
        obj = json.loads(payload)
        obj.pop("status", None)
        meta = obj.setdefault("metadata", {})
        for field in ("managedFields", "resourceVersion", "uid", "creationTimestamp", "generation"):
            meta.pop(field, None)
        annotations = meta.setdefault("annotations", {})
        if kind == "Secret":
            data = obj.get("data", {})
            obj["data"] = {key: "<redacted>" for key in sorted(data.keys())}
            if "stringData" in obj:
                obj["stringData"] = {key: "<redacted>" for key in sorted(obj["stringData"].keys())}
            annotations["platform.nifi.io/control-plane-backup-note"] = "Secret data redacted; restore from operator-managed secret escrow."
        with open(output_path, "w", encoding="utf-8") as handle:
            json.dump(obj, handle, indent=2, sort_keys=True)
        entry["exists"] = True
        entry["snapshotPath"] = f"referenced-resources/{safe_name}"
    except subprocess.CalledProcessError:
        entry["exists"] = False
        entry["snapshotPath"] = ""

with open(sys.argv[1], "w", encoding="utf-8") as handle:
    json.dump(inventory, handle, indent=2, sort_keys=True)
PY

echo "PASS: control-plane backup bundle exported"
echo "  release: ${release}"
echo "  namespace: ${namespace}"
echo "  output: ${output_dir}"
echo "  values: ${output_dir}/helm-values.user.yaml, ${output_dir}/helm-values.resolved.yaml"
echo "  manifest: ${output_dir}/helm-manifest.yaml"
echo "  references: ${output_dir}/reference-inventory.json"
echo "  checklist: ${output_dir}/recovery-checklist.md"
