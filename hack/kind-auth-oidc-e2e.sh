#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-nifi-fabric-auth-oidc}"
NAMESPACE="${NAMESPACE:-nifi}"
SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-nifi-system}"
HELM_RELEASE="${HELM_RELEASE:-nifi}"
CONTROLLER_IMAGE="${CONTROLLER_IMAGE:-nifi-fabric-controller:dev}"
NIFI_IMAGE="${NIFI_IMAGE:-apache/nifi:2.0.0}"
VERSION_VALUES_FILE="${VERSION_VALUES_FILE:-}"
KEYCLOAK_IMAGE="${KEYCLOAK_IMAGE:-quay.io/keycloak/keycloak:26.1}"
PROBE_IMAGE="${PROBE_IMAGE:-python:3.12-slim}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
START_EPOCH="$(date +%s)"
SKIP_KIND_BOOTSTRAP="${SKIP_KIND_BOOTSTRAP:-false}"
FAST_PROFILE="${FAST_PROFILE:-false}"
FAST_VALUES_FILE="${FAST_VALUES_FILE:-examples/test-fast-values.yaml}"
LOCALBIN="${LOCALBIN:-${ROOT_DIR}/bin}"
KUBECTL_VERSION="${KUBECTL_VERSION:-v1.31.0}"

KEYCLOAK_ADMIN_USER="${KEYCLOAK_ADMIN_USER:-admin}"
KEYCLOAK_ADMIN_PASSWORD="${KEYCLOAK_ADMIN_PASSWORD:-ChangeMeChangeMe1!}"
OIDC_CLIENT_ID="${OIDC_CLIENT_ID:-nifi-fabric}"
OIDC_CLIENT_SECRET="${OIDC_CLIENT_SECRET:-ChangeMeChangeMe1!}"
OIDC_ALICE_PASSWORD="${OIDC_ALICE_PASSWORD:-ChangeMeChangeMe1!}"
OIDC_BOB_PASSWORD="${OIDC_BOB_PASSWORD:-ChangeMeChangeMe1!}"
OIDC_CHARLIE_PASSWORD="${OIDC_CHARLIE_PASSWORD:-ChangeMeChangeMe1!}"

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
  if [[ "${SKIP_KIND_BOOTSTRAP}" != "true" ]]; then
    kubectl delete namespace "${SYSTEM_NAMESPACE}" --ignore-not-found --wait=true --timeout=5m >/dev/null 2>&1 || true
  fi
}

controller_ready() {
  kubectl -n "${SYSTEM_NAMESPACE}" get deployment/nifi-fabric-controller-manager >/dev/null 2>&1
}

wait_for_nifi_pod_ready() {
  wait_for "NiFi StatefulSet creation" 300 bash -ec '
    kubectl -n "'"${NAMESPACE}"'" get statefulset "'"${HELM_RELEASE}"'" >/dev/null 2>&1
  '

  local replicas
  replicas="$(kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}')"
  if [[ -z "${replicas}" || "${replicas}" == "0" ]]; then
    fail "NiFi StatefulSet ${HELM_RELEASE} has no desired replicas"
  fi

  wait_for "NiFi pod scheduling" 900 bash -ec '
    expected_replicas="'"${replicas}"'"
    for ordinal in $(seq 0 $(( expected_replicas - 1 ))); do
      host_ip="$(kubectl -n "'"${NAMESPACE}"'" get pod "'"${HELM_RELEASE}"'-${ordinal}" -o jsonpath="{.status.hostIP}" 2>/dev/null || true)"
      [[ -n "${host_ip}" ]] || exit 1
    done
  '

  wait_for "NiFi pod readiness" 900 bash -ec '
    expected_replicas="'"${replicas}"'"
    for ordinal in $(seq 0 $(( expected_replicas - 1 ))); do
      ready="$(kubectl -n "'"${NAMESPACE}"'" get pod "'"${HELM_RELEASE}"'-${ordinal}" -o jsonpath="{range .status.conditions[?(@.type==\"Ready\")]}{.status}{end}" 2>/dev/null || true)"
      [[ "${ready}" == "True" ]] || exit 1
    done
  '
}

wait_for_oidc_runtime_ready() {
  wait_for "NiFi OIDC authentication configuration endpoint" 300 bash -ec '
    kubectl -n "'"${NAMESPACE}"'" exec -c nifi "'"${HELM_RELEASE}"'-0" -- sh -ec "curl -skf https://'"${HELM_RELEASE}"'-0.'"${HELM_RELEASE}"'-headless.'"${NAMESPACE}"'.svc.cluster.local:8443/nifi-api/authentication/configuration >/dev/null"
  '
}

wait_for_oidc_discovery_from_nifi_pod() {
  local deadline=$(( $(date +%s) + 300 ))
  while true; do
    if kubectl -n "${NAMESPACE}" exec -c nifi "${HELM_RELEASE}-0" -- \
      curl -fsS "http://keycloak.${NAMESPACE}.svc.cluster.local:8080/realms/nifi/.well-known/openid-configuration" >/dev/null 2>&1; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      fail "OIDC discovery/config failure: NiFi could not reach the Keycloak discovery document over cluster DNS"
    fi
    sleep 5
  done
}

wait_for_probe_pod_ready() {
  wait_for "OIDC probe pod scheduling" 300 bash -ec '
    phase="$(kubectl -n "'"${NAMESPACE}"'" get pod oidc-probe -o jsonpath="{.status.phase}" 2>/dev/null || true)"
    [[ -n "${phase}" && "${phase}" != "Failed" ]]
  '

  wait_for "OIDC probe pod readiness" 300 bash -ec '
    ready="$(kubectl -n "'"${NAMESPACE}"'" get pod oidc-probe -o jsonpath="{range .status.conditions[?(@.type==\"Ready\")]}{.status}{end}" 2>/dev/null || true)"
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
  echo "==> OIDC diagnostics after failure at +$(elapsed)s"
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  kubectl config current-context || true
  kubectl get ns || true
  kubectl -n "${NAMESPACE}" get deployment,statefulset,pod,svc,secret,configmap,nificluster || true
  kubectl -n "${NAMESPACE}" get deployment keycloak -o yaml || true
  kubectl -n "${NAMESPACE}" get pods -l app=keycloak -o wide || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}' || true
  kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,UID:.metadata.uid,DEL:.metadata.deletionTimestamp || true
  kubectl -n "${NAMESPACE}" describe pods || true
  kubectl -n "${NAMESPACE}" logs deployment/keycloak --tail=300 || true
  kubectl -n "${NAMESPACE}" logs pod/oidc-probe --tail=300 || true
  kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi-fabric-controller-manager --tail=300 || true
  kubectl -n "${NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  kubectl -n "${SYSTEM_NAMESPACE}" get events --sort-by=.lastTimestamp | tail -n 100 || true
  nifi_exec sh -ec '
    echo "proxy host:"
    grep "^nifi.web.proxy.host=" /opt/nifi/nifi-current/conf/nifi.properties || true
    echo
    echo "oidc settings:"
    grep "^nifi.security.user.oidc" /opt/nifi/nifi-current/conf/nifi.properties || true
    echo
    echo "seeded groups:"
    grep "group identifier" /opt/nifi/nifi-current/conf/users.xml || true
  ' || true

  capture_cmd nificluster-yaml kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o yaml
  capture_cmd nificluster-status kubectl -n "${NAMESPACE}" get nificluster "${HELM_RELEASE}" -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
  capture_cmd statefulset-yaml kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o yaml
  capture_cmd pod-summary kubectl -n "${NAMESPACE}" get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,UID:.metadata.uid,DEL:.metadata.deletionTimestamp
  capture_cmd describe-pods kubectl -n "${NAMESPACE}" describe pods
  capture_cmd keycloak-logs kubectl -n "${NAMESPACE}" logs deployment/keycloak --tail=500
  capture_cmd oidc-probe-logs kubectl -n "${NAMESPACE}" logs pod/oidc-probe --tail=500
  capture_cmd controller-logs kubectl -n "${SYSTEM_NAMESPACE}" logs deployment/nifi-fabric-controller-manager --tail=500
  capture_cmd nifi-events bash -lc "kubectl -n '${NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 200"
  capture_cmd system-events bash -lc "kubectl -n '${SYSTEM_NAMESPACE}' get events --sort-by=.lastTimestamp | tail -n 200"
}

fail() {
  echo "FAIL: $*" >&2
  dump_diagnostics
  exit 1
}

trap 'fail "OIDC evaluator workflow aborted"' ERR

helm_values_args=(
  -f examples/managed/values.yaml
)

if [[ -n "${VERSION_VALUES_FILE}" ]]; then
  helm_values_args+=(-f "${VERSION_VALUES_FILE}")
fi

helm_values_args+=(
  -f examples/oidc-values.yaml
  -f examples/oidc-group-claims-values.yaml
  -f examples/oidc-kind-values.yaml
)

profile_label=""
if [[ "${FAST_PROFILE}" == "true" ]]; then
  helm_values_args+=(-f "${FAST_VALUES_FILE}")
  profile_label=" with fast profile"
fi

bootstrap_keycloak() {
  kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: keycloak-admin
type: Opaque
stringData:
  username: ${KEYCLOAK_ADMIN_USER}
  password: ${KEYCLOAK_ADMIN_PASSWORD}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: keycloak-realm
data:
  nifi-realm.json: |
    {
      "realm": "nifi",
      "enabled": true,
      "registrationAllowed": false,
      "clients": [
        {
          "clientId": "${OIDC_CLIENT_ID}",
          "name": "NiFi Fabric",
          "enabled": true,
          "protocol": "openid-connect",
          "publicClient": false,
          "secret": "${OIDC_CLIENT_SECRET}",
          "standardFlowEnabled": true,
          "directAccessGrantsEnabled": false,
          "redirectUris": [
            "https://${HELM_RELEASE}-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local:8443/*"
          ],
          "webOrigins": [
            "https://${HELM_RELEASE}-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local:8443"
          ],
          "protocolMappers": [
            {
              "name": "groups",
              "protocol": "openid-connect",
              "protocolMapper": "oidc-group-membership-mapper",
              "consentRequired": false,
              "config": {
                "full.path": "false",
                "id.token.claim": "true",
                "access.token.claim": "true",
                "userinfo.token.claim": "true",
                "claim.name": "groups"
              }
            }
          ]
        }
      ],
      "groups": [
        { "name": "nifi-platform-admins" },
        { "name": "nifi-flow-observers" }
      ],
      "users": [
        {
          "username": "alice",
          "enabled": true,
          "emailVerified": true,
          "firstName": "Alice",
          "lastName": "Admin",
          "email": "alice@example.com",
          "credentials": [
            { "type": "password", "value": "${OIDC_ALICE_PASSWORD}", "temporary": false }
          ],
          "groups": [ "nifi-platform-admins" ]
        },
        {
          "username": "bob",
          "enabled": true,
          "emailVerified": true,
          "firstName": "Bob",
          "lastName": "Observer",
          "email": "bob@example.com",
          "credentials": [
            { "type": "password", "value": "${OIDC_BOB_PASSWORD}", "temporary": false }
          ],
          "groups": [ "nifi-flow-observers" ]
        },
        {
          "username": "charlie",
          "enabled": true,
          "emailVerified": true,
          "firstName": "Charlie",
          "lastName": "Viewer",
          "email": "charlie@example.com",
          "credentials": [
            { "type": "password", "value": "${OIDC_CHARLIE_PASSWORD}", "temporary": false }
          ]
        }
      ]
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: keycloak
spec:
  replicas: 1
  selector:
    matchLabels:
      app: keycloak
  template:
    metadata:
      labels:
        app: keycloak
    spec:
      containers:
      - name: keycloak
        image: ${KEYCLOAK_IMAGE}
        args:
        - start-dev
        - --import-realm
        - --http-enabled=true
        - --hostname-strict=false
        env:
        - name: KEYCLOAK_ADMIN
          valueFrom:
            secretKeyRef:
              name: keycloak-admin
              key: username
        - name: KEYCLOAK_ADMIN_PASSWORD
          valueFrom:
            secretKeyRef:
              name: keycloak-admin
              key: password
        ports:
        - name: http
          containerPort: 8080
        readinessProbe:
          httpGet:
            path: /realms/nifi/.well-known/openid-configuration
            port: http
          initialDelaySeconds: 20
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 30
        volumeMounts:
        - name: realm
          mountPath: /opt/keycloak/data/import
      volumes:
      - name: realm
        configMap:
          name: keycloak-realm
---
apiVersion: v1
kind: Service
metadata:
  name: keycloak
spec:
  selector:
    app: keycloak
  ports:
  - name: http
    port: 8080
    targetPort: http
EOF

  kubectl -n "${NAMESPACE}" create secret generic nifi-oidc \
    --from-literal=clientSecret="${OIDC_CLIENT_SECRET}" \
    --dry-run=client -o yaml | kubectl apply -f -
}

run_oidc_probe() {
  kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: oidc-probe
spec:
  enableServiceLinks: false
  restartPolicy: Never
  containers:
  - name: python
    image: ${PROBE_IMAGE}
    command: ["sleep", "3600"]
EOF

  wait_for_probe_pod_ready

  kubectl -n "${NAMESPACE}" exec -i oidc-probe -- env \
    NIFI_BASE_URL="https://${HELM_RELEASE}-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local:8443" \
    NIFI_AUTH_CONFIG_URL="https://${HELM_RELEASE}-0.${HELM_RELEASE}-headless.${NAMESPACE}.svc.cluster.local:8443/nifi-api/authentication/configuration" \
    EXPECTED_REPLICAS="$(kubectl -n "${NAMESPACE}" get statefulset "${HELM_RELEASE}" -o jsonpath='{.spec.replicas}')" \
    python - <<'PY'
import html
import http.cookiejar
import json
import re
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ctx = ssl._create_unverified_context()
base_url = __import__("os").environ["NIFI_BASE_URL"]
auth_config_url = __import__("os").environ["NIFI_AUTH_CONFIG_URL"]
expected_replicas = int(__import__("os").environ["EXPECTED_REPLICAS"])

def build_opener():
    return urllib.request.build_opener(
        urllib.request.HTTPSHandler(context=ctx),
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
    )

def login(username, password):
    opener = build_opener()
    auth_config = json.loads(opener.open(auth_config_url, timeout=60).read().decode("utf-8"))
    start_url = auth_config["authenticationConfiguration"]["loginUri"]
    if not start_url:
        raise SystemExit(f"NiFi did not advertise an OIDC loginUri for {username}")

    response = opener.open(start_url, timeout=60)
    body = response.read().decode("utf-8", "ignore")
    final_url = response.geturl()
    if "kc-form-login" not in body and "login-actions/authenticate" not in final_url:
        raise SystemExit(f"expected Keycloak login page for {username}, got {final_url}")

    action_match = re.search(r'<form[^>]+id="kc-form-login"[^>]+action="([^"]+)"', body)
    if not action_match:
        raise SystemExit(f"could not find Keycloak login form action for {username}")

    form_values = {}
    for match in re.finditer(r'<input[^>]*name="([^"]+)"[^>]*value="([^"]*)"', body):
        form_values[match.group(1)] = html.unescape(match.group(2))

    form_values["username"] = username
    form_values["password"] = password
    form_action = urllib.parse.urljoin(final_url, html.unescape(action_match.group(1)))
    login_response = opener.open(form_action, data=urllib.parse.urlencode(form_values).encode("utf-8"), timeout=60)
    login_response.read()
    return opener

def login_and_check(username, password, allowed_path, expected_code):
    opener = login(username, password)
    request = urllib.request.Request(base_url + allowed_path)
    try:
      api_response = opener.open(request, timeout=60)
      code = api_response.getcode()
    except urllib.error.HTTPError as exc:
      code = exc.code

    if code != expected_code:
        raise SystemExit(f"{username} expected {expected_code} from {allowed_path}, got {code}")
    return code

def check_cluster_summary(username, password, expected_count):
    opener = login(username, password)
    summary = json.loads(opener.open(base_url + "/nifi-api/flow/cluster/summary", timeout=60).read().decode("utf-8"))["clusterSummary"]
    connected = int(summary.get("connectedNodeCount", -1))
    total = int(summary.get("totalNodeCount", -1))
    if connected != expected_count or total != expected_count:
        raise SystemExit(
            f"{username} expected connectedNodeCount=totalNodeCount={expected_count}, got connected={connected} total={total}"
        )
    return {"connectedNodeCount": connected, "totalNodeCount": total}

def wait_for_admin_ready(timeout=300):
    deadline = time.time() + timeout
    last_error = "admin readiness not yet observed"
    while time.time() < deadline:
        try:
            login_and_check("alice", "ChangeMeChangeMe1!", "/nifi-api/controller/config", 200)
            check_cluster_summary("alice", "ChangeMeChangeMe1!", expected_replicas)
            return
        except SystemExit as exc:
            last_error = str(exc)
            time.sleep(5)
    raise SystemExit(
        "wrong admin bootstrap or cluster not yet settled: "
        f"alice could not reach controller/config with a fully connected cluster within timeout; last error: {last_error}"
    )

wait_for_admin_ready()

results = {
    "alice_controller": login_and_check("alice", "ChangeMeChangeMe1!", "/nifi-api/controller/config", 200),
    "alice_cluster_summary": check_cluster_summary("alice", "ChangeMeChangeMe1!", expected_replicas),
    "alice_flow": login_and_check("alice", "ChangeMeChangeMe1!", "/nifi-api/flow/process-groups/root", 200),
    "bob_flow": login_and_check("bob", "ChangeMeChangeMe1!", "/nifi-api/flow/process-groups/root", 403),
    "bob_controller": login_and_check("bob", "ChangeMeChangeMe1!", "/nifi-api/controller/config", 403),
    "charlie_flow": login_and_check("charlie", "ChangeMeChangeMe1!", "/nifi-api/flow/process-groups/root", 403),
}
print(json.dumps(results, indent=2, sort_keys=True))
PY

  kubectl -n "${NAMESPACE}" delete pod oidc-probe --ignore-not-found >/dev/null 2>&1 || true
}

assert_nifi_property() {
  local expected_line="$1"
  local failure_message="$2"
  if ! nifi_exec sh -ec "grep -Fqx ${expected_line@Q} /opt/nifi/nifi-current/conf/nifi.properties"; then
    fail "${failure_message}"
  fi
}

assert_nifi_file_contains() {
  local file_path="$1"
  local needle="$2"
  local failure_message="$3"
  if ! nifi_exec sh -ec "grep -Fq ${needle@Q} ${file_path@Q}"; then
    fail "${failure_message}"
  fi
}

log_step "preparing cluster access for focused OIDC validation"
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
  run_make kind-load-nifi-image NIFI_IMAGE="${NIFI_IMAGE}"
fi

log_step "clearing any prior NiFi-Fabric install in the target namespaces"
cleanup_namespaces

log_step "creating baseline TLS material"
NIFI_IMAGE="${NIFI_IMAGE}" bash "${ROOT_DIR}/hack/create-kind-secrets.sh" "${NAMESPACE}" "${HELM_RELEASE}" nifi-tls nifi-auth

log_step "bootstrapping Keycloak"
bootstrap_keycloak
kubectl -n "${NAMESPACE}" rollout status deployment/keycloak --timeout=10m

log_step "ensuring the controller and CRD"
run_make install-crd
if [[ "${SKIP_KIND_BOOTSTRAP}" == "true" ]] && controller_ready; then
  printf '    reusing existing controller deployment\n'
else
  run_make docker-build-controller
  run_make kind-load-controller
  run_make deploy-controller
fi
kubectl -n "${SYSTEM_NAMESPACE}" rollout status deployment/nifi-fabric-controller-manager --timeout=5m

log_step "installing NiFi in oidc + externalClaimGroups mode${profile_label}"
(cd "${ROOT_DIR}" && helm upgrade --install "${HELM_RELEASE}" charts/nifi \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  "${helm_values_args[@]}")
kubectl apply -f "${ROOT_DIR}/examples/managed/nificluster.yaml"
wait_for_nifi_pod_ready
wait_for_oidc_runtime_ready

log_step "verifying OIDC runtime wiring inside the NiFi pod"
assert_nifi_property \
  "nifi.security.user.oidc.discovery.url=http://keycloak.${NAMESPACE}.svc.cluster.local:8080/realms/nifi/.well-known/openid-configuration" \
  "OIDC discovery/config failure: NiFi did not render the expected in-cluster Keycloak discovery URL"
assert_nifi_property \
  "nifi.security.user.oidc.client.id=${OIDC_CLIENT_ID}" \
  "OIDC discovery/config failure: NiFi did not render the expected OIDC client id"
assert_nifi_property \
  "nifi.security.user.oidc.claim.identifying.user=email" \
  "wrong claim mapping: NiFi did not render the expected identifying user claim"
assert_nifi_property \
  "nifi.security.user.oidc.claim.groups=groups" \
  "wrong claim mapping: NiFi did not render the expected groups claim"
assert_nifi_file_contains \
  "/opt/nifi/nifi-current/conf/users.xml" \
  "nifi-platform-admins" \
  "wrong admin bootstrap: the seeded NiFi application groups do not include nifi-platform-admins"
assert_nifi_file_contains \
  "/opt/nifi/nifi-current/conf/users.xml" \
  "nifi-flow-observers" \
  "wrong claim mapping: the seeded NiFi application groups do not include nifi-flow-observers"
if ! nifi_exec test -f /opt/nifi/nifi-current/conf/authorizations.xml; then
  fail "wrong admin bootstrap: NiFi did not generate authorizations.xml"
fi

log_step "verifying OIDC discovery from the NiFi pod"
wait_for_oidc_discovery_from_nifi_pod

if ! nifi_exec sh -ec 'if grep -q "^nifi.web.proxy.host=" /opt/nifi/nifi-current/conf/nifi.properties; then exit 1; fi'; then
  fail "wrong proxy-host / external URL assumptions: the focused in-cluster OIDC path should not set nifi.web.proxy.host"
fi

log_step "running in-cluster OIDC login and group-based authorization checks"
run_oidc_probe

echo
echo "PASS: focused OIDC auth workflow completed successfully in +$(elapsed)s"
  echo "  Keycloak bootstrap"
echo "  NiFi managed install in oidc + externalClaimGroups mode on ${NIFI_IMAGE}"
echo "  OIDC discovery and group-claim runtime wiring"
echo "  Initial Admin Identity bootstrap fallback and non-admin denial checks"
