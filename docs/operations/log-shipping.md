# Log Shipping

NiFi-Fabric does not ship a built-in logging agent.

The supported product pattern is to use the existing raw Kubernetes extension
hooks in the NiFi pod:

- `sidecars[]`
- `extraVolumes[]`
- `extraVolumeMounts[]`

This keeps log shipping explicit and environment-owned rather than turning it
into a controller feature or a first-class product subsystem.

## What The Example Does

The repository includes one concrete sidecar example for the standard platform
chart path:

- [platform-managed-log-shipping-vector-values.yaml](../../examples/platform-managed-log-shipping-vector-values.yaml)
- [log-shipping-vector-configmap.yaml](../../examples/log-shipping-vector-configmap.yaml)

That example:

- adds one Vector sidecar through `nifi.sidecars[]`
- mounts the existing shared `logs` volume from the NiFi pod read-only
- mounts one ConfigMap-backed Vector config
- adds one writable `emptyDir` for Vector state under `/var/lib/vector`
- tails `*.log` under `/opt/nifi/nifi-current/logs`
- writes structured events to the sidecar stdout stream so a cluster logging pipeline can collect them

This example is intentionally small. It shows the sidecar wiring and shared-log
volume pattern without committing the product to one destination backend.

## Important Notes

- the main NiFi container already tails `nifi-app.log` to stdout, so this example may duplicate some events your cluster logging pipeline already captures
- the shared logs volume is `emptyDir` by default, so this pattern is for near-real-time forwarding unless you explicitly enable `persistence.logs.*`
- enabling `persistence.logs.*` gives the shared log path a per-pod PVC for local retention, but it does not replace centralized logging or make log retention a product-managed feature
- if you need checkpointing, buffering, Secrets, or a remote sink, extend the example explicitly for your environment
- NiFi-Fabric does not claim support for every third-party agent or backend; the supported shape is the Kubernetes sidecar pattern itself

## Use The Example

Create the sample Vector ConfigMap:

```bash
kubectl -n nifi apply -f examples/log-shipping-vector-configmap.yaml
```

Then apply the overlay on the standard managed path:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-log-shipping-vector-values.yaml
```

If you use a different release name or namespace, update the command
accordingly.

## Operator Guidance

Use this example when you want:

- a documented answer to "how do I ship more than the default `nifi-app.log` tail?"
- one obvious place to add log parsing or enrichment
- an environment-owned path that works with the standard platform chart

Use a different sidecar or sink when your platform standard is not Vector. The
same pod-extension pattern can be reused with Fluent Bit, Filebeat, or another
agent if your team prefers that stack.
