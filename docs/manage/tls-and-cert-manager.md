# TLS and cert-manager

NiFi-Fabric is TLS-first.

## What This Feature Does

NiFi-Fabric supports two TLS sourcing models:

- external Kubernetes Secret
- cert-manager-managed certificate Secret

The chart owns the workload wiring. The controller owns TLS lifecycle decisions such as observe-only, autoreload-first behavior, and restart-required escalation.

## Standard Configuration Surface

Use `charts/nifi` values under:

- `tls.mode`
- `tls.existingSecret`
- `tls.autoReload.*`
- `tls.certManager.*`

Use `charts/nifi-platform` values under:

- `nifi.tls.*`
- `cluster.restartPolicy.tlsDrift`

## External Secret Mode

This is the baseline path.

Required Secret:

- `Secret/nifi-tls`

The Secret contract is defined by the app chart values:

- `ca.crt`
- `keystore.p12`
- `truststore.p12`
- password keys
- `sensitivePropsKey` or a dedicated secret reference

## Cert-Manager Mode

cert-manager mode is supported when cert-manager already exists in the cluster.

What NiFi-Fabric expects:

- cert-manager is installed separately
- the issuer is already available
- the chart can render the `Certificate`
- stable password references are still provided

What NiFi-Fabric does not do:

- install cert-manager for you as part of the product chart
- replace cert-manager lifecycle with controller lifecycle

## Support Level

- external Secret mode: supported
- cert-manager mode: supported, with cert-manager as a prerequisite
- environment-specific proof: kind-focused today, see [Compatibility](../compatibility.md)
