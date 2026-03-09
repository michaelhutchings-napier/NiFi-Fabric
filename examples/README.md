# Examples

These are the four evaluator-facing examples for the current private alpha.

## Standalone

- [examples/standalone/values.yaml](/home/michael/Work/nifi2-platform/examples/standalone/values.yaml)
  - Minimal Helm values for a standalone NiFi 2 install on kind.
  - Use with `make helm-install-standalone`.

## Managed

- [examples/managed/values.yaml](/home/michael/Work/nifi2-platform/examples/managed/values.yaml)
  - Minimal Helm values for managed mode.
  - Use with `make helm-install-managed`.

- [examples/managed/nificluster.yaml](/home/michael/Work/nifi2-platform/examples/managed/nificluster.yaml)
  - Minimal `NiFiCluster` for managed mode in the `Running` state.
  - Use with `make apply-managed`.

## Rollout Trigger

- [examples/managed/rollout-trigger-values.yaml](/home/michael/Work/nifi2-platform/examples/managed/rollout-trigger-values.yaml)
  - Minimal Helm values overlay that changes a pod template annotation.
  - Use to trigger the managed `OnDelete` revision rollout path.

## Hibernate And Restore

- [examples/managed/nificluster-hibernated.yaml](/home/michael/Work/nifi2-platform/examples/managed/nificluster-hibernated.yaml)
  - Minimal `NiFiCluster` example for the `Hibernated` state.
  - Apply it to hibernate the managed cluster.
  - Restore with [examples/managed/nificluster.yaml](/home/michael/Work/nifi2-platform/examples/managed/nificluster.yaml).
