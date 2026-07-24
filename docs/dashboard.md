# Dashboard operator guide

The operator console exposes the Increment 2 control plane as 17 named operational areas. Each area is backed by controller APIs and either performs routine actions directly or displays retained evidence from the durable workflow.

## Areas

1. **Setup and health** — first-run checklist, host/controller health, queue summary, users, audit, backups, restore dry runs, and update preflight.
2. **Repositories** — repository registration, enable/disable, sync, diagnostics, trusted project identity, and memory namespace.
3. **Capability packs** — catalog, versions, prerequisites, installation lifecycle, Repo Doctor scans, proposal review, typed assignment configuration, preview, apply, and rollback.
4. **Jobs** — task submission, job timeline, contracts, risk, phases, artifacts, findings, documentation manifests, golden approvals, and evidence graph navigation.
5. **Quality** — retained findings, lifecycle actions, waiver reauthentication, Test Designer, golden/rehearsal, and QC evidence.
6. **Documentation** — Documentation Agent manifests, policy profile, impact simulation, source mappings, checks, findings, and publication readiness.
7. **Code intelligence** — index status, graph queries, context manifests, baseline/differential verification, cache simulation, and trusted snapshot maintenance.
8. **Forges** — GitHub, GitLab, and local Git profile diagnostics, mappings, webhooks, synchronization, credential-reference handling, and normalized objects.
9. **Runners and Windows** — Linux worker profiles, Windows simulator/remote worker profiles, bounded operation classes, capacity, protocol evidence, and gated capability support.
10. **Models and agents** — local model manifests, provider profiles, endpoint and capability probes, egress simulation, routing, benchmark history, and safe optimization evidence.
11. **Scheduling and resources** — topology, resource profiles, queue simulation, retained scheduler decisions, recurring schedules, notifications, and automation requests.
12. **Policy and risk** — OPA bundles, tests, simulations, activation/rollback history, deterministic policy decisions, risk routing, waivers, and documentation impact support.
13. **Security and SBOM** — security capability-pack scan profiles, SBOM release-diff rehearsals, retained security findings, suppression review state, FMEA hooks, and release evidence.
14. **Evaluation** — immutable historical datasets, isolated evaluation runs, comparison reports, reproducibility hashes, and review-only promotion evidence.
15. **Memory and evidence** — project-scoped memory search, lifecycle actions, export validation, provenance, and evidence traceability entry points.
16. **Observability** — local collector status, redaction policy, event timeline, support bundle manifests, hashes, and external OTLP default-off status.
17. **Configuration** — descriptor search, effective values, drafts, review/apply, imports/exports, dependency validation, revision history, and rollback.

## Current validation boundary

The App-level accessibility test asserts that all 17 named navigation targets are present and exercises the Documentation, Security and SBOM, Evaluation, Observability, Models and agents, Jobs/evidence, setup, and bootstrap surfaces with deterministic fixture data.

The remaining Milestone 7 dashboard work is broader:

- direct filtered configuration links from every domain page to the fields it consumes;
- reciprocal links from each configuration field back to consuming feature/status views;
- browser E2E and keyboard traversal across every required page;
- final evidence that no page is a placeholder and every routine feature has UI/API/CLI parity.
