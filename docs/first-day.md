# First Access and Day-1 Checks

Use this page right after the standard managed install through `charts/nifi-platform`.

The goal is simple:

- confirm the install is healthy
- log in once
- verify the basic Kubernetes, TLS, and NiFi signals look right

## Fast Path

For the standard managed cert-manager quickstart, start with:

```bash
kubectl -n nifi get nificluster,statefulset,pods,svc,pvc
kubectl -n nifi get certificate,secret
kubectl -n nifi-system get deployment,pods
helm -n nifi status nifi
```

For standalone installs, use:

```bash
kubectl -n nifi get statefulset,pods,svc,pvc,secret
helm -n nifi status nifi
```

If that standalone install uses cert-manager, also run:

```bash
kubectl -n nifi get certificate
```

If you are working from this repository, `make first-day-check` is still available as an optional wrapper around the same day-1 checks.

## 1. Check The Core Resources

```bash
kubectl -n nifi get nificluster,statefulset,pods,svc,pvc
kubectl -n nifi-system get deployment,pods
kubectl -n cert-manager get pods
```

What good looks like:

- the `nifi-controller-manager` deployment is `Ready`
- the NiFi `StatefulSet` is `Ready`
- NiFi pods are `Running`
- NiFi PVCs are `Bound`

## 2. Check TLS And Auth Bootstrap

```bash
kubectl -n nifi get certificate,secret
kubectl -n nifi get secret nifi-auth -o jsonpath='{.data.username}' | base64 -d; echo
kubectl -n nifi get secret nifi-auth -o jsonpath='{.data.password}' | base64 -d; echo
```

What good looks like:

- the workload `Certificate` is `Ready`
- the `nifi-tls` Secret exists
- the `nifi-auth` Secret exists and returns credentials

## 3. Access NiFi For The First Time

For a first local check, use port-forward:

```bash
kubectl -n nifi port-forward svc/nifi 8443:8443
```

Then open:

```text
https://localhost:8443/nifi
```

Log in with the username and password from `Secret/nifi-auth`.

## 4. Run A Short Health Check

```bash
helm -n nifi status nifi
kubectl -n nifi get nificluster nifi -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}{"\n"}{end}'
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.tls.phase}{" "}{.status.tls.reason}{"\n"}'
kubectl -n nifi get nificluster nifi -o yaml
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=100
```

What good looks like:

- the Helm release is `deployed`
- `Available=True`, `SecretsReady=True`, and `TLSMaterialReady=True`
- `status.tls.phase` is usually `Idle` once the initial rollout settles
- the controller logs do not show repeated reconciliation failures

## 5. Optional First Metrics Check

If you enabled metrics, confirm the rendered objects exist:

```bash
kubectl -n nifi get service,servicemonitor
```

## When To Dig Deeper

If one of the checks above fails, go to:

- [Operations and Troubleshooting](operations.md)
- [TLS and cert-manager](manage/tls-and-cert-manager.md)
- [Authentication](manage/authentication.md)
