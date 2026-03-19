# Experimental Features

NiFi-Fabric keeps experimental scope explicit.

## Experimental Today

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
