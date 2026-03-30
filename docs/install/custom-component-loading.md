# Custom Component Loading

NiFi-Fabric supports custom NiFi component loading through standard Kubernetes
pod-extension hooks. This stays guidance-first: the chart gives you the wiring,
but it does not become an extension manager, package registry, or sync system.

## Supported Patterns

Use one of these patterns depending on how stable the custom content is and how
you want to deliver it.

### 1. Custom Image

Use a custom NiFi image when the custom content is stable and versioned
together with the workload image.

Good fit:

- bundled custom NARs
- bundled JDBC drivers
- stable Python extensions
- air-gapped environments that already mirror application images

Why this is the preferred stable path:

- one image tag captures the full runtime
- rollout behavior stays obvious
- no extra pod wiring is needed for the component files themselves

### 2. Mounted Volume Plus `config.extraProperties`

Use `extraVolumes[]` and `extraVolumeMounts[]` when operators already manage the
files separately and want to mount them into the NiFi pod.

Good fit:

- JDBC drivers from a ConfigMap, Secret, or projected volume
- custom Python modules or scripts
- certificates or helper files required by specific processors
- prebuilt NARs made available through an operator-managed volume source

Use `config.extraProperties` when NiFi needs an explicit property pointing at
that mounted path.

### 3. Init-Container Preparation Into a Shared Writable Path

Use `extraInitContainers[]` when files need to be copied, unpacked, or prepared
before the main NiFi container starts.

Good fit:

- content copied from a utility image into a shared volume
- archives that must be unpacked before NiFi starts
- content that needs reshaping into a writable path before NiFi consumes it

This is also the pattern the chart already uses for the optional flow-action
audit reporter NAR.

## What The Product Supports

The supported product surface here is the Kubernetes wiring itself:

- `extraVolumes[]`
- `extraVolumeMounts[]`
- `extraInitContainers[]`
- `config.extraProperties`

That means NiFi-Fabric supports clear, operator-owned mounting and preparation
patterns. It does not claim ownership of:

- extension build pipelines
- git-sync workflows
- package lifecycle management
- marketplace integration
- automatic component rollback

## Common Use Cases

### Custom NAR Directories

NiFi can load additional NAR libraries from explicit properties such as custom
`nifi.nar.library.directory.*` entries.

Typical shape:

1. Mount or prepare a directory such as `/opt/nifi/fabric/extensions/custom-nars`
2. Point NiFi at it through `config.extraProperties`

Example property:

```yaml
config:
  extraProperties:
    nifi.nar.library.directory.custom: /opt/nifi/fabric/extensions/custom-nars
```

### JDBC Drivers

Processors that depend on JDBC drivers often only need the driver files mounted
at a stable path that the processor or controller service can reference.

Typical shape:

1. Mount the driver JARs into the pod
2. Reference the mounted path in the NiFi controller service configuration

This usually does not require `config.extraProperties`.

### Python Extensions

Python processors or supporting modules can be mounted or copied into a known
directory, then referenced through NiFi properties when the runtime needs an
explicit path.

Typical shape:

1. Mount or prepare the Python extension directory
2. Set the matching NiFi property under `config.extraProperties`

## Example Overlay

See:

- [custom-components-jdbc-values.yaml](../../examples/custom-components-jdbc-values.yaml)

This example shows a simple mounted-volume path for JDBC drivers:

- one `ConfigMap`-backed `extraVolumes[]` entry
- one matching `extraVolumeMounts[]` entry
- no extra lifecycle machinery

The checked-in example is written for `charts/nifi-platform`, so the pod
extension fields live under `nifi.*`. For direct `charts/nifi` installs, use
the same fields at the top level instead of nesting them under `nifi:`.

Adapt the same pattern for:

- mounted custom NAR directories
- mounted Python extension directories
- processor-side support files

## Operator Notes

- prefer a custom image for stable, versioned component bundles
- prefer a mounted volume when operators already own the component files
- prefer an extra init container when files must be copied or unpacked before
  NiFi starts
- keep mounted paths explicit and predictable
- document any matching NiFi properties in the same Helm values overlay

## Non-Goals

- not a custom component CRD
- not git-sync by default
- not automatic extension lifecycle management
- not a general plugin marketplace
