# Operations and Troubleshooting

This page is the fast path for common operational checks.

## Common Checks

Helm release status:

```bash
helm -n nifi status nifi
```

Platform resources:

```bash
kubectl -n nifi get nificluster,statefulset,pods,svc
kubectl -n nifi-system get deployment,pods
```

NiFiCluster status:

```bash
kubectl -n nifi get nificluster nifi -o yaml
```

Controller logs:

```bash
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

## Common Failure Domains

### Install Prerequisites

Check:

- controller image is reachable from the cluster
- `Secret/nifi-tls` exists when using external TLS
- `Secret/nifi-auth` exists
- cert-manager exists when using cert-manager mode

### Cluster Health

Symptoms:

- pods are running but NiFi is not converged
- rollout or restore remains in progress

Check:

- pod readiness
- NiFiCluster conditions
- recent events in `nifi` and `nifi-system`

### Authentication

Symptoms:

- UI login fails
- OIDC redirect or LDAP bind fails

Check:

- auth mode values
- client secret or LDAP manager Secret references
- `web.proxyHosts` when external browser-facing access is involved

### Metrics

Symptoms:

- `ServiceMonitor` exists but scraping fails
- exporter `/metrics` is unhealthy

Check:

- machine-auth Secret contents
- metrics CA Secret contents
- selected metrics mode
- exporter pod logs when exporter mode is enabled

### Autoscaling

Symptoms:

- recommendation exists but scale does not happen
- scale-down remains blocked

Check:

- `status.autoscaling`
- lifecycle precedence conditions
- cooldown and stabilization windows
- blocked or failure reasons on execution state

## Support Boundary

NiFi-Fabric is intentionally conservative about support claims:

- kind is the runtime proof baseline in this repository
- AKS and OpenShift guidance is published separately and remains conservative
- experimental features are clearly marked and should be treated differently from the standard platform path
