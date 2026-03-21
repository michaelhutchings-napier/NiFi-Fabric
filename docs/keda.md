# KEDA Integration

KEDA is an optional autoscaling input for NiFi-Fabric.

The recommended autoscaling model is still the built-in controller-owned autoscaler. KEDA is for teams that already use KEDA triggers and want those triggers to feed replica intent into NiFi-Fabric without giving KEDA direct ownership of the NiFi workload.

## Supported Model

- available with managed installs through `charts/nifi-platform`
- KEDA targets `NiFiCluster`, not the NiFi `StatefulSet`
- the NiFi-Fabric controller remains the only component that changes real NiFi replicas
- scale-up is supported
- scale-down remains optional and controller-mediated

## Why It Works This Way

NiFi scale-down is not just a replica change. Nodes may need disconnect, offload, and other safety checks before removal.

That is why NiFi-Fabric does not let KEDA or a generated HPA own the `StatefulSet` directly. KEDA can request capacity, but the controller still decides whether a real action is safe.

## When To Use It

KEDA is a good fit when:

- your platform already standardizes on KEDA
- you want external event or resource signals to request NiFi capacity
- you are comfortable treating the KEDA replica request as runtime-owned data

The built-in controller autoscaler is the better default when you do not need KEDA's trigger ecosystem.

## What KEDA Does And Does Not Do

KEDA does:

- watch external triggers
- write replica intent through the `NiFiCluster` `/scale` surface
- work with the controller's autoscaling model

KEDA does not:

- scale the NiFi `StatefulSet` directly
- bypass rollout, TLS, hibernation, restore, degraded-state, or safe scale-down checks
- take ownership of destructive scale-down execution

## How To Enable It

1. Install KEDA in your cluster.
2. Start from a managed `charts/nifi-platform` install.
3. Add [platform-managed-keda-values.yaml](/home/michael/Work/nifi2-platform/examples/platform-managed-keda-values.yaml).
4. Add [platform-managed-keda-scale-down-values.yaml](/home/michael/Work/nifi2-platform/examples/platform-managed-keda-scale-down-values.yaml) only if you want external downscale intent.

Example:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-keda-values.yaml
```

## What Operators Should Expect

- the KEDA request is an input, not a guarantee that NiFi will resize immediately
- the controller may adjust, defer, block, or ignore that request based on lifecycle state and autoscaling rules
- when KEDA is enabled, `spec.autoscaling.external.requestedReplicas` becomes runtime-managed and should not be treated as a hand-authored GitOps field

## Support Position

KEDA support is GA for this documented model:

- optional
- secondary to the built-in controller autoscaler
- controller-owned for all real scale execution

## Read Next

- [Autoscaling](manage/autoscaling.md)
- [Compatibility](compatibility.md)
- [Operations and Troubleshooting](operations.md)
- [Advanced Install Paths](install/advanced.md)
