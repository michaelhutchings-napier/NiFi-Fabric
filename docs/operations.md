# Operations and Troubleshooting

This is the customer-facing starter operations package for NiFi-Fabric.

It stays intentionally small:

- one starter Grafana dashboard
- one starter Prometheus alert rules file
- concise runbooks for the most common platform failure modes

What it covers:

- controller lifecycle signals for rollout, TLS drift, hibernation, restore, and autoscaling
- metrics subsystem checks for the currently supported metrics modes
- operator-facing troubleshooting steps built around `NiFiCluster` status, controller events, controller metrics, and chart-owned metrics resources

What operators still need to adapt:

- Prometheus scrape job labels, namespaces, and alert routing
- Grafana datasource names and dashboard folder conventions
- severity thresholds, notification policies, and maintenance silences
- any environment-specific dashboards for ingress, storage, cloud load balancers, or external identity systems

## Included Assets

- [Starter dashboards](operations/dashboards.md)
- [Starter alerts](operations/alerts.md)
- [Starter runbooks](operations/runbooks.md)
- [Grafana starter dashboard JSON](../ops/grafana/nifi-fabric-starter-dashboard.json)
- [Prometheus starter alert rules YAML](../ops/prometheus/nifi-fabric-starter-alerts.yaml)

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

Controller metrics quick check:

```bash
kubectl -n nifi-system port-forward deployment/nifi-controller-manager 18080:8080
curl --silent http://127.0.0.1:18080/metrics | rg '^nifi_platform_'
```

## Support Boundary

NiFi-Fabric is intentionally conservative about support claims:

- kind is the runtime proof baseline in this repository
- AKS and OpenShift guidance is published separately and remains conservative because no real cluster was exercised in this slice
- `nativeApi` remains the production-ready metrics path
- `exporter` remains experimental even though focused runtime proof exists
- `siteToSite` remains prepared-only
- the starter operations assets are templates, not a production certification pack
