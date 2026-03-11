#!/usr/bin/env bash

set -euo pipefail

cluster_name="${1:?cluster name is required}"
image="${2:?image is required}"
attempts="${3:-3}"
image_ref="docker.io/${image}"

if docker exec "${cluster_name}-control-plane" ctr -n k8s.io images ls -q | grep -Fx "${image_ref}" >/dev/null 2>&1; then
  echo "kind cluster ${cluster_name} already has ${image_ref}"
  exit 0
fi

for attempt in $(seq 1 "${attempts}"); do
  if docker exec "${cluster_name}-control-plane" ctr -n k8s.io images pull --platform linux/amd64 "${image_ref}"; then
    exit 0
  fi

  if [[ "${attempt}" -eq "${attempts}" ]]; then
    echo "failed to preload ${image} into kind cluster ${cluster_name} after ${attempts} attempts" >&2
    exit 1
  fi

  echo "retrying kind image preload for ${image} (${attempt}/${attempts})" >&2
  sleep 5
done
