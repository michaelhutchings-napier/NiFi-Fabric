# Roadmap

NiFi-Fabric keeps the roadmap small and explicit.

## Production-Ready Now

- one-release platform install with `charts/nifi-platform`
- cert-manager-first managed quickstart install through `charts/nifi-platform`
- standalone app install with `charts/nifi`
- thin controller model for rollout, TLS handling, hibernation, and restore
- built-in controller-owned autoscaling, including advisory recommendations, enforced scale-up, richer capacity reasoning, actual StatefulSet removal-candidate qualification, and sequential multi-step safe scale-down
- optional KEDA external intent through `NiFiCluster` `/scale`, with controller-owned execution and starter operational support
- supported external Secret and cert-manager TLS paths
- first-class OIDC and LDAP auth options
- native API metrics as the primary metrics mode
- exporter metrics as an optional GA secondary metrics mode
- typed Site-to-Site metrics export as an optional GA sender-side path
- typed Site-to-Site status export as an optional GA sender-side path
- typed Site-to-Site provenance export as an optional GA sender-side path
- Git-based Flow Registry Client direction through chart-managed catalog rendering
- Parameter Context management
- versioned-flow import
- NiFi Registry integration through a typed `provider=nifiRegistry` client plus platform-chart versioned-flow import

## Planned Next

- more Layer 7 support for the Istio Ambient service mesh profile
- broader per-node drainability ranking only if it stays explainable, conservative, and justified by trustworthy evidence beyond the current actual-removal-candidate qualification model
- broader bulk autoscaling policy depth beyond the current sequential scale-down episode model only if it remains sequential controller-owned one-node steps with fresh settle and requalification after every removal

## Not Planned Right Now

- ZooKeeper support is not planned. NiFi-Fabric is intentionally NiFi 2.x only, and the product keeps the clustering and state-management model on the native Kubernetes path rather than reintroducing a second coordination system and the extra lifecycle, test, and support surface that comes with it.
- automatic auth priority or fallback is not planned. The product keeps `auth.mode` explicit so operators can see exactly which auth path is active, which authorization model goes with it, and which Secrets and bootstrap inputs are required. That keeps auth behavior explainable and avoids surprising fallback during install or upgrade.

## Review Summary

This is the current review view of the roadmap ideas below. `recommended` means a strong fit for NiFi-Fabric. `consider` means potentially useful, but more situational or lower priority. `not recommended` means the idea would likely widen the product in the wrong direction even if kept optional.

### Recommended

- quickstart and demo path visibility
  - status: `recommended`
  - fit: `high`
- documented log-shipper example overlay using `sidecars[]`
  - status: `recommended`
  - fit: `high`
- backup and DR documentation improvements
  - status: `recommended`
  - fit: `high`
- repository encryption support
  - status: `recommended`
  - fit: `high`
- dashboard pack expansion focused on NiFi runtime health
  - status: `recommended`
  - fit: `high`
- storage surface extensions, especially per-repository storage classes
  - status: `recommended`
  - fit: `high`
- Azure Blob guidance for control-plane backup artifacts on AKS
  - status: `recommended`
  - fit: `high`

### Consider

- `logging.levels.*` troubleshooting surface
  - status: `consider`
  - fit: `medium`
- `debugStartup` troubleshooting mode
  - status: `consider`
  - fit: `medium`
- external identity-provider guidance, including Keycloak examples
  - status: `consider`
  - fit: `medium`
- Azure DevOps flow documentation and possible workflow parity exploration
  - status: `consider`
  - fit: `medium`
- exporter deployment replica configuration
  - status: `consider`
  - fit: `medium`
- optional persistent logs
  - status: `consider`
  - fit: `medium`
- generic external Site-to-Site networking, but only if kept extremely narrow
  - status: `consider`
  - fit: `low`

### Not Recommended

- broad extra ports and services matrix
  - status: `not recommended`
  - fit: `low`
- broad arbitrary storage topology toolkit
  - status: `not recommended`
  - fit: `low`
- ZooKeeper support
  - status: `not recommended`
  - fit: `low`
- automatic auth priority or fallback
  - status: `not recommended`
  - fit: `low`
- product-owned backup orchestration
  - status: `not recommended`
  - fit: `low`
- cross-region replication or DR topology management
  - status: `not recommended`
  - fit: `low`

## Brainstorm Ideas

### Quickstart And Demo Path Visibility

Idea:

- make the fastest safe evaluation path much more obvious in top-level docs without weakening the production-first install story

Why this came up:

- the repository already has a strong quickstart path through `examples/platform-managed-cert-manager-quickstart-values.yaml`
- the standard install page already documents the managed quickstart flow in `docs/install/helm.md`
- `README.md`, `docs/start-here.md`, and `examples/README.md` still ask the reader to do a bit too much discovery to understand the quickest path from zero to a running NiFi cluster

Current repo touchpoints:

- `README.md`
- `docs/start-here.md`
- `docs/install/helm.md`
- `docs/first-day.md`
- `examples/README.md`
- `examples/platform-managed-cert-manager-quickstart-values.yaml`
- `examples/platform-managed-quickstart-values.yaml`
- `docs/local-kind.md`

Potential deliverables:

- add one small "Fast Evaluation" block near the top of `README.md` with one recommended command, one sentence on what gets bootstrapped, and one link to day-1 checks
- make `docs/start-here.md` explicitly branch into:
  - standard production install
  - quick evaluation install
  - advanced explicit-auth-and-TLS install
- tighten `examples/README.md` so the first section is "start here" rather than a long mixed catalog of overlays and focused validation assets
- call out the difference between:
  - the standard cert-manager-first quickstart
  - the secondary self-signed quickstart
  - the explicit advanced path
- add one short customer-facing note that the quickstart path is valid for evaluation and low-friction first installs, but not a substitute for explicit enterprise auth planning

Guardrails:

- do not turn the homepage into an "examples catalog"
- do not bury the standard production install path under too many local-dev options
- do not make `kind` or repo proof flows sound like the primary customer production path

Non-goals:

- not a redesign of the whole documentation set
- not a change to product behavior
- not a new installation mode

### Documented Log Shipper Overlay

Idea:

- add a documented log-shipping example overlay built on the existing generic sidecar hooks instead of productizing a single shipper implementation
- consider a small optional NiFi logging-override surface for troubleshooting-oriented log-level changes if customers need a safer path than ad hoc image or file customization

Why this came up:

- the app chart already exposes `sidecars[]`, `extraVolumes[]`, and `extraVolumeMounts[]`
- the StatefulSet already has a writable logs volume that sidecars can mount
- customers who compare us with broader charts may expect a clear answer to "how do I ship NiFi logs?"
- we likely want to show a supported pattern without turning log shipping into a first-class subsystem we own forever
- some comparison charts also expose simple log-level overrides such as:
  - `logging.levels.org.apache.nifi=DEBUG`
  - `logging.levels.org.apache.nifi.web.security=DEBUG`
- the current repo does not expose a first-class `logging.levels` values surface, so operators today would need lower-level customization if they want temporary troubleshooting-oriented NiFi logger changes

Current repo touchpoints:

- `charts/nifi/values.yaml` for `sidecars[]`, `extraVolumes[]`, and `extraVolumeMounts[]`
- `charts/nifi/templates/statefulset.yaml` for the mounted logs volume and sidecar rendering
- `charts/nifi/templates/statefulset.yaml` for the current `nifi-app.log` tail-to-stdout behavior
- `docs/operations.md`
- `docs/README.md`
- `examples/README.md`

Potential deliverables:

- create one example overlay such as `examples/log-shipping-vector-values.yaml` or `examples/log-shipping-fluent-bit-values.yaml`
- document how the sidecar mounts the existing `logs` volume and tails `/opt/nifi/nifi-current/logs`
- show the smallest viable customer shape:
  - one sidecar
  - one config source
  - one destination example
- add a short doc page or section under operations that explains:
  - NiFi-Fabric does not ship a built-in logging agent
  - the product supports sidecar-based shipping through generic Kubernetes hooks
  - platform teams can standardize on Vector, Fluent Bit, Filebeat, or another agent
- include one note about log rotation expectations and the fact that log shipping is environment-specific
- if customers need it, evaluate a small optional NiFi logging surface such as:
  - `logging.levels.*` values that render deterministic logback overrides for selected logger names
  - one clearly documented pattern for short-lived troubleshooting overrides rather than a broad logging-management feature
- if that logging surface is added, document examples such as:
  - `org.apache.nifi=DEBUG`
  - `org.apache.nifi.web.security=DEBUG`
  and explain that these are operational debugging tools, not recommended default production settings

Guardrails:

- prefer one concrete example over a broad matrix of agents
- keep the feature optional and external
- avoid introducing controller behavior, CRDs, or provider-specific lifecycle ownership
- if log-level overrides are added, keep them narrow, deterministic, and easy to remove
- prefer chart-rendered explicit config over ad hoc startup mutation magic

Non-goals:

- not a built-in `filebeat.*` API
- not a managed logging stack
- not support commitments for every third-party log shipper
- not full logback ownership as a large product subsystem unless real demand appears

### Debug Startup Support

Idea:

- consider a small optional `debugStartup`-style troubleshooting switch for the NiFi workload if customers need a more obvious startup-debug path

Why this came up:

- some comparison charts expose a simple `debugStartup` value that pauses or changes startup behavior for investigation
- the current repo does not expose a first-class `debugStartup` switch
- troubleshooting today relies on normal pod logs, controller logs, probes, and any lower-level custom overrides the operator introduces

Current repo touchpoints:

- `charts/nifi/values.yaml`
- `charts/nifi/templates/statefulset.yaml`
- `docs/operations.md`
- `docs/operations/runbooks.md`
- `docs/first-day.md`

Potential deliverables:

- decide whether the real customer need is:
  - a startup pause before `nifi.sh run`
  - extra shell tracing around bootstrap
  - a one-shot troubleshooting mode that keeps the container alive for inspection
- if added, keep the surface very small and explicit, for example:
  - `debugStartup.enabled`
  - optional `debugStartup.sleepSeconds` or a simple command mode
- document exactly what the switch does to avoid support ambiguity:
  - whether probes should be disabled or adjusted
  - whether the pod is expected to become Ready
  - whether this mode is for temporary operator troubleshooting only
- ensure any debug-startup mode is clearly incompatible with normal production readiness expectations unless intentionally designed otherwise

Guardrails:

- keep the feature operator-driven and temporary
- do not let troubleshooting mode silently change normal production startup semantics
- avoid a broad grab-bag of debug flags

Non-goals:

- not a permanent alternate runtime mode
- not an excuse to bypass clear startup and readiness behavior
- not a replacement for better status, events, and logs

### Backup And Disaster Recovery Extensions

Idea:

- capture which backup and DR ideas are a good fit for NiFi-Fabric and which ones should stay outside the product boundary

Why this came up:

- comparison repos often present backup, snapshot, flow export, and disaster-recovery guidance as part of the chart story
- NiFi-Fabric already has a clear two-layer recovery model, but it is useful to spell out what that means for future product decisions
- the repository already includes control-plane backup guidance, managed hibernation and restore, typed NiFi Registry integration, and typed Site-to-Site sender-side observability paths

Current repo touchpoints:

- `docs/dr.md`
- `docs/manage/hibernation-and-restore.md`
- `docs/manage/flow-registry-clients.md`
- `docs/manage/flows.md`
- `docs/manage/observability-metrics.md`
- `docs/reference/app-chart-values.md`
- `charts/nifi/values.yaml`

Accepted direction:

- improve backup and DR documentation so customers can more easily understand the current split:
  - control-plane backup belongs to NiFi-Fabric
  - storage snapshot and restore belong to the storage platform
  - runtime data recovery belongs to operator procedures and platform storage capabilities
- add clearer operator guidance for backup planning, such as:
  - what to preserve from Helm values and rendered intent
  - which Secrets, issuers, and ConfigMaps matter for recovery
  - what PVC-backed repositories exist and why storage-level recovery is required for them
- consider a small advanced storage or DR doc section showing how generic PVC snapshot workflows can be layered on top of the current chart model
- improve docs that connect the existing typed NiFi Registry and versioned-flow import features to DR planning, so customers understand that registry-backed flow definitions are already handled through typed Flow Registry Client and import surfaces rather than ad hoc `nifi.registry.*` snippets
- consider a small operator-runbook-style advisory section for backup and recovery steps if it stays clearly documented as guidance rather than as a product-owned automation guarantee

Why these are accepted:

- they improve customer understanding without introducing a second control plane
- they align with the current DR split already documented in `docs/dr.md`
- they make existing features easier to use rather than broadening product ownership

Not accepted:

- do not add a product-owned automated backup controller or storage orchestration layer
- do not turn cloud- or storage-specific snapshot behavior into a first-class NiFi-Fabric API
- do not present manual "stop NiFi, snapshot PVCs, export flow, start NiFi" procedures as a product-guaranteed consistent backup workflow
- do not broaden Site-to-Site settings or `nifi.remote.input.*` tuning into a productized cross-region replication or disaster-recovery feature
- do not expand into multi-region active-active or cross-region replication management as part of the core platform
- do not replace the typed Flow Registry Client and versioned-flow import model with raw-property examples as the main product story

Why these are not accepted:

- they would widen the product into backup orchestration, storage automation, or flow-topology management
- they would introduce environment-specific behavior and guarantees that are hard to test and support cleanly
- they conflict with the project goal of staying smaller, explainable, and NiFi 2-first rather than becoming a broad operator platform

Guardrails:

- prefer documentation and operator guidance over new automation
- keep storage recovery generic and platform-owned
- keep typed NiFi configuration features as the preferred product surface
- avoid implying durable queue or repository recovery guarantees that the product does not own

Non-goals:

- not a backup operator
- not a storage snapshot scheduler
- not a cross-region data replication product
- not a generic disaster-recovery orchestration layer

### Generic Site-to-Site Networking

Idea:

- evaluate whether NiFi-Fabric should expose a small optional product surface for generic NiFi Site-to-Site networking beyond the current typed sender-side observability paths

Why this came up:

- comparison charts expose broader automatic Site-to-Site routing for both cluster-local and ingress-backed external access
- NiFi-Fabric already sets the core secure NiFi runtime properties such as `nifi.remote.input.host`, but the public product surface today focuses on typed Site-to-Site sender-side features for metrics, status, and provenance
- some customers may eventually want a supported answer for general external Site-to-Site client connectivity rather than only the current typed observability sender paths

Current repo touchpoints:

- `charts/nifi/templates/statefulset.yaml`
- `charts/nifi/values.yaml`
- `docs/manage/observability-metrics.md`
- `docs/reference/app-chart-values.md`
- `docs/architecture.md`

What exists today:

- secure clustered NiFi runtime wiring already sets the basic NiFi remote-input host and secure cluster properties
- typed Site-to-Site sender-side product features already exist for:
  - metrics
  - status
  - provenance
- these current typed paths are intentionally narrower than a generic Site-to-Site networking or external client-routing feature

Accepted direction:

- document the current boundary more clearly so customers understand that typed Site-to-Site observability features are supported today, while generic external Site-to-Site client exposure is not yet a first-class product surface
- if real customer demand appears, consider a small advanced networking feature for generic Site-to-Site exposure that stays narrow and explicit, for example:
  - one documented external host model
  - one documented TLS model
  - one documented ingress or Route shape
  - one predictable mapping into `web.proxyHosts` and any required NiFi remote-input properties
- keep any future feature focused on connectivity and routing, not on broader dataflow design, replication policy, or automation of client-side topology

How far we should push it:

- acceptable first step:
  - documentation that explains the current boundary and what is or is not supported today
- acceptable second step, only if needed:
  - one optional advanced exposure model for generic external Site-to-Site access on standard Kubernetes ingress
  - optionally one equivalent documented OpenShift Route model if the NiFi protocol and Route behavior can be kept clear and supportable
- probably too far:
  - multiple ingress-controller-specific strategies
  - automatic per-node external host or subdomain generation across many environments
  - a matrix of raw, HTTP, NodePort, LoadBalancer, and ingress permutations all marketed as supported
  - turning Site-to-Site routing into a broad network-programming surface with many low-level knobs

Not accepted:

- do not turn generic Site-to-Site networking into a broad exposure matrix across every ingress, service, and external-DNS style
- do not claim support for arbitrary external routing combinations without tight documentation and validation coverage
- do not broaden this into cross-region replication, DR topology automation, or generic NiFi networking management
- do not let this replace the clearer typed Site-to-Site product paths we already support

Why these are not accepted:

- broad Site-to-Site networking support would expand the chart into a large environment-specific networking surface
- it would be harder to explain, validate, and support than the current typed sender-side model
- it risks moving the product away from smaller, boring, and explainable APIs toward a generic NiFi exposure toolbox

Guardrails:

- prefer one narrow documented model over many knobs
- keep TLS explicit and end-to-end
- keep ownership on the NiFi workload side only; do not build a second control plane for external client routing
- require clear documentation of hostname, proxy-host, and transport expectations before claiming support

Non-goals:

- not a generic Site-to-Site networking framework
- not a full external client-routing abstraction layer
- not a cross-region replication feature
- not a replacement for the existing typed Site-to-Site observability features

### Repository Encryption Support

Idea:

- evaluate whether NiFi-Fabric should expose a small explicit chart surface for NiFi repository encryption

Why this came up:

- comparison charts expose a direct repository-encryption configuration shape
- NiFi-Fabric already has strong TLS, cert-manager, sensitive-properties-key, and trust-bundle surfaces, but does not currently expose an equivalent first-class repository-encryption values model
- repository encryption is a meaningful security feature, so it is worth deciding explicitly whether it belongs in the product surface

Current repo touchpoints:

- `charts/nifi/values.yaml`
- `charts/nifi/templates/statefulset.yaml`
- `docs/manage/tls-and-cert-manager.md`
- `docs/reference/app-chart-values.md`
- `charts/nifi-platform/values.yaml`

What exists today:

- explicit TLS Secret and cert-manager support
- sensitive-properties-key support
- optional additional trust-bundle support
- no obvious first-class repository-encryption values surface in the app chart or platform chart

Accepted direction:

- consider a narrow explicit repository-encryption feature only if it can stay simple, well-documented, and clearly scoped
- if added, keep the public shape small and security-focused, for example:
  - enable or disable repository encryption
  - key identifier
  - Secret reference for the repository keystore material
- document exactly which NiFi repositories are covered, what Secret material is expected, and what the operational rotation story is
- make the feature explicit and operator-owned rather than trying to auto-generate or silently manage long-lived encryption material

Why this is accepted:

- repository encryption is a meaningful security capability rather than just another chart knob
- it fits better than many broader brainstorm items because it directly improves workload security posture
- it can potentially stay within the app chart and explicit Secret model without introducing controller sprawl

Not accepted:

- do not add a broad secret-generation or key-management subsystem for repository encryption
- do not blur repository encryption with general backup, DR, or storage orchestration
- do not over-automate encryption-material rotation unless there is a clearly supportable operational model
- do not add a large matrix of repository-encryption permutations without strong documentation and validation coverage

Why these are not accepted:

- encryption material and rotation quickly become sensitive operational concerns
- too much automation here would widen the platform into a key-management product surface
- the project should prefer explicit operator-owned secret handling over hidden crypto lifecycle behavior

Guardrails:

- keep the API small
- require explicit Secret references for encryption material
- document operational consequences clearly before claiming support
- prefer no feature over a confusing or weakly specified crypto feature

Non-goals:

- not a key-management system
- not a secret-rotation controller for repository encryption
- not a replacement for cluster-wide secret-management tooling

### Dashboard Pack Expansion

Idea:

- expand the current starter observability assets into a slightly broader but still product-shaped dashboard set

Why this came up:

- NiFi-Fabric already ships a starter Grafana dashboard and starter Prometheus alerts
- comparison charts sometimes ship a broader Grafana directory with more runtime-oriented dashboards
- our current dashboard is intentionally focused on NiFi-Fabric lifecycle and controller signals, which is good, but some customers may also expect a clearer runtime-health dashboard for the NiFi workload itself

Current repo touchpoints:

- `ops/grafana/nifi-fabric-starter-dashboard.json`
- `ops/prometheus/nifi-fabric-starter-alerts.yaml`
- `docs/operations/dashboards.md`
- `docs/operations/alerts.md`
- `docs/manage/observability-metrics.md`

What exists today:

- one small starter dashboard focused on:
  - rollout
  - TLS handling
  - hibernation and restore
  - autoscaling
  - controller lifecycle signals
- one starter alerts file
- no broader packaged dashboard set for generic NiFi runtime-health views

Accepted direction:

- keep the existing product-operations dashboard as the primary starting point
- consider adding a second starter dashboard focused on NiFi runtime health, such as:
  - secured NiFi metrics availability
  - cluster-health and readiness-oriented signals
  - core throughput or queue-pressure metrics already exposed through supported metrics modes
  - exporter health when exporter mode is enabled
- if expanded beyond one additional dashboard, keep the pack small and aligned with the metrics we actually expose and support
- document clearly which panels assume:
  - controller scrape wiring
  - native API metrics mode
  - exporter mode
  - environment-specific Prometheus labels

Why this is accepted:

- it improves customer day-2 usability without changing core runtime behavior
- it builds on metrics and signals the product already exposes
- it fits the existing "starter assets" positioning better than many broader feature ideas

Not accepted:

- do not build a large, opinionated dashboard bundle that assumes one Grafana folder model, one datasource layout, or one organization-wide monitoring convention
- do not add dashboards for subsystems the product does not support, such as ZooKeeper
- do not overclaim monitoring support beyond the actual metrics surfaces and modes NiFi-Fabric documents
- do not let dashboards become a substitute for clear docs, alerts, and runbooks

Why these are not accepted:

- broad dashboard packs tend to become stale and environment-specific
- they create support expectations around labels, scrape jobs, and third-party monitoring conventions that are outside the product
- a smaller product-shaped pack is more maintainable and honest

Guardrails:

- prefer a small number of high-value starter dashboards
- keep each dashboard tied to documented metrics surfaces
- make assumptions explicit in docs
- avoid product drift into a generic monitoring bundle

Non-goals:

- not a full observability suite
- not a generic NiFi monitoring pack for every environment
- not a ZooKeeper dashboard set

### Storage Surface Extensions

Idea:

- evaluate a small set of storage improvements that build on the current separate repository PVC model without turning the chart into a generic storage-topology toolkit

Why this came up:

- NiFi-Fabric already provisions separate PVCs for:
  - database repository
  - FlowFile repository
  - content repository
  - provenance repository
- comparison charts expose more granular storage-class and mount-topology controls, including persistent logs and extra backup mounts
- some of that flexibility is useful, but much of it would widen the support surface quickly if added without strong boundaries

Current repo touchpoints:

- `charts/nifi/values.yaml`
- `charts/nifi/templates/statefulset.yaml`
- `docs/reference/app-chart-values.md`
- `docs/dr.md`
- `docs/architecture.md`

What exists today:

- separate PVC sizing for database, FlowFile, content, and provenance repositories
- one shared `persistence.storageClassName` across those PVCs
- writable runtime config and logs are not first-class persistent PVCs in the chart today
- generic `extraVolumes[]` and `extraVolumeMounts[]` are available for operator-provided extensions

Accepted direction:

- improve storage documentation so customers can understand the current repository layout and what is already separated today
- consider per-repository `storageClassName` overrides as the strongest candidate storage enhancement, because it solves a real operational need without requiring arbitrary storage wiring
- consider optional persistent logs only if there is real demand and the operational model stays simple and explicit
- document how operator-provided backup or archive mounts can be layered on top of `extraVolumes[]` and `extraVolumeMounts[]` for environment-specific cases

Why this is accepted:

- per-repository storage classes fit real platform needs
- advanced storage docs improve usability without expanding behavior
- these changes can stay within the current chart model and do not require new controllers or CRDs

Not accepted:

- do not expose a broad arbitrary per-path volume-topology toolkit as a first-class product surface
- do not productize every internal writable directory as a persistent volume just because another chart can
- do not blur storage layout enhancements into backup orchestration or recovery guarantees

Why these are not accepted:

- they would significantly expand the storage test matrix
- they make the chart harder to understand and support
- they would move the product away from its simpler, more explainable platform model

Guardrails:

- prefer storage documentation first
- prefer one clear storage-class override story over many topology knobs
- keep backup and recovery guarantees separate from storage wiring

Non-goals:

- not a generic storage-layout framework
- not per-directory persistence for every NiFi path by default
- not a storage orchestration control plane

### Azure Blob Backup Integration

Idea:

- evaluate whether Azure Blob Storage should appear in the product only as an operator backup pattern, not as a core backup feature

Why this came up:

- teams running on AKS may naturally ask about backing up control-plane exports or other recovery artifacts to Azure Blob Storage
- Blob Storage is a reasonable destination for operator backup workflows, but it is not the same thing as product-owned stateful backup orchestration

Current repo touchpoints:

- `docs/dr.md`
- `docs/architecture.md`
- `hack/export-control-plane-backup.sh`
- `docs/install/helm.md`

Accepted direction:

- document Azure Blob Storage as one possible operator-managed destination for control-plane backup artifacts if AKS-facing guidance would benefit from it
- optionally add a doc example showing how the exported control-plane bundle could be stored in Blob Storage by operator workflow or CI/CD automation
- keep the story focused on backup destination guidance, not on runtime data-plane backup claims

Why this is accepted:

- it fits the existing operator-owned backup model
- it helps AKS users without changing the product’s core architecture
- it can remain documentation and workflow guidance rather than code or controller behavior

Not accepted:

- do not add Azure Blob backup as a first-class NiFi-Fabric feature
- do not add controller logic to push data or PVC state into Blob Storage
- do not imply that Blob Storage export of control-plane artifacts is equivalent to full NiFi repository recovery
- do not make Azure-specific backup workflow a required or primary product path

Why these are not accepted:

- Blob backup is cloud-specific and environment-specific
- product-owned backup export to Blob would widen us into backup tooling and cloud integration
- it would confuse control-plane backup with stateful data recovery

Guardrails:

- keep Azure Blob guidance optional and operator-facing
- clearly distinguish control-plane artifacts from data-plane state
- do not promise durable queue or repository recovery through object storage alone

Non-goals:

- not an Azure backup operator
- not PVC-to-Blob replication
- not a substitute for storage snapshots and recovery planning

## Optional Candidate Features

These are optional ideas that could fit the product if they stay explicit, off by default, and small in scope.

### High Fit

- per-repository storage-class overrides
  - rating: `high`
  - why: solves a real platform need without changing the main product model
- repository encryption support
  - rating: `high`
  - why: meaningful security feature that can stay explicit and Secret-driven
- runtime health dashboard expansion
  - rating: `high`
  - why: improves day-2 usability using signals the product already exposes
- quickstart and demo path visibility improvements
  - rating: `high`
  - why: improves first-use experience without adding runtime complexity
- log-shipper example overlay using `sidecars[]`
  - rating: `high`
  - why: useful operational pattern with very low product-surface risk
- backup and DR documentation improvements
  - rating: `high`
  - why: strengthens customer guidance without widening product ownership
- AKS or Azure Blob operator guidance for control-plane backup artifacts
  - rating: `high`
  - why: helpful cloud guidance that can remain purely optional and doc-driven

### Medium Fit

- configurable exporter deployment replicas
  - rating: `medium`
  - why: useful convenience knob, but exporter remains a secondary metrics path
- optional persistent logs
  - rating: `medium`
  - why: some customers will want it, but it adds storage and lifecycle considerations
- `logging.levels.*` troubleshooting surface
  - rating: `medium`
  - why: useful for debugging, but should stay narrow and temporary in spirit
- `debugStartup` troubleshooting mode
  - rating: `medium`
  - why: can help operators, but easy to overdesign or make confusing
- advanced storage documentation and sizing guidance
  - rating: `medium`
  - why: helpful and low risk, but less impactful than the highest-fit items
- external identity-provider guidance, including Keycloak examples
  - rating: `medium`
  - why: strong documentation value, but mostly a docs-and-examples improvement rather than a product capability change
- Azure DevOps flow documentation and workflow parity exploration
  - rating: `medium`
  - why: useful for some customers, but still secondary to the better-covered provider paths

### Low Fit

- generic external Site-to-Site networking surface
  - rating: `low`
  - why: possible if kept very narrow, but easy to widen into a large networking support matrix
- broad extra ports and services matrix
  - rating: `low`
  - why: turns the chart into a generic exposure toolkit rather than a product install surface
- broad arbitrary storage topology toolkit
  - rating: `low`
  - why: expands the storage support matrix and weakens the simpler chart model

### Optional But Not Recommended

- ZooKeeper support
  - rating: `low`
  - why: introduces a second architecture path that conflicts with the NiFi 2-first design
- automatic auth priority or fallback
  - rating: `low`
  - why: reduces clarity around active auth mode and makes install and upgrade behavior harder to reason about
- product-owned backup orchestration
  - rating: `low`
  - why: widens the platform into backup control-plane behavior and cloud-specific storage concerns
- cross-region replication or DR topology management
  - rating: `low`
  - why: moves the product into broader dataflow and disaster-recovery orchestration

### Exporter Deployment Scaling

Idea:

- consider making the optional metrics exporter deployment replica count configurable

Why this came up:

- NiFi-Fabric already supports an optional exporter metrics mode with its own Deployment, Service, and ServiceMonitor
- the current exporter deployment is fixed at one replica
- some comparison charts expose a simple replica knob for their monitor or exporter deployment

Current repo touchpoints:

- `charts/nifi/values.yaml`
- `charts/nifi/templates/metrics-exporter-deployment.yaml`
- `charts/nifi/templates/servicemonitor.yaml`
- `docs/manage/observability-metrics.md`
- `docs/reference/app-chart-values.md`

What exists today:

- exporter mode already supports:
  - image settings
  - Service settings
  - ServiceMonitor settings
  - auth and TLS settings for upstream scraping
  - optional supplemental flow-status gauges
- exporter Deployment replicas are currently fixed at `1`

Accepted direction:

- if customers need it, add one small explicit setting such as `observability.metrics.exporter.deployment.replicas`
- document clearly when more than one replica is actually useful and what duplicate scraping or duplicate supplemental collection might mean operationally
- keep the feature optional and limited to the exporter deployment only

Why this is accepted:

- it is a small operational convenience knob
- it stays within the existing exporter feature rather than widening the metrics model
- it may help teams that want higher availability for the exporter endpoint itself

Not accepted:

- do not broaden this into a large exporter autoscaling feature set
- do not imply that multiple exporter replicas always improve correctness or signal quality without documenting duplicate-scrape considerations
- do not turn exporter mode into the primary recommended path; `nativeApi` remains the default product recommendation

Why these are not accepted:

- the exporter is already a secondary metrics mode
- extra scaling knobs without clear semantics can create confusion around scrape duplication and operational behavior
- this should stay a small ergonomics improvement, not a new subsystem

Guardrails:

- keep the API to one obvious replica-count field if implemented
- document duplicate-scrape or duplicate-status-gauge implications clearly
- preserve `nativeApi` as the primary supported metrics recommendation

Non-goals:

- not exporter autoscaling
- not a full HA monitoring architecture
- not a replacement for native API metrics

### Advanced Storage Layouts

Idea:

- optionally add a small advanced storage-layouts section later if real customer demand appears for finer repository placement or multi-disk tuning

Why this came up:

- the current app chart already provisions separate PVCs for database, flowfile, content, and provenance repositories
- some comparison charts expose more arbitrary repository-to-volume layouts
- that flexibility can be useful in specialized environments, but it also adds configuration surface and harder support conversations

Current repo touchpoints:

- `charts/nifi/values.yaml` persistence settings
- `charts/nifi/templates/statefulset.yaml` volume claim templates
- `docs/reference/app-chart-values.md`
- `docs/install/helm.md`
- `docs/dr.md`

Potential deliverables:

- add a short advanced section to the values reference or install docs explaining the current storage model and what it already gives customers
- document when the default separate repository PVC layout is enough
- capture the likely next extension only if needed:
  - more explicit per-repository storage classes
  - optional multi-volume content or provenance layouts
  - clearer sizing guidance by workload pattern
- write down the tradeoff clearly before any implementation work:
  - more storage knobs can help specialized customers
  - more storage knobs also expand the test matrix and support burden

Guardrails:

- keep the default storage path simple
- do not add arbitrary volume topology unless there is real customer pull
- prefer documentation first over new chart API

Non-goals:

- not a commitment to multi-disk striping
- not a storage-operator feature set
- not a replacement for platform-owned snapshot, backup, or restore tooling

### External Identity Provider Guidance

Idea:

- expand optional external identity-provider guidance, especially around Keycloak-backed OIDC, while keeping provider ownership outside the product

Why this came up:

- the repo already includes Keycloak-oriented OIDC evaluation examples
- customers often need help with claim mapping, bootstrap admin setup, group naming, TLS trust, and callback URLs more than they need another auth feature
- this is a strong documentation opportunity because the product already supports standard OIDC inputs and external claim-group mapping

Current repo touchpoints:

- `docs/manage/authentication.md`
- `docs/install/advanced.md`
- `examples/oidc-values.yaml`
- `examples/oidc-group-claims-values.yaml`
- `examples/oidc-kind-values.yaml`
- `examples/oidc-kind-initial-admin-group-values.yaml`
- `examples/oidc-external-url-values.yaml`
- `examples/openshift/oidc-managed-values.yaml`

Potential deliverables:

- add a dedicated section or doc that explains the OIDC setup contract in customer terms:
  - discovery URL
  - client ID and Secret
  - user identity claim
  - group claim
  - initial admin identity or initial admin group
  - proxy host or Route host alignment
- add a Keycloak example walkthrough that stays clearly optional and external:
  - NiFi-Fabric consumes OIDC metadata and claims
  - Keycloak remains customer-managed
  - realm, client, mapper, and group modeling stay outside product ownership
- add clearer guidance for `authz.mode=externalClaimGroups`:
  - how external groups map into named NiFi bundles
  - how to keep group names stable
  - how to reason about bootstrap admin versus ongoing group-based access
- document CA trust expectations for private IdPs through the existing additional trust bundle path

Guardrails:

- do not let the product become a Keycloak integration operator
- keep the OIDC contract provider-agnostic even if Keycloak is the best example
- do not imply NiFi-Fabric manages realms, clients, or identity-provider lifecycle

Non-goals:

- not Keycloak provisioning
- not identity sync outside NiFi's own auth model
- not a provider-specific API surface in the chart

### Azure DevOps Flow Direction

Idea:

- improve Azure DevOps flow documentation and examples as an optional Git-backed path, and consider deeper workflow parity only if real demand appears

Why this came up:

- the repo already has an `azureDevOps` Flow Registry Client provider shape
- the values reference already lists Azure DevOps as a supported provider in the Flow Registry Client catalog
- this is easy to miss because GitHub, GitLab, and Bitbucket have more visible examples and validation flows today

Current repo touchpoints:

- `examples/azure-devops-flow-registry-values.yaml`
- `examples/README.md`
- `docs/manage/flow-registry-clients.md`
- `docs/manage/flows.md`
- `docs/reference/app-chart-values.md`
- `charts/nifi/templates/_flow_registry.tpl`

Potential deliverables:

- surface the Azure DevOps example more clearly in docs and the examples index
- add a small customer-facing explanation of what the current provider shape expects:
  - API URL
  - organization
  - project
  - repository name
  - OAuth2 access-token provider name
  - web client service name
- document the current product boundary clearly:
  - Flow Registry Client catalogs can render Azure DevOps-backed clients
  - deeper workflow ownership or parity with every other Git provider is future work, not an implied GA guarantee
- if real demand appears, evaluate whether versioned-flow import should gain broader parity for Azure DevOps-backed sources

Guardrails:

- keep this in the Flow Registry Client slice
- do not expand into a general Azure DevOps platform integration story
- avoid claiming parity that the runtime-managed import slice does not yet implement

Non-goals:

- not a broad Azure DevOps product surface
- not pipeline integration
- not organization or project provisioning
