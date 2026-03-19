# Experimental Features

NiFi-Fabric keeps experimental scope explicit.

## Experimental Today

### Local OIDC Browser-Flow Hardening

- focused kind evaluator only
- used to harden ingress and group-claims proof paths
- current local Keycloak `26.x` browser-flow coverage remains conservative until the focused gate is green again

### Site-to-Site Status And Provenance Export

- site-to-site status export
- site-to-site provenance export
- both paths are runtime-proven but still intentionally bounded and experimental

## Experimental Design Rule

An experimental feature in NiFi-Fabric must still keep:

- one lifecycle control plane
- explicit ownership of destructive actions
- conservative support claims
