# Flow Registry Clients

NiFi-Fabric treats Git-based Flow Registry Clients as the preferred modern direction.

## What This Feature Does

The chart can render prepared Flow Registry Client catalog content for NiFi.

NiFi Registry is supported here as a NiFi `2.x` compatibility path, not as the strategic long-term center of the product.

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

- GitHub: verified on the supported NiFi `2.x` line
- GitHub: end-to-end save-to-registry workflow verified on the supported NiFi `2.x` line
- GitLab: verified on the supported NiFi `2.x` line
- Bitbucket: verified on the supported NiFi `2.x` line
- NiFi Registry: verified on the supported NiFi `2.x` line through a real in-cluster `apache/nifi-registry` service
- Azure DevOps: prepared definition, render-validated

## Typed NiFi Registry Compatibility Path

The typed `provider=nifiRegistry` surface is intentionally small:

- client `name`
- `provider: nifiRegistry`
- `nifiRegistry.url`
- optional `nifiRegistry.sslContextServiceName` when the live NiFi Registry Client should reference an existing SSL context service
- optional description text

What this support path owns:

- prepared catalog entries rendered by the chart
- for `versionedFlowImports.*`, the specific live `provider=nifiRegistry` Flow Registry Client objects the product creates or reconciles for the declared import path

What remains operator-owned:

- undeclared or manually created Flow Registry Clients
- same-name operator-owned clients that the product did not mark as owned
- broader live registry-client lifecycle outside the NiFi Registry import path
- registry credentials, trust, and topology beyond the currently documented compatibility profile

Manual UI edits to product-owned live `provider=nifiRegistry` clients created by the import path may be reconciled back to the declared state. Manual edits to undeclared or operator-owned clients remain outside product ownership.

## Current End-to-End Workflow Verification

The first end-to-end workflow verification uses GitHub on the supported NiFi `2.x` line.

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
