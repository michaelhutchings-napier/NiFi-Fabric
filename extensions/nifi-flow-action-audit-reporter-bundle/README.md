# Flow Action Audit Reporter Bundle

This bundle contains the planned NiFi-Fabric `FlowActionReporter` implementation for structured design-time audit logging.

Current scope:

- one fixed JSON logger reporter
- one NAR packaging module
- no arbitrary reporter properties beyond what NiFi exposes in the `FlowActionReporter` API

The reporter writes one JSON event per flow action to the dedicated SLF4J logger:

- `org.apache.nifi.flowaudit`

## Build

If `mvn` is available locally:

```bash
cd extensions/nifi-flow-action-audit-reporter-bundle
mvn -DskipTests package
```

If `mvn` is not available locally, use the repository helper:

```bash
bash hack/build-flow-action-audit-reporter-nar.sh
```

Expected artifact:

- `extensions/nifi-flow-action-audit-reporter-bundle/nifi-flow-action-audit-reporter-nar/target/nifi-flow-action-audit-reporter-nar-0.0.1-SNAPSHOT.nar`

To build the minimal reporter image used by `observability.audit.flowActions.export.type=log`:

```bash
bash hack/build-flow-action-audit-reporter-image.sh
```

Default image tag:

- `nifi-flow-action-audit-reporter:0.0.1-SNAPSHOT`

To print the currently resolved artifact version:

```bash
make print-flow-action-audit-reporter-version
```

To copy the built NAR into `dist/flow-action-audit-reporter/` for CI or release packaging:

```bash
make build-flow-action-audit-reporter-dist
```

## CI And Release

The repository now treats the reporter as a first-class build artifact:

- the main `ci` workflow builds the reporter NAR and the minimal reporter image on every normal validation run
- the dedicated `Flow Action Audit Reporter Artifact` workflow uploads the built NAR as a workflow artifact on pull requests
- pushes to `main` or `master` publish the reporter image to `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter:edge` and `:sha-<commit>`
- pushing a tag named `flow-action-audit-reporter-vX.Y.Z` publishes the image tag `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter:X.Y.Z` and attaches the matching NAR to the GitHub release for that tag

## Current Status

The repository now contains the reporter source and NAR scaffold.

The chart now supports the bounded advanced path:

- `observability.audit.flowActions.export.type=log`

This requires a reporter image that contains the built NAR and is referenced through:

- `observability.audit.flowActions.export.log.installation.image.*`
