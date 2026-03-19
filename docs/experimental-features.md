# Experimental Features

NiFi-Fabric keeps experimental scope explicit.

## Experimental Today

### Local OIDC Browser-Flow Hardening

- focused kind evaluator only
- used to harden ingress and group-claims proof paths
- current local Keycloak `26.x` browser-flow coverage remains conservative until the focused gate is green again

## Experimental Design Rule

An experimental feature in NiFi-Fabric must still keep:

- one lifecycle control plane
- explicit ownership of destructive actions
- conservative support claims
