# Tools

NiFi-Fabric keeps small pointers here for external tools that are useful
alongside the platform but live in separate repositories.

Current external tool:

- [Flow Upgrade Advisor](flow-upgrade-advisor/README.md)
- [NiFi-Fabric Authz Tooling](nifi-fabric-authz/README.md)

Current in-repo optional CLI entrypoint:

- `go run ./cmd/nifi-fabric-authz --help`

The authz generator is a supported optional in-tree CLI for Git-rendered OIDC authorization overlays.
