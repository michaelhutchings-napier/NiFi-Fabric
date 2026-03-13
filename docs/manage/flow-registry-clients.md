# Flow Registry Clients

NiFi-Fabric treats Git-based Flow Registry Clients as the preferred modern direction.

## What This Feature Does

The chart can render prepared Flow Registry Client catalog content for NiFi.

Supported provider direction:

- GitHub
- GitLab
- Bitbucket
- Azure DevOps

## Product Position

- Git-based Flow Registry Clients are the supported modern direction
- classic NiFi Registry is not the preferred direction in this project
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
- GitLab: focused runtime proof on NiFi `2.8.0`
- Bitbucket: prepared definition, render-validated
- Azure DevOps: prepared definition, render-validated

## What This Feature Does Not Do

- manage imported flows as a controller workflow
- synchronize client state continuously
- create a new flow-management CRD surface
