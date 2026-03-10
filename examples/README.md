# Examples

These are the four evaluator-facing examples for the current private alpha.

Recommended one-command evaluator installs:

- standalone: `make install-standalone`
- managed: `make install-managed`
- managed + cert-manager: `make install-managed-cert-manager`

There is also one AKS-prepared set of starting overlays:

- [aks/standalone-values.yaml](aks/standalone-values.yaml)
  - Prepared starting point for future AKS standalone evaluation.
  - Not yet validated on a real AKS cluster.

- [aks/managed-values.yaml](aks/managed-values.yaml)
  - Prepared starting point for future AKS managed-mode evaluation.
  - Compose with [cert-manager-values.yaml](cert-manager-values.yaml) if cert-manager already exists in the AKS cluster.
  - Not yet validated on a real AKS cluster.

There is also one OpenShift-prepared set of starting overlays:

- [openshift/standalone-values.yaml](openshift/standalone-values.yaml)
  - Prepared starting point for future OpenShift standalone evaluation.
  - Keeps the Service internal and renders a passthrough Route.
  - Not yet validated on a real OpenShift cluster.

- [openshift/managed-values.yaml](openshift/managed-values.yaml)
  - Prepared starting point for future OpenShift managed-mode evaluation.
  - Keeps the Service internal, renders a passthrough Route, and relaxes fixed kind-style UID settings.
  - Compose with [cert-manager-values.yaml](cert-manager-values.yaml) if cert-manager already exists in the OpenShift cluster.
  - Not yet validated on a real OpenShift cluster.

There is also one optional TLS-source overlay:

- [cert-manager-values.yaml](cert-manager-values.yaml)
  - Switches the chart from `tls.mode=externalSecret` to `tls.mode=certManager`.
  - Use it on top of either the standalone or managed Helm values when cert-manager and the `nifi-ca` issuer bootstrap are already installed.
  - Still requires a separate Secret for the PKCS12 password and `nifi.sensitive.props.key`.
  - For kind evaluator setup, run `make kind-bootstrap-cert-manager` first.
  - The focused fresh-kind evaluation command is `make kind-cert-manager-e2e`.

There are also prepared authentication overlays:

- [oidc-values.yaml](oidc-values.yaml)
  - Enables `auth.mode=oidc`.
  - Compose with [managed/values.yaml](managed/values.yaml).
  - Pair with [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml) for NiFi application groups, policies, and external proxy hosts.
  - Requires a real OIDC provider and is not validated by the current kind gate.

- [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml)
  - Seeds NiFi application groups and file-managed policies for OIDC group claims.
  - Group names in the token must match these NiFi application group names exactly.

- [ldap-values.yaml](ldap-values.yaml)
  - Enables `auth.mode=ldap` with `authz.mode=ldapSync`.
  - Requires a real LDAP server and bind Secret.
  - Not validated by the current kind gate.

Only one authentication mode is supported at a time. The intended thin-platform combinations are:

- `singleUser + fileManaged`
- `oidc + externalClaimGroups`
- `ldap + ldapSync`

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
