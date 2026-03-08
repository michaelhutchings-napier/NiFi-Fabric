# AGENTS.md

## Project intent
This repository is a NiFi 2-first Kubernetes platform project.
It is NOT a NiFiKop clone.
It must stay simpler than NiFiKop and avoid feature sprawl.

## Core principles
- Prefer NiFi 2 native Kubernetes capabilities over custom controller logic.
- Prefer Helm for standard Kubernetes resources.
- Prefer a thin controller only for lifecycle/safety logic Helm cannot do well.
- Keep CRDs minimal.
- Design for GitOps first.
- Design for AKS first, OpenShift-friendly second.
- Security and TLS by default.
- Cert rotation and restart safety are first-class requirements.
- Avoid magic and hidden behavior.

## Working rules
- Before writing code, update architecture docs if the design changes.
- Do not introduce new CRDs unless justified in docs/api.md and an ADR.
- Do not add support for NiFi 1.x.
- Do not add advanced dataflow/user/registry management in MVP.
- Keep APIs boring and explainable.
- Prefer explicit status conditions.
- Prefer small, testable reconciliation loops.
- Prefer controller-runtime conventions.

## Quality bar
- Every major design decision needs rationale.
- Every reconciliation behavior needs test coverage notes.
- Every rollout/restart path must describe failure handling.
- Keep the operator understandable by one engineer reading the code for the first time.