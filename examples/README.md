# Examples

These are the four evaluator-facing examples for the current private alpha.

There is also one optional TLS-source overlay:

- [cert-manager-values.yaml](cert-manager-values.yaml)
  - Switches the chart from `tls.mode=externalSecret` to `tls.mode=certManager`.
  - Use it on top of either the standalone or managed Helm values when cert-manager and the `nifi-ca` issuer bootstrap are already installed.
  - Still requires a separate Secret for the PKCS12 password and `nifi.sensitive.props.key`.
  - For kind evaluator setup, run `make kind-bootstrap-cert-manager` first.
  - The focused fresh-kind evaluation command is `make kind-cert-manager-e2e`.

## Standalone

- [standalone/values.yaml](standalone/values.yaml)
  - Minimal Helm values for a standalone NiFi 2 install on kind.
  - Use with `make helm-install-standalone`.

## Managed

- [managed/values.yaml](managed/values.yaml)
  - Minimal Helm values for managed mode.
  - Use with `make helm-install-managed`.

- [managed/nificluster.yaml](managed/nificluster.yaml)
  - Minimal `NiFiCluster` for managed mode in the `Running` state.
  - Use with `make apply-managed`.

## Rollout Trigger

- [managed/rollout-trigger-values.yaml](managed/rollout-trigger-values.yaml)
  - Minimal Helm values overlay that changes a pod template annotation.
  - Use to trigger the managed `OnDelete` revision rollout path.

## Hibernate And Restore

- [managed/nificluster-hibernated.yaml](managed/nificluster-hibernated.yaml)
  - Minimal `NiFiCluster` example for the `Hibernated` state.
  - Apply it to hibernate the managed cluster.
  - Restore with [managed/nificluster.yaml](managed/nificluster.yaml).
