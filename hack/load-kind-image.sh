#!/usr/bin/env bash

set -euo pipefail

cluster_name="${1:?cluster name is required}"
image="${2:?image is required}"
attempts="${3:-3}"

if [[ "${image}" != */* ]]; then
  image_ref="docker.io/library/${image}"
elif [[ "${image}" == *"/"* ]]; then
  first_component="${image%%/*}"

  if [[ "${first_component}" == "localhost" || "${first_component}" == *.* || "${first_component}" == *:* ]]; then
    image_ref="${image}"
  else
    image_ref="docker.io/${image}"
  fi
fi

kind_nodes() {
  kind get nodes --name "${cluster_name}"
}

node_has_image() {
  local node_name="$1"
  docker exec "${node_name}" ctr -n k8s.io images ls -q | grep -Fx "${image_ref}" >/dev/null 2>&1
}

cluster_has_image() {
  local node_name=""
  while IFS= read -r node_name; do
    if [[ -z "${node_name}" ]]; then
      continue
    fi
    if ! node_has_image "${node_name}"; then
      return 1
    fi
  done < <(kind_nodes)

  return 0
}

load_with_kind() {
  local local_ref="$1"
  kind load docker-image --name "${cluster_name}" "${local_ref}" >/dev/null
}

load_with_ctr_import_fallback() {
  local local_ref="$1"
  local archive_path=""
  local node_name=""

  archive_path="$(mktemp "${TMPDIR:-/tmp}/kind-image-${cluster_name}.XXXXXX.tar")"
  trap 'rm -f "${archive_path}"' RETURN
  docker image save "${local_ref}" -o "${archive_path}"

  while IFS= read -r node_name; do
    if [[ -z "${node_name}" ]]; then
      continue
    fi
    if node_has_image "${node_name}"; then
      continue
    fi

    cat "${archive_path}" | docker exec -i "${node_name}" ctr --namespace=k8s.io images import --digests --snapshotter=overlayfs - >/dev/null
  done < <(kind_nodes)

  trap - RETURN
  rm -f "${archive_path}"
}

load_local_ref() {
  local local_ref="$1"

  if load_with_kind "${local_ref}"; then
    echo "loaded ${local_ref} into kind cluster ${cluster_name} from local docker cache"
    return 0
  fi

  echo "kind load docker-image failed for ${local_ref}; retrying via docker save | ctr import fallback" >&2
  load_with_ctr_import_fallback "${local_ref}"
  echo "loaded ${local_ref} into kind cluster ${cluster_name} via ctr import fallback"
}

if cluster_has_image; then
  echo "kind cluster ${cluster_name} already has ${image_ref}"
  exit 0
fi

for local_ref in "${image_ref}" "${image}"; do
  if docker image inspect "${local_ref}" >/dev/null 2>&1; then
    load_local_ref "${local_ref}"
    exit 0
  fi
done

if docker pull "${image_ref}" >/dev/null 2>&1; then
  load_local_ref "${image_ref}"
  echo "pulled ${image_ref} into local docker cache and loaded it into kind cluster ${cluster_name}"
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
