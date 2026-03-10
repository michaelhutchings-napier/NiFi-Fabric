#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-auth-ldap}"
NAMESPACE="${NAMESPACE:-nifi}"
SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-nifi-system}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_IMAGE="${CONTROLLER_IMAGE:-nifi-fabric-controller:dev}"
LDAP_IMAGE="${LDAP_IMAGE:-osixia/openldap:1.5.0}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
START_EPOCH="$(date +%s)"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
LOCALBIN="${LOCALBIN:-${ROOT_DIR}/bin}"
KUBECTL_VERSION="${KUBECTL_VERSION:-v1.31.0}"

LDAP_ADMIN_PASSWORD="${LDAP_ADMIN_PASSWORD:-ChangeMeChangeMe1!}"
LDAP_USER_PASSWORD="${LDAP_USER_PASSWORD:-ChangeMeChangeMe1!}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

ensure_kubectl() {
  mkdir -p "${LOCALBIN}"
  if timeout 5s kubectl version --client=true --output=yaml >/dev/null 2>&1; then
    return 0
  fi

  curl -fsSL -o "${LOCALBIN}/kubectl" "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl"
  chmod +x "${LOCALBIN}/kubectl"
  export PATH="${LOCALBIN}:${PATH}"

  if ! timeout 5s kubectl version --client=true --output=yaml >/dev/null 2>&1; then
    echo "failed to provision a working kubectl binary" >&2
    exit 1
  fi
}

log_step() {
  printf '\n==> %s\n' "$1"
}

elapsed() {
  echo "$(( $(date +%s) - START_EPOCH ))"
}

run_make() {
  (cd "${ROOT_DIR}" && KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" make "$@")
}

capture_cmd() {
  local name="$1"
  shift

  if [[ -z "${ARTIFACT_DIR}" ]]; then
    return 0
  fi

  mkdir -p "${ARTIFACT_DIR}"
  {
    echo "### ${name}"
    "$@"
  } >"${ARTIFACT_DIR}/${name}.log" 2>&1 || true
}

wait_for() {
  local description="$1"
  local timeout="$2"
  shift 2

  local deadline=$(( $(date +%s) + timeout ))
  while true; do
    if "$@"; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      fail "${description} timed out"
    fi
    sleep 5
  done
}

cleanup_namespaces() {
  kubectl delete namespace "${NAMESPACE}" --ignore-not-found --wait=true --timeout=5m >/dev/null 2>&1 || true
  kubectl delete namespace "${SYSTEM_NAMESPACE}" --ignore-not-found --wait=true --timeout=5m >/dev/null 2>&1 || true
}

wait_for_nifi_pod_ready() {
  wait_for "NiFi pod scheduling" 900 bash -ec '
    host_ip="$(kubectl -n "'"${NAMESPACE}"'" get pod "'"${HELM_RELEASE}"'-0" -o jsonpath="{.status.hostIP}" 2>/dev/null || true)"
    [[ -n "${host_ip}" ]]
  '

  wait_for "NiFi pod readiness" 900 bash -ec '
    ready="$(kubectl -n "'"${NAMESPACE}"'" get pod "'"${HELM_RELEASE}"'-0" -o jsonpath="{range .status.conditions[?(@.type==\"Ready\")]}{.status}{end}" 2>/dev/null || true)"
    [[ "${ready}" == "True" ]]
  '
}

nifi_exec() {
  local attempt output rc

  for attempt in $(seq 1 24); do
    if output="$(kubectl -n "${NAMESPACE}" exec -c nifi "${HELM_RELEASE}-0" -- "$@" 2>&1)"; then
      printf '%s' "${output}"
      return 0
    fi
    rc=$?
    if grep -Eq 'container not found|unable to upgrade connection|container .* is not running' <<<"${output}"; then
      sleep 5
      continue
    fi
    printf '%s\n' "${output}" >&2
    return "${rc}"
  done

  printf '%s\n' "${output}" >&2
  return 1
}

dump_diagnostics() {
  set +e
  echo
  echo "==> LDAP diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl get ns || true
  kubectl -n "${NAMESPACE}" get deployment,statefulset,pod,svc,secret,configmap,nificluster || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,UID:.metadata.uid,DEL:.metadata.deletionTimestamp || true
  kubectl -n "${NAMESPACE}" describe pods || true
  kubectl -n "${NAMESPACE}" logs deployment/ldap --tail=300 || true
  kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi-fabric-controller-manager --tail=300 || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${SYSTEM_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  nifi_exec sh -ec '
    echo "ldap provider settings:"
    grep -n "ldap-provider" /opt/nifi/nifi-current/conf/login-identity-providers.xml || true
    grep -n "ldap-user-group-provider" /opt/nifi/nifi-current/conf/authorizers.xml || true
    echo
    echo "seeded groups:"
    grep "group identifier" /opt/nifi/nifi-current/conf/users.xml || true
  ' || true

  capture_cmd nificluster-yaml kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml
  capture_cmd nificluster-status kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
  capture_cmd statefulset-yaml kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml
  capture_cmd pod-summary kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,UID:.metadata.uid,DEL:.metadata.deletionTimestamp
  capture_cmd describe-pods kubectl -n "${NAMESPACE}" describe pods
  capture_cmd ldap-logs kubectl -n "${NAMESPACE}" logs deployment/ldap --tail=500
  capture_cmd controller-logs kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi-fabric-controller-manager --tail=500
  capture_cmd nifi-events bash -lc "kubectl -n '${NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 200"
  capture_cmd system-events bash -lc "kubectl -n '${SYSTEM_NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 200"
}

fail() {
  echo "FAIL: $*" >&2
  dump_diagnostics
  exit 1
}

trap 'fail "LDAP evaluator workflow aborted"' ERR

bootstrap_ldap() {
  kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ldap
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ldap
  template:
    metadata:
      labels:
        app: ldap
    spec:
      enableServiceLinks: false
      containers:
      - name: ldap
        image: ${LDAP_IMAGE}
        env:
        - name: LDAP_ORGANISATION
          value: NiFi Fabric
        - name: LDAP_DOMAIN
          value: example.com
        - name: LDAP_ADMIN_PASSWORD
          value: ${LDAP_ADMIN_PASSWORD}
        - name: LDAP_TLS
          value: "false"
        ports:
        - name: ldap
          containerPort: 389
        readinessProbe:
          tcpSocket:
            port: ldap
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 24
---
apiVersion: v1
kind: Service
metadata:
  name: ldap
spec:
  selector:
    app: ldap
  ports:
  - name: ldap
    port: 389
    targetPort: ldap
EOF

  kubectl -n "${NAMESPACE}" create secret generic nifi-ldap-bind \
    --from-literal=managerDn='cn=admin,dc=example,dc=com' \
    --from-literal=managerPassword="${LDAP_ADMIN_PASSWORD}" \
    --dry-run=client -o yaml | kubectl apply -f -

  kubectl -n "${NAMESPACE}" create secret generic nifi-auth \
    --from-literal=username='alice' \
    --from-literal=password="${LDAP_USER_PASSWORD}" \
    --dry-run=client -o yaml | kubectl apply -f -
}

seed_ldap() {
  kubectl -n "${NAMESPACE}" exec deployment/ldap -- sh -ec '
    cat >/tmp/nifi-seed.ldif <<'"'"'LDIF'"'"'
dn: ou=People,dc=example,dc=com
objectClass: organizationalUnit
ou: People

dn: ou=Groups,dc=example,dc=com
objectClass: organizationalUnit
ou: Groups

dn: uid=alice,ou=People,dc=example,dc=com
objectClass: inetOrgPerson
cn: Alice Admin
sn: Admin
uid: alice
mail: alice@example.com
userPassword: '"${LDAP_USER_PASSWORD}"'

dn: uid=bob,ou=People,dc=example,dc=com
objectClass: inetOrgPerson
cn: Bob Operator
sn: Operator
uid: bob
mail: bob@example.com
userPassword: '"${LDAP_USER_PASSWORD}"'

dn: uid=charlie,ou=People,dc=example,dc=com
objectClass: inetOrgPerson
cn: Charlie Viewer
sn: Viewer
uid: charlie
mail: charlie@example.com
userPassword: '"${LDAP_USER_PASSWORD}"'

dn: cn=nifi-platform-admins,ou=Groups,dc=example,dc=com
objectClass: groupOfNames
cn: nifi-platform-admins
member: uid=alice,ou=People,dc=example,dc=com

dn: cn=nifi-flow-operators,ou=Groups,dc=example,dc=com
objectClass: groupOfNames
cn: nifi-flow-operators
member: uid=bob,ou=People,dc=example,dc=com
LDIF
    ldapadd -x -H ldap://127.0.0.1:389 -D "cn=admin,dc=example,dc=com" -w "'"${LDAP_ADMIN_PASSWORD}"'" -f /tmp/nifi-seed.ldif
  '
}

expect_nifi_api_code() {
  local username="$1"
  local password="$2"
  local path="$3"
  local expected="$4"

  local code
  code="$(nifi_exec env \
    NIFI_USERNAME="${username}" \
    NIFI_PASSWORD="${password}" \
    NIFI_PATH="${path}" \
    sh -ec '
      TOKEN=$(curl --silent --show-error --fail \
        --cacert /opt/nifi/tls/ca.crt \
        -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
        --data-urlencode "username=${NIFI_USERNAME}" \
        --data-urlencode "password=${NIFI_PASSWORD}" \
        "https://'${HELM_RELEASE}'-0.'${HELM_RELEASE}'-headless.'${NAMESPACE}'.svc.cluster.local:8443/nifi-api/access/token")
      curl --silent --show-error \
        --output /tmp/nifi-auth-check.out \
        --write-out "%{http_code}" \
        --cacert /opt/nifi/tls/ca.crt \
        -H "Authorization: Bearer ${TOKEN}" \
        "https://'${HELM_RELEASE}'-0.'${HELM_RELEASE}'-headless.'${NAMESPACE}'.svc.cluster.local:8443${NIFI_PATH}"
    ')" || return 1

  if [[ "${code}" != "${expected}" ]]; then
    fail "expected ${expected} from ${path} for ${username}, got ${code}"
  fi
}

log_step "preparing cluster access for focused LDAP validation"
require_command kind
require_command helm
require_command docker
require_command go
ensure_kubectl
if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]]; then
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null
else
  run_make kind-down || true
  run_make kind-up

  log_step "preloading the NiFi runtime image into kind"
  run_make kind-load-nifi-image
fi

log_step "clearing any prior NiFi-Fabric install in the target namespaces"
cleanup_namespaces

log_step "creating baseline TLS material"
bash "${ROOT_DIR}/hack/create-kind-secrets.sh" "${NAMESPACE}" "${HELM_RELEASE}" nifi-tls nifi-auth

log_step "bootstrapping LDAP"
bootstrap_ldap
kubectl -n "${NAMESPACE}" rollout status deployment/ldap --timeout=10m
seed_ldap

log_step "installing the controller and CRD"
run_make install-crd
run_make docker-build-controller
run_make kind-load-controller
run_make deploy-controller
kubectl -n "${SYSTEM_NAMESPACE}" rollout status deployment/nifi-fabric-controller-manager --timeout=5m

log_step "installing NiFi in ldap + ldapSync mode"
(cd "${ROOT_DIR}" && helm upgrade --install "${HELM_RELEASE}" charts/nifi \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  -f examples/managed/values.yaml \
  -f examples/ldap-values.yaml \
  -f examples/ldap-kind-values.yaml)
kubectl apply -f "${ROOT_DIR}/examples/managed/nificluster.yaml"
wait_for_nifi_pod_ready

log_step "verifying LDAP runtime wiring inside the NiFi pod"
nifi_exec sh -ec '
  grep -q "ldap-provider" /opt/nifi/nifi-current/conf/login-identity-providers.xml
  grep -q "ldap-user-group-provider" /opt/nifi/nifi-current/conf/authorizers.xml
  grep -q "Initial Admin Identity\">alice<" /opt/nifi/nifi-current/conf/authorizers.xml
  grep -q "<property name=\"Identity Strategy\">USE_USERNAME</property>" /opt/nifi/nifi-current/conf/login-identity-providers.xml
'

log_step "verifying NiFi health with LDAP credentials"
(cd "${ROOT_DIR}" && KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" make kind-health)

log_step "running LDAP login and group-based authorization checks"
expect_nifi_api_code alice "${LDAP_USER_PASSWORD}" /nifi-api/controller/config 200
expect_nifi_api_code bob "${LDAP_USER_PASSWORD}" /nifi-api/flow/process-groups/root 403
expect_nifi_api_code bob "${LDAP_USER_PASSWORD}" /nifi-api/controller/config 403
expect_nifi_api_code charlie "${LDAP_USER_PASSWORD}" /nifi-api/flow/process-groups/root 403

echo
echo "PASS: focused LDAP auth workflow completed successfully in +$(elapsed)s"
echo "  LDAP bootstrap"
echo "  NiFi managed install in ldap + ldapSync mode"
echo "  LDAP login provider and user-group provider wiring"
echo "  initial admin identity bootstrap plus authenticated non-admin denial checks"
