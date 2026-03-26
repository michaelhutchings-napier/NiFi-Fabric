# First Access and Day-1 Checks

Use this page right after the standard managed install through `charts/nifi-platform`.

The goal is simple:

- confirm the install is healthy
- log in once
- verify the basic Kubernetes, TLS, and NiFi signals look right

## Fast Path

For the standard managed cert-manager quickstart, start with:

```bash
make first-day-check \
  NAMESPACE=nifi \
  HELM_RELEASE=nifi \
  STATEFULSET_NAME=nifi \
  CLUSTER_NAME=nifi \
  MANAGED=true \
  CONTROLLER_NAMESPACE=nifi-system \
  CONTROLLER_DEPLOYMENT=nifi-controller-manager \
  SERVICE_NAME=nifi \
  TLS_PARAMS_SECRET=nifi-tls-params \
  CERTIFICATE=nifi
```

For standalone or explicit external-secret installs, omit `TLS_PARAMS_SECRET` and set `MANAGED=false`.

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
make first-day-check NAMESPACE=nifi HELM_RELEASE=nifi STATEFULSET_NAME=nifi CLUSTER_NAME=nifi MANAGED=true CONTROLLER_NAMESPACE=nifi-system CONTROLLER_DEPLOYMENT=nifi-controller-manager SERVICE_NAME=nifi TLS_PARAMS_SECRET=nifi-tls-params CERTIFICATE=nifi
helm -n nifi status nifi
kubectl -n nifi get nificluster nifi -o yaml
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=100
```

What good looks like:

- the helper reports `PASS`
- the Helm release is `deployed`
- the `NiFiCluster` does not show a degraded rollout
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
