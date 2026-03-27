#!/usr/bin/env bash

set -euo pipefail

namespace="nifi"
release="nifi"
audit_archive_dir="/opt/nifi/nifi-current/database_repository/flow-audit-archive"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      namespace="$2"
      shift 2
      ;;
    --release)
      release="$2"
      shift 2
      ;;
    --audit-archive-dir)
      audit_archive_dir="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if ! command -v kubectl >/dev/null 2>&1; then
  echo "missing required command: kubectl" >&2
  exit 1
fi

pod="${release}-0"
properties="$(kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- cat /opt/nifi/nifi-current/conf/nifi.properties)"
init_containers="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{range .spec.initContainers[*]}{.name}{"\n"}{end}')"

for want in \
  'nifi.database.directory=./database_repository' \
  'nifi.flow.configuration.archive.enabled=true' \
  "nifi.flow.configuration.archive.dir=${audit_archive_dir}" \
  'nifi.flow.configuration.archive.max.time=30 days' \
  'nifi.flow.configuration.archive.max.storage=2 GB' \
  'nifi.flow.configuration.archive.max.count=1000' \
  'nifi.web.request.log.format=%{client}a - %u %t "%r" %s %O "%{Referer}i" "%{User-Agent}i"'
do
  if ! grep -Fq -- "${want}" <<<"${properties}"; then
    echo "expected local audit property missing from runtime nifi.properties: ${want}" >&2
    exit 1
  fi
done

for unexpected in \
  'nifi.flow.action.reporter.implementation=' \
  'nifi.nar.library.directory.flow.action.audit='
do
  if grep -Fq -- "${unexpected}" <<<"${properties}"; then
    echo "unexpected reporter wiring present during local-only stage: ${unexpected}" >&2
    exit 1
  fi
done

if grep -Fxq 'install-flow-action-audit-reporter' <<<"${init_containers}"; then
  echo "unexpected audit reporter init container present during local-only stage" >&2
  exit 1
fi

kubectl -n "${namespace}" exec -i -c nifi "${pod}" -- sh -ec "test -d '${audit_archive_dir}'"

echo "verified local-only audit layer on ${pod}"
