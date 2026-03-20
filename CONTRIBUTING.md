# Contributing

Thanks for contributing to NiFi-Fabric.

NiFi-Fabric is a NiFi 2-first Kubernetes platform. It is intentionally smaller than a broad NiFi operator stack, so contributions should prefer simple, explainable APIs and avoid feature sprawl.

## Before You Start

Please read:

- [README](README.md)
- [Start Here](docs/start-here.md)
- [Architecture Summary](docs/architecture.md)
- [Compatibility](docs/compatibility.md)

For design guardrails, this repository follows the rules in [AGENTS.md](AGENTS.md).

## Contribution Flow

1. Fork the repository or create a feature branch from `main`
2. Make your change in a small, reviewable slice
3. Update docs when user-facing behavior or design changes
4. Run the relevant checks locally
5. Open a pull request against `main`

Please do not push directly to `main`. Use pull requests for all changes.

## Design Expectations

- keep the standard install path simple
- prefer Helm for ordinary Kubernetes resources
- keep the controller thin and safety-focused
- avoid adding new CRDs without clear design rationale
- avoid turning the project into a NiFiKop clone
- keep APIs boring, explicit, and explainable

If your change affects architecture or product shape, update the architecture docs first.

## Local Checks

Run the checks that match your change. Common checks are:

```bash
go test ./...
helm lint charts/nifi
helm lint charts/nifi-platform
helm template nifi charts/nifi-platform -f examples/platform-managed-cert-manager-quickstart-values.yaml
git diff --check
```

If your change affects docs only, a smaller docs-focused check is fine.

## Pull Request Notes

Good pull requests usually include:

- a short explanation of the problem
- a small summary of what changed
- notes on docs updates
- exact validation commands used
- any known limits or follow-up work

Use a draft PR if you want early feedback.

## Questions

If you are unsure whether a change fits the project direction, open an issue or a draft PR first. Small alignment early is much easier than a large redesign later.
