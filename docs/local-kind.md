# Local Kind Workflow

This repository now includes a concrete local workflow for bringing up a minimal NiFi 2 cluster on kind using the standalone Helm chart.

## What This Flow Does

- creates a kind cluster
- creates a PKCS12 TLS Secret and a single-user auth Secret
- installs the standalone chart with kind-friendly values
- validates the cluster through the NiFi API over a local port-forward

This flow is intentionally local-development oriented. It is not a production deployment guide.

## Prerequisites

- Docker
- kind
- kubectl
- Helm 3
- openssl
- keytool

## Exact Commands

```bash
make kind-up
make kind-secrets
make helm-install-standalone
kubectl -n nifi rollout status statefulset/nifi --timeout=20m
kubectl -n nifi get pods
kubectl -n nifi get leases,configmaps | rg '^nifi'
```

```bash
kubectl -n nifi exec nifi-0 -c nifi -- sh -ec '
TOKEN=$(curl --silent --show-error --fail \
  --cacert /opt/nifi/tls/ca.crt \
  -H "Content-Type: application/x-www-form-urlencoded; charset=UTF-8" \
  --data-urlencode "username=admin" \
  --data-urlencode "password=ChangeMeChangeMe1!" \
  https://nifi-0.nifi-headless.nifi.svc.cluster.local:8443/nifi-api/access/token)

curl --silent --show-error --fail \
  --cacert /opt/nifi/tls/ca.crt \
  -H "Authorization: Bearer ${TOKEN}" \
  https://nifi-0.nifi-headless.nifi.svc.cluster.local:8443/nifi-api/flow/cluster/summary
'
```

## Expected Secrets

`make kind-secrets` calls `hack/create-kind-secrets.sh` and creates:

- `Secret/nifi-tls`
- `Secret/nifi-auth`

The TLS Secret contains:

- `keystore.p12`
- `truststore.p12`
- `ca.crt`
- `keystorePassword`
- `truststorePassword`
- `sensitivePropsKey`

The auth Secret contains:

- `username`
- `password`

## Notes

- The helper script uses a self-signed CA and a server certificate valid for the chart Service and headless Service DNS names.
- The helper script creates both PKCS12 stores. If the workstation does not have `keytool`, it runs `keytool` in a disposable `apache/nifi:2.0.0` container.
- The local API example executes `curl` inside `nifi-0` so the TLS hostname and the NiFi node identity stay aligned.
- The kind-focused standalone example leaves `nifi.security.autoreload.enabled=false` for now; cert rotation policy is still a later slice.
- The API example uses the exported `ca.crt` rather than `curl -k`.
- Managed mode still renders, but advanced controller-driven rollout behavior is not implemented yet.
