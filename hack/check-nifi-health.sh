#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${NAMESPACE:-nifi}"
STATEFULSET="${STATEFULSET:-nifi}"
AUTH_SECRET="${AUTH_SECRET:-nifi-auth}"
CONTAINER="${CONTAINER:-nifi}"
KUBECTL="${KUBECTL:-kubectl}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-600}"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-10}"
STABLE_POLLS="${STABLE_POLLS:-3}"
ALLOW_FORMER_NODES="${ALLOW_FORMER_NODES:-false}"

usage() {
  cat <<'EOF'
Usage: hack/check-nifi-health.sh [--namespace ns] [--statefulset name] [--auth-secret name]
                                 [--timeout seconds] [--interval seconds] [--stable-polls count]
                                 [--allow-former-nodes]

Checks three separate signals for a standalone NiFi 2 cluster:
1. Kubernetes pod readiness
2. Secured NiFi API reachability on each pod
3. Cluster convergence from each pod's local /nifi-api/flow/cluster/summary view

The script exits 0 only after the convergence signal is stable for the requested
number of consecutive polls. It exits 1 on timeout or any hard failure.

`--allow-former-nodes` relaxes the total-node-count check so each healthy pod may
report `totalNodeCount >= expected replicas` while the remaining connected nodes
already match the new expected size. This is intended only for safe post-removal
checks such as managed hibernation or autoscaling scale-down validation.
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="$2"
      shift 2
      ;;
    --statefulset)
      STATEFULSET="$2"
      shift 2
      ;;
    --auth-secret)
      AUTH_SECRET="$2"
      shift 2
      ;;
    --timeout)
      TIMEOUT_SECONDS="$2"
      shift 2
      ;;
    --interval)
      INTERVAL_SECONDS="$2"
      shift 2
      ;;
    --stable-polls)
      STABLE_POLLS="$2"
      shift 2
      ;;
    --allow-former-nodes)
      ALLOW_FORMER_NODES="true"
      shift
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

require_command "${KUBECTL}"
require_command python3
require_command base64

expected_replicas="$("${KUBECTL}" -n "${NAMESPACE}" get statefulset "${STATEFULSET}" -o jsonpath='{.spec.replicas}')"
service_name="$("${KUBECTL}" -n "${NAMESPACE}" get statefulset "${STATEFULSET}" -o jsonpath='{.spec.serviceName}')"
tls_mount_path="$("${KUBECTL}" -n "${NAMESPACE}" get statefulset "${STATEFULSET}" -o jsonpath="{.spec.template.spec.containers[?(@.name==\"${CONTAINER}\")].volumeMounts[?(@.name==\"tls\")].mountPath}")"
username="$("${KUBECTL}" -n "${NAMESPACE}" get secret "${AUTH_SECRET}" -o jsonpath='{.data.username}' | base64 -d)"
password="$("${KUBECTL}" -n "${NAMESPACE}" get secret "${AUTH_SECRET}" -o jsonpath='{.data.password}' | base64 -d)"

if [[ -z "${expected_replicas}" || "${expected_replicas}" == "0" ]]; then
  echo "statefulset/${STATEFULSET} has no desired replicas" >&2
  exit 1
fi

if [[ -z "${service_name}" ]]; then
  echo "statefulset/${STATEFULSET} has no serviceName" >&2
  exit 1
fi

if [[ -z "${tls_mount_path}" ]]; then
  echo "statefulset/${STATEFULSET} does not define a tls volume mount for container ${CONTAINER}" >&2
  exit 1
fi

start_epoch="$(date +%s)"
first_ready_epoch=""
first_api_epoch=""
first_converged_epoch=""
stable_convergence_polls=0
attempt=0

print_stage_summary() {
  local label="$1"
  local epoch="$2"
  if [[ -n "${epoch}" ]]; then
    printf '  %s at +%ss\n' "${label}" "$(( epoch - start_epoch ))"
  else
    printf '  %s: not yet observed\n' "${label}"
  fi
}

while true; do
  attempt="$((attempt + 1))"
  now_epoch="$(date +%s)"
  elapsed="$((now_epoch - start_epoch))"

  if (( elapsed > TIMEOUT_SECONDS )); then
    echo
    echo "FAIL: timed out after ${TIMEOUT_SECONDS}s"
    print_stage_summary "pods ready" "${first_ready_epoch}"
    print_stage_summary "secured API reachable on all pods" "${first_api_epoch}"
    print_stage_summary "cluster converged on all pods" "${first_converged_epoch}"
    echo "  fallback diagnostic signal: use pod readiness plus per-pod API reachability only to distinguish startup from convergence lag"
    exit 1
  fi

  ready_count=0
  api_count=0
  converged_count=0
  node_lines=()

  echo
  echo "Attempt ${attempt} at +${elapsed}s"

  for ((ordinal = 0; ordinal < expected_replicas; ordinal++)); do
    pod="${STATEFULSET}-${ordinal}"
    host="${pod}.${service_name}.${NAMESPACE}.svc.cluster.local"
    ready_status="$("${KUBECTL}" -n "${NAMESPACE}" get pod "${pod}" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}' 2>/dev/null || true)"

    if [[ "${ready_status}" == "True" ]]; then
      ready_count="$((ready_count + 1))"
    fi

    summary_json=""
    api_ok="no"
    clustered="false"
    connected_to_cluster="false"
    connected_count="-"
    total_count="-"
    connected_nodes="-"
    failure_reason=""

    if summary_json="$(
      "${KUBECTL}" -n "${NAMESPACE}" exec "${pod}" -c "${CONTAINER}" -- \
        env NIFI_HOST="${host}" NIFI_USERNAME="${username}" NIFI_PASSWORD="${password}" TLS_CA_PATH="${tls_mount_path}/ca.crt" sh -ec '
          TOKEN=$(curl --silent --show-error --fail \
            --cacert "${TLS_CA_PATH}" \
            -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
            --data-urlencode "username=${NIFI_USERNAME}" \
            --data-urlencode "password=${NIFI_PASSWORD}" \
            "https://${NIFI_HOST}:8443/nifi-api/access/token")

          curl --silent --show-error --fail \
            --cacert "${TLS_CA_PATH}" \
            -H "Authorization: Bearer ${TOKEN}" \
            "https://${NIFI_HOST}:8443/nifi-api/flow/cluster/summary"
        ' 2>/dev/null
    )"; then
      api_ok="yes"
      api_count="$((api_count + 1))"
      parsed_summary="$(
        printf '%s' "${summary_json}" | python3 -c '
import json
import sys

data = json.load(sys.stdin)["clusterSummary"]
print("\t".join([
    str(data.get("clustered", False)).lower(),
    str(data.get("connectedToCluster", False)).lower(),
    str(data.get("connectedNodeCount", "")),
    str(data.get("totalNodeCount", "")),
    str(data.get("connectedNodes", "")),
]))
'
      )"
      IFS=$'\t' read -r clustered connected_to_cluster connected_count total_count connected_nodes <<< "${parsed_summary}"

      total_count_matches="false"
      if [[ "${ALLOW_FORMER_NODES}" == "true" ]]; then
        if [[ "${total_count}" =~ ^[0-9]+$ ]] && (( total_count >= expected_replicas )); then
          total_count_matches="true"
        fi
      elif [[ "${total_count}" == "${expected_replicas}" ]]; then
        total_count_matches="true"
      fi

      if [[ "${clustered}" == "true" && "${connected_to_cluster}" == "true" && "${connected_count}" == "${expected_replicas}" && "${total_count_matches}" == "true" ]]; then
        converged_count="$((converged_count + 1))"
      fi
    else
      failure_reason="api-unreachable"
    fi

    node_lines+=("  ${pod}: ready=${ready_status:-Unknown} api=${api_ok} clustered=${clustered} connected=${connected_to_cluster} nodes=${connected_nodes:-'-'} reason=${failure_reason:-none}")
  done

  echo "  pods ready: ${ready_count}/${expected_replicas}"
  echo "  secured API reachable: ${api_count}/${expected_replicas}"
  echo "  cluster converged: ${converged_count}/${expected_replicas}"
  printf '%s\n' "${node_lines[@]}"

  if (( ready_count == expected_replicas )) && [[ -z "${first_ready_epoch}" ]]; then
    first_ready_epoch="${now_epoch}"
  fi

  if (( api_count == expected_replicas )) && [[ -z "${first_api_epoch}" ]]; then
    first_api_epoch="${now_epoch}"
  fi

  if (( converged_count == expected_replicas )); then
    stable_convergence_polls="$((stable_convergence_polls + 1))"
    if [[ -z "${first_converged_epoch}" ]]; then
      first_converged_epoch="${now_epoch}"
    fi
  else
    stable_convergence_polls=0
  fi

  echo "  stable convergence polls: ${stable_convergence_polls}/${STABLE_POLLS}"

  if (( stable_convergence_polls >= STABLE_POLLS )); then
    echo
    echo "PASS: cluster health gate satisfied"
    print_stage_summary "pods ready" "${first_ready_epoch}"
    print_stage_summary "secured API reachable on all pods" "${first_api_epoch}"
    print_stage_summary "cluster converged on all pods" "${first_converged_epoch}"
    printf '  stable convergence satisfied at +%ss\n' "$(( now_epoch - start_epoch ))"
    exit 0
  fi

  sleep "${INTERVAL_SECONDS}"
done
