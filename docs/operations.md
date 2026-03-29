# Operations and Troubleshooting

NiFi-Fabric includes a small starter operations package for day-two use.

## What You Get

- starter dashboards
- starter alert rules
- planned-change playbooks
- starter runbooks
- basic troubleshooting steps for the standard managed install

## What This Page Is For

Use this page when you want to:

- check the health of the standard managed install
- find the right status, logs, and Kubernetes objects to inspect
- hand the platform off to an operations team with a reasonable starting point

## Start Here

Helpful first links:

- [Operations playbooks](operations/playbooks.md)
- [Log shipping](operations/log-shipping.md)
- [Starter dashboards](operations/dashboards.md)
- [Starter alerts](operations/alerts.md)
- [Starter runbooks](operations/runbooks.md)
- [Backup, Restore, and Disaster Recovery](dr.md)

## Fast Checks

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

## Operator Notes

The included playbooks and runbooks do different jobs:

- use [Operations playbooks](operations/playbooks.md) for planned changes such as upgrades or declared flow version changes
- use [Starter runbooks](operations/runbooks.md) when the cluster is already blocked, failed, or degraded
- use [Backup, Restore, and Disaster Recovery](dr.md) when you need the operator recovery model, recovery boundaries, or the control-plane export and recovery helpers

The included dashboards, alerts, playbooks, and runbooks are starter material. Most teams will still adapt:

- Prometheus and Grafana conventions
- namespace and label choices
- alert routing and severity
- environment-specific troubleshooting around ingress, storage, and identity systems
