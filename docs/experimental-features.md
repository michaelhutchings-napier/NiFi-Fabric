# Experimental Features

NiFi-Fabric keeps experimental scope explicit.

## Experimental Today

### KEDA Integration

- optional
- managed-platform only
- built-in controller-owned autoscaling remains the primary and recommended model
- the richer built-in autoscaling model itself is not experimental; only the optional KEDA external-intent path is
- targets `NiFiCluster`, not the NiFi `StatefulSet`
- the controller remains the only executor of real scale actions
- writes runtime-managed external replica intent through `NiFiCluster` `/scale`
- reports controller-bounded external intent plus current handling states such as actionable, deferred, blocked, or ignored through `status.autoscaling.external`

See [KEDA Integration Position](keda.md).

### Exporter Metrics Mode

- optional companion deployment
- flow metrics only
- separate from the primary `nativeApi` path

### Local OIDC Browser-Flow Hardening

- focused kind evaluator only
- used to harden ingress and group-claims proof paths
- current local Keycloak `26.x` browser-flow coverage remains conservative until the focused gate is green again

## Prepared-Only Today

### Site-to-Site Metrics

- values contract exists
- runtime path is not wired yet
- reporting-task and receiver lifecycle remain out of scope for the current slice

## Experimental Design Rule

An experimental feature in NiFi-Fabric must still keep:

- one lifecycle control plane
- explicit ownership of destructive actions
- conservative support claims
