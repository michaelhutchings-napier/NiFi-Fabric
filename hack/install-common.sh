#!/usr/bin/env bash

set -euo pipefail

phase() {
  printf '\n==> %s\n' "$1"
}

info() {
  printf '    %s\n' "$1"
}

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "ERROR: missing required command: ${cmd}" >&2
    exit 1
  fi
}

check_prereqs() {
  require_command kind
  require_command kubectl
  require_command helm
  require_command docker
}

check_cert_manager_prereqs() {
  check_prereqs
  require_command cmctl
}

require_kind_cluster() {
  local cluster_name="$1"
  local attempts="${2:-10}"
  local sleep_seconds="${3:-1}"
  local attempt

  for attempt in $(seq 1 "${attempts}"); do
    if kind get clusters | grep -qx "${cluster_name}"; then
      return 0
    fi
    sleep "${sleep_seconds}"
  done

  echo "ERROR: kind cluster ${cluster_name} does not exist" >&2
  exit 1
}

configure_kind_cluster_access() {
  local cluster_name="$1"
  local kubeconfig_path="${TMPDIR:-/tmp}/${cluster_name}.kubeconfig"
  require_kind_cluster "${cluster_name}"
  kind get kubeconfig --name "${cluster_name}" >"${kubeconfig_path}"
  export KUBECONFIG="${kubeconfig_path}"
}

print_failure_help() {
  local namespace="$1"
  local release="${2:-nifi}"
  local controller_namespace="${3:-nifi-system}"
  local controller_deployment="${4:-nifi-fabric-controller-manager}"

  cat <<EOF >&2

Install failed.

Most useful debug commands:
  make kind-health
  kubectl -n ${namespace} get pods
  kubectl -n ${namespace} get sts ${release}
  kubectl -n ${namespace} get events --sort-by=.lastTimestamp | tail -n 50
  kubectl -n ${controller_namespace} logs deployment/${controller_deployment} --tail=200
  kubectl -n ${namespace} get nificluster ${release} -o jsonpath='{.status.lastOperation.phase}{"\\n"}{.status.lastOperation.message}{"\\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\\n"}{end}' 2>/dev/null || true
  kubectl -n ${controller_namespace} port-forward deployment/${controller_deployment} 8080:8080
  curl -s localhost:8080/metrics | rg '^nifi_fabric_' || true
EOF
}

print_success_footer() {
  local title="$1"
  shift

  printf '\nSUCCESS: %s\n\n' "${title}"
  echo "Next commands:"
  for cmd in "$@"; do
    printf '  %s\n' "${cmd}"
  done
}
