# Flow Registry Clients

NiFi-Fabric treats Git-based Flow Registry Clients as the preferred modern direction.

## What This Feature Does

The chart can render prepared Flow Registry Client catalog content for NiFi.

NiFi Registry is supported here as a bounded NiFi `2.x` compatibility path, not as the strategic long-term center of the product.

Supported provider direction:

- GitHub
- GitLab
- Bitbucket
- Azure DevOps
- NiFi Registry

## Product Position

- Git-based Flow Registry Clients are the supported modern direction
- classic NiFi Registry is a compatibility-oriented path in this project
- the chart prepares catalog definitions; it does not become a broad flow-management platform

## Configuration Surface

Use app chart values under:

- `flowRegistryClients.enabled`
- `flowRegistryClients.mountPath`
- `flowRegistryClients.clients[]`

Use platform chart values under:

- `nifi.flowRegistryClients.*`

## Support Level

- GitHub: focused runtime proof on NiFi `2.8.0`
- GitHub: focused end-to-end save-to-registry workflow proof on NiFi `2.8.0`
- GitLab: focused runtime proof on NiFi `2.8.0`
- Bitbucket: focused runtime proof on NiFi `2.8.0`
- NiFi Registry: focused runtime proof on NiFi `2.8.0` through a real in-cluster `apache/nifi-registry` service
- Azure DevOps: prepared definition, render-validated

## Typed NiFi Registry Compatibility Path

The bounded typed `provider=nifiRegistry` surface is intentionally small:

- client `name`
- `provider: nifiRegistry`
- `nifiRegistry.url`
- optional `nifiRegistry.sslContextServiceName` when the live NiFi Registry Client should reference an existing bounded SSL context service
- optional description text

What this support path owns:

- prepared catalog entries rendered by the chart
- for bounded `versionedFlowImports.*`, the specific live `provider=nifiRegistry` Flow Registry Client objects the product creates or reconciles for the declared import path

What remains operator-owned:

- undeclared or manually created Flow Registry Clients
- same-name operator-owned clients that the product did not mark as owned
- broader live registry-client lifecycle outside the bounded NiFi Registry import path
- registry credentials, trust, and topology beyond the currently documented compatibility profile

Manual UI edits to product-owned live `provider=nifiRegistry` clients created by the bounded import path may be reconciled back to the declared state. Manual edits to undeclared or operator-owned clients remain outside product ownership.

## Current End-to-End Workflow Proof

The first bounded workflow proof uses GitHub on NiFi `2.8.0`.

What it proves:

- the chart-prepared external client is usable through the NiFi runtime API
- bucket discovery works
- a user can create a child process group with the seeded mutable-flow bundle
- a user can save that process group to the external Git-backed registry through NiFi version control APIs

What remains manual or out of scope:

- importing flows remains user-driven
- deployment remains user-driven
- there is no controller-managed flow synchronization
- there are no flow CRDs

## What This Feature Does Not Do

- manage imported flows as a controller workflow
- synchronize client state continuously
- create a new flow-management CRD surface
- broaden NiFi Registry support into generic registry-provider management
