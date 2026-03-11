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
  - Use [oidc-kind-values.yaml](oidc-kind-values.yaml) for the focused kind OIDC evaluator.

- [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml)
  - Seeds NiFi application groups and file-managed policies for OIDC group claims.
  - Group names in the token must match these NiFi application group names exactly.

- [oidc-kind-values.yaml](oidc-kind-values.yaml)
  - Focused kind OIDC overlay.
  - Keeps the flow internal to the cluster.
  - Uses the documented `Initial Admin Identity` fallback for the first admin path.
  - The focused runtime command is `make kind-auth-oidc-e2e`.

- [oidc-external-url-values.yaml](oidc-external-url-values.yaml)
  - Adds an ingress-backed public HTTPS host and matching `web.proxyHosts` entry for OIDC redirects.
  - Compose with [oidc-values.yaml](oidc-values.yaml) and [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml).

- [ldap-values.yaml](ldap-values.yaml)
  - Enables `auth.mode=ldap` with `authz.mode=ldapSync`.
  - Use [ldap-kind-values.yaml](ldap-kind-values.yaml) for the focused kind LDAP evaluator.

- [ldap-kind-values.yaml](ldap-kind-values.yaml)
  - Focused kind LDAP overlay.
  - Uses the documented `Initial Admin Identity` bootstrap path.
  - The focused runtime command is `make kind-auth-ldap-e2e`.

- [ingress-proxy-host-values.yaml](ingress-proxy-host-values.yaml)
  - Generic ingress and `web.proxyHosts` overlay for auth-enabled browser access.
  - Prepared only. Adjust hostnames, ingress class, and annotations for your environment.

- [openshift/route-proxy-host-values.yaml](openshift/route-proxy-host-values.yaml)
  - OpenShift passthrough Route host plus matching `web.proxyHosts`.
  - Compose with OpenShift overlays and either OIDC or LDAP when the cluster is available.

There are also prepared Flow Registry Client overlays:

- [github-flow-registry-values.yaml](github-flow-registry-values.yaml)
  - Prepared GitHub Flow Registry Client catalog entry.
  - Renders a validated definition only; it does not auto-create the client in NiFi.

- [gitlab-flow-registry-values.yaml](gitlab-flow-registry-values.yaml)
  - Prepared GitLab Flow Registry Client catalog entry.
  - Renders a validated definition only; it does not auto-create the client in NiFi.

- [bitbucket-flow-registry-values.yaml](bitbucket-flow-registry-values.yaml)
  - Prepared Bitbucket Flow Registry Client catalog entry.
  - Renders a validated definition only; it does not auto-create the client in NiFi.

- [azure-devops-flow-registry-values.yaml](azure-devops-flow-registry-values.yaml)
  - Prepared Azure DevOps Flow Registry Client catalog entry.
  - Renders a validated definition only; it does not auto-create the client in NiFi.

There is also one focused NiFi version compatibility overlay:

- [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml)
  - Overrides the chart image tag to `apache/nifi:2.8.0`.
  - Uses `replicaCount: 2` for the focused single-node kind compatibility proof.
  - Compose with either [standalone/values.yaml](standalone/values.yaml) or [managed/values.yaml](managed/values.yaml).
  - The focused managed proof command is `make kind-nifi-2-8-e2e`.

Only one authentication mode is supported at a time. The intended thin-platform combinations are:

- `singleUser + fileManaged`
- `oidc + externalClaimGroups`
- `ldap + ldapSync`

Preferred bootstrap:

- `authz.bootstrap.initialAdminGroup`

Fallback bootstrap:

- `authz.bootstrap.initialAdminIdentity`

Focused auth evaluator commands:

- `make kind-auth-oidc-e2e`
- `make kind-auth-ldap-e2e`
- `make kind-nifi-2-8-e2e`

Flow Registry Client notes:

- classic NiFi Registry is not the preferred direction here
- Git-based Flow Registry Clients are preferred
- the chart renders a prepared catalog under `flowRegistryClients.mountPath`
- there is no controller-managed flow import or synchronization

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
