# Complete Increment 2: intelligence, policy, providers, and operability

This ExecPlan is a living document. Keep `Progress`, `Surprises & Discoveries`, `Decision Log`, `Outcomes & Retrospective`, and the revision notes current whenever implementation changes direction or stops. The authoritative contracts are `project.md` for the completed Increment 1 appliance and `LOCAL_CODE_MAINTAINER_INCREMENT_2_GOAL.md` for this increment. The machine-readable requirement map is `config/increment-2-coverage.json`.

## Purpose / Big Picture

Increment 2 turns the working local maintenance appliance into an explainable heterogeneous code-maintenance platform without weakening Increment 1. An operator can onboard PHP, .NET/Windows, R, and mixed repositories through a browser; inspect evidence-backed Repo Doctor proposals; assign pinned capability packs; clarify and approve a task contract; see risk, baseline, code impact, context, tests, documentation, model, scheduling, policy, and egress decisions; run the workflow with deterministic local fakes; and trace the resulting code, verification, findings, documentation, memory, commit, forge result, and SBOM through one evidence graph.

The controller remains the sole workflow-state authority. New parsers, forges, providers, workers, policy engines, schedulers, indexes, and caches stay behind narrow domain interfaces. The browser and CLI call the same controller services. Workers never receive a Docker socket or forge credentials, remote inference is disabled by default, and no endpoint accepts arbitrary commands, images, mounts, paths, networks, model arguments, provider URLs outside approved profiles, or unrestricted environment variables.

The locally testable outcome uses a protocol-accurate fake provider gateway, fake GitHub and GitLab services, local bare Git, a narrow simulated Windows worker, small fixture repositories, and fake local inference. Real forge credentials, a licensed Windows worker, proprietary hardware, remote-provider accounts, signing infrastructure, and large model weights remain finite operator-only validations with safe fakes and exact procedures.

## Progress

- [x] (2026-07-20 20:46Z) Read the complete Increment 2 contract, repository guidance, base product contract, completed Increment 1 plan, current migrations, configuration/storage/API/CLI/UI seams, and current package inventory.
- [x] (2026-07-20 20:46Z) Established the pre-change baseline: containerized Go tests passed, frontend tests passed with two tests and no critical accessibility violations, Compose validation passed, and generated OpenAPI client drift passed after the required per-container `npm ci`.
- [x] (2026-07-20 20:46Z) Created this self-contained living plan and the machine-readable requirement-to-implementation/test matrix.
- [x] (2026-07-20 21:05Z) Added the trusted pure configuration registry for all 13 Increment 1 system-document fields, explicit bootstrap-only descriptors, strict unknown-key/scope/value rejection, deterministic seven-layer precedence with provenance, write-only redaction, and deterministic complete snapshot hashing; focused and full Go tests pass.
- [ ] Milestone 1 — replace the single-document configuration surface with the complete typed registry, seven-scope effective-value resolver, immutable job snapshots, drafts/review/apply/rollback/import/export/dry-run/prerequisite APIs, CLI parity, and dedicated accessible workbench.
- [ ] Milestone 2 — add incremental code intelligence, deterministic Context Compiler, baseline/differential verification, test-impact analysis, isolated content-addressed caches, and their status/query/rebuild browser surfaces.
- [ ] Milestone 3 — add evidence-backed Repo Doctor onboarding, signed/checksummed capability-pack lifecycle, PHP/R/security packs, and catalog/assignment/upgrade/rollback UI.
- [ ] Milestone 4 — normalize forge behavior across existing GitHub, GitLab, and local bare Git; add the constrained simulated Windows worker and heterogeneous fixtures; preserve credential isolation and provider-specific metadata.
- [ ] Milestone 5 — add task contracts, deterministic risk routing and waivers, versioned agent contracts, independent Test Designer, golden/rehearsal gates, Documentation Agent/QC, and OPA-backed policy authoring/simulation/activation/rollback.
- [ ] Milestone 6 — add resource scheduling, the provider-neutral model gateway and safe runtime laboratory, evidence graph, isolated historical evaluation, OpenTelemetry, bounded support bundles, and complete operational pages.
- [ ] Milestone 7 — close every matrix row, run migration/backup/restore/restart and full Increment 1+2 acceptance, complete security/property/fuzz/browser/performance/soak evidence, update operator documentation, and list only genuinely external validations as operator-only.

## Surprises & Discoveries

- Observation: the Increment 2 contract tells Codex to read `LOCAL_CODE_MAINTAINER_GOAL.md`, but that file does not exist in the tree; `project.md` contains the matching completed base-appliance contract and repository guidance names it authoritative.
  Evidence: `git ls-files` and `rg --files` find `project.md` and the two untracked increment goal files, but no `LOCAL_CODE_MAINTAINER_GOAL.md`. This plan therefore treats `project.md` as the base contract without inventing an alias file.
- Observation: Increment 1 configuration is one append-only whole-system JSON document, not a setting registry.
  Evidence: `internal/config/types.go` defines `System`; `config_revisions` stores before/after documents; the API exposes only get, validate, revise, list revisions, and rollback. This is a useful immutable audit backbone but cannot express scoped provenance, drafts, secrets, prerequisites, or optimistic per-scope edits.
- Observation: the existing console has 10 top-level pages and configuration appears only as a revision/rollback table under Administration.
  Evidence: `web/src/OperatorConsole.tsx` defines ten `PageID` values. Increment 2 requires 17 named, navigable operational areas plus typed configuration controls.
- Observation: the tool containers intentionally use a fresh in-memory `/src/web/node_modules` for each run.
  Evidence: `npm run check:api` alone failed with `openapi-typescript: not found`; `npm ci && npm run check:api` passed. Every recorded frontend/container gate must install the locked dependencies in that invocation.

## Decision Log

- Decision: preserve `config_revisions` and build an additive registry/scoped-value/draft/snapshot model around it.
  Rationale: the existing append-only revision ledger, diff, audit, rollback, backup, and restore behavior is proven Increment 1 functionality. Additive migration avoids destructive conversion and lets the legacy whole-system document remain a compatibility projection while new code uses typed descriptors and effective values.
  Date/Author: 2026-07-20 / Codex
- Decision: use stable dotted setting keys and JSON-compatible typed values, with descriptors compiled into the trusted controller binary and mutable values stored outside worktrees.
  Rationale: compiled descriptors make type, scope, role, secret, dependency, apply, and dry-run behavior deterministic and reviewable. Values can be exported and migrated without allowing repository content to define trusted policy or executable hooks.
  Date/Author: 2026-07-20 / Codex
- Decision: implement Increment 2 by domain slices that include schema, storage, service, API/OpenAPI, CLI, UI, authorization, audit, tests, and docs before a slice is marked complete.
  Rationale: the contract explicitly forbids deferring the browser or configuration surface to a cosmetic final phase. Vertical slices keep API/CLI/UI behavior aligned and make the coverage matrix enforceable.
  Date/Author: 2026-07-20 / Codex
- Decision: use deterministic in-process or loopback fakes for every external family, and keep real adapters disabled until an administrator explicitly creates and probes an approved profile.
  Rationale: CI must prove protocols, redaction, SSRF defenses, capability negotiation, fallbacks, costs, retries, and failure closure without contacting real providers or requiring secrets.
  Date/Author: 2026-07-20 / Codex
- Decision: keep code intelligence controller-local and project-separated in SQLite, with derived blobs keyed by repository/blob/tool identity.
  Rationale: this matches the single-host deployment, existing durable store, backup/recovery model, and namespace boundary. Tree-sitter/SCIP/read-only LSP adapters can enrich data without becoming workflow authorities or receiving unrestricted execution.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

Increment 2 is in implementation. The base appliance is healthy at the starting commit and no feature claims have been made yet. The first stopping point establishes a reproducible baseline, an additive architecture direction, a seven-milestone dependency order, and machine-readable rows for every major contract section and every definition-of-done item.

## Context and Orientation

The repository root is `/srv/coder`. `cmd/controller` assembles the trusted control plane; `internal/api` owns the REST/OpenAPI surface; `internal/storage` defines ports and `internal/storage/sqlite` implements them; `internal/workflow` is the durable coordinator; `internal/config` currently owns the Increment 1 system document; `cmd/maintainctl` is the recovery/automation CLI; and `web/src/OperatorConsole.tsx` is the browser console. Migrations are embedded from `internal/storage/sqlite/migrations` and are forward-only. External implementations live behind interfaces such as the runner, model, repository, memory, and Git bridge ports.

A configuration descriptor is immutable trusted metadata for one stable setting key: type/schema, help text, category, allowed scopes, safe default, secret classification, required role, apply semantics, dependencies, prerequisites, validation and dry-run capability, and UI hints. A scoped value is a typed override at one of seven ordered scopes: built-in default, system, capability pack, project, named environment/runner, job template, and one-job override. An effective value is the deterministic winner plus every contributing value and provenance. An immutable job configuration snapshot records all effective values and descriptor versions when the job is accepted, so later configuration edits cannot change a running or historical job.

A capability pack is a versioned, checksummed declarative bundle of configuration fragments, toolchain identities, verifier classes, service needs, documentation rules, risk hints, and compatibility metadata. It never contains arbitrary shell, images, mounts, networks, capabilities, paths, or environment fields. Repo Doctor parses untrusted repository evidence in bounded sandboxes and produces confidence-rated proposals; it never applies a pack or configuration automatically.

The code-intelligence index is derived, project-separated data with provenance and partial-failure records. The Context Compiler selects bounded, deterministic stage packets from approved sources and records why each range was selected, its trust and cost, and the reserved output budget. Baselines classify verification observations as pre-existing, resolved, new, changed, or indeterminate; a targeted inner loop may accelerate work but a fresh full required suite still gates publication.

The provider gateway is the only inference-egress path. Provider, endpoint, model, and route profiles are versioned records with write-only credentials, probed capabilities, trust/data-classification policy, budgets, costs, circuit state, and approved endpoint/network behavior. Model names never imply capabilities. Remote inference starts disabled and every outbound packet is scanned, classified, bounded, manifested, and policy-approved.

## Plan of Work

Milestone 1 begins with a `config.Registry` and exhaustive compiled descriptor inventory for every currently configurable Increment 1 field plus the Increment 2 settings introduced as later slices land. Add pure functions for descriptor validation, canonical value validation, seven-scope precedence, provenance, redaction, export/import, dependency checks, and immutable snapshot hashing. Migration 16 adds descriptor-version records, scoped values with optimistic versions, drafts and draft entries, prerequisite/dry-run results, revisions linked to exact scopes, and job snapshots. The service layer performs validation, review, apply, rollback, and audit transactionally. Existing `/config` operations remain compatible while new `/config/descriptors`, `/values`, `/effective`, `/drafts`, `/validate`, `/dry-runs`, `/prerequisites`, `/revisions`, `/export`, and `/import` routes receive typed OpenAPI schemas and ETags. `maintainctl config` gains the same operations. The browser gets a dedicated Configuration page with registry search, basic/advanced views, scope selection, inheritance and provenance, effective preview, diff, validation, prerequisites, dry run, drafts, review/apply/discard, revisions/rollback, redacted export/import/reset, apply-state badges, keyboard operation, and accessible errors. A machine-readable inventory test fails if any descriptor lacks service/API/CLI/UI/test evidence or if a runtime setting has no descriptor.

Milestone 2 adds migration-backed project-local code-intelligence state: repository revisions, blob identities, parse facts, symbols, references, test relationships, dependency relations, index runs, failures, and coverage. Parser inputs are bounded and hostile; language adapters are replaceable and failures remain visible without corrupting prior good facts. The service supports incremental refresh, query, status, coverage, rebuild, and cancellation. The Context Compiler consumes only authorized indexed facts, project memory, task/risk/policy records, and bounded source ranges; it deduplicates and reserves output capacity, persists only a redacted manifest, and supports manifest inspection and comparison. Baseline records and observations bind to repo/config/tool/pack identities and enforce the five-way classification. Test impact records selected tests, reasons, confidence, and omissions while the final-gate policy always schedules a fresh complete required suite. Cache namespaces and keys include project, repository/blob/config/tool/pack/platform identities; integrity, quotas, retention, and audited purge are enforced.

Milestone 3 introduces Repo Doctor scan/proposal/evidence tables and a stepwise browser onboarding flow. Bounded detectors inspect manifests, locks, CI, languages, tool configs, docs, services, and line-ending/platform hints, display confidence/evidence/diffs, and require explicit acceptance. Capability packs use a schema and checksum verifier plus a catalog/assignment lifecycle with trust, compatibility, version comparison, upgrade dry run, apply, rollback, and effective-configuration preview. Ship declarative `php83-intranet`, `r-statistical-validation`, and `sbom-fmea-security` packs with pinned tool identities and fixture-backed checks. No pack may expand runner authority; unavailable tools/services produce prerequisites or skipped-with-policy results, never silent substitution.

Milestone 4 generalizes repository operations into normalized forge objects with explicit provider metadata, cursor/pagination/rate/retry/idempotency/webhook/credential-rotation behavior. Preserve the proven GitHub path, add protocol-accurate fake GitLab and local bare-Git adapters, and expose forge profiles, credentials, mappings, probes, webhook health, cursors, and diagnostics in the browser. Add `windows-dotnet-labautomation` as a declarative pack and a narrow authenticated simulated-Windows worker contract with fixed operation classes, artifact schemas, time/resource limits, and no arbitrary PowerShell. The real Windows adapter remains operator-only. Heterogeneous PHP, R, .NET, mixed, GitHub, GitLab, and local-Git fixtures prove normalization and restart/idempotency behavior.

Milestone 5 adds a versioned task-contract schema and Clarifier stage before implementation. Questions, assumptions, answers, acceptance requirements, edit boundaries, constraints, and approval history are durable and operator-editable. Deterministic risk rules calculate a level and routing requirements from repository/task/pack/policy evidence; explanations are stored, increases are automatic, and decreases require an authorized, reasoned, expiring waiver. Versioned agent input/output contracts require evidence-linked claims and reject hidden reasoning fields. Medium/high-risk work invokes an independent Test Designer whose plan is implemented and verified separately. Golden/rehearsal fixtures gate changes to prompts, packs, policies, routes, parsers, and agent contracts. Documentation has its own policy, agent, render/link/source-map/quality tools, findings, previews, and QC stage. OPA bundles are pinned and versioned; structured templates are the normal editor, advanced Rego is authorized separately, simulation/tests precede activation, rollback is append-only, and errors fail closed.

Milestone 6 adds persistent resource inventory, leases, queue decisions, fairness, deadline, model-residency, provider-limit, and co-residence constraints. Every scheduling decision is explainable and unsafe combinations are rejected. The provider gateway implements local llama plus fake-contract-tested OpenAI Responses, OpenAI Chat, generic compatible, Azure, Anthropic, Gemini/Vertex, and Bedrock families behind one neutral call contract. Endpoint admission enforces HTTPS where applicable, scheme/host/port/path allow-lists, DNS/IP revalidation, private/link-local/metadata denial, redirect policy, size/time limits, write-only secrets, data classifications, egress scanning/manifests, trust tiers, budgets, costs, circuit breakers, batch isolation, resumability, capability probing, and non-escalating fallbacks. The runtime benchmark lab records exact binary/model/hardware/settings identity, compares quality and performance, and keeps experimental profiles off by default and reversible. The evidence graph stores typed provenance edges from request through publication/SBOM. Historical patch evaluation uses isolated ephemeral memory/cache namespaces and reproducible datasets. OpenTelemetry spans/metrics/log correlation are bounded, redacted, local by default, optionally exported only through approved disabled-by-default OTLP profiles; support bundles use the same redaction policy.

Milestone 7 reconciles all browser information architecture into at least the required 17 named areas: Setup and health, Repositories, Capability packs, Jobs, Quality, Documentation, Code intelligence, Forges, Runners and Windows, Models and agents, Scheduling and resources, Policy and risk, Security and SBOM, Evaluation, Memory and evidence, Observability, and Configuration. Each page must perform routine operations rather than display placeholders. Complete migration upgrades from retained databases, backup/restore/rollback of every new domain, restart reconciliation, property/fuzz/security/isolation tests, protocol failure tests, browser E2E and keyboard/accessibility runs, performance thresholds, and bounded soak tests. Finish the operator, provider, Windows, pack-authoring, security, privacy, schema, migration, troubleshooting, and external-validation documents. The final acceptance report links every row in `config/increment-2-coverage.json` to implementation and current test evidence.

## Concrete Steps

Run all commands from `/srv/coder`. This development host requires `sudo` for its rootful Docker daemon; a properly configured rootless host can omit it. Use the supported containerized path and install frontend dependencies inside every ephemeral web-tool invocation:

    sudo docker compose run --rm go-tool gofmt -w cmd internal
    sudo docker compose run --rm go-tool go test ./...
    sudo docker compose run --rm go-tool go test -race ./...
    sudo docker compose run --rm go-tool go vet ./...
    sudo docker compose run --rm web-tool sh -c 'npm ci && npm test'
    sudo docker compose run --rm web-tool sh -c 'npm ci && npm run build'
    sudo docker compose run --rm web-tool sh -c 'npm ci && npx redocly lint ../internal/api/openapi.yaml'
    sudo docker compose run --rm web-tool sh -c 'npm ci && npm run check:api'
    sudo docker compose config --quiet
    ./scripts/acceptance.sh
    sudo docker compose build controller maintainctl runnerd

For each slice, first add failing pure/domain and migration tests, then implement storage and service behavior, then API/OpenAPI and CLI, then the browser workflow and accessibility regression. Run the narrow package/test after each edit, regenerate API types only with the documented npm command, and commit the slice after its coverage rows and this plan are updated.

## Validation and Acceptance

Milestone 1 passes when every Increment 1 setting is described, all seven scopes resolve deterministically with provenance, secrets redact everywhere, concurrent edits conflict through ETags/versions, drafts and reauthentication gates work, rollback is additive, job snapshots cannot change, and UI/API/CLI yield byte-equivalent canonical effective configuration. Import and export are bounded and schema/version checked. Browser tests exercise typed fields, dependency errors, focus/error association, keyboard apply/rollback, and safe/basic versus advanced views.

Milestones 2–4 pass when hostile parser and archive fixtures cannot escape limits or namespaces; incremental facts, contexts, baselines, impacts, and caches bind to exact identities; a new failure cannot be classified away; full final verification cannot be skipped; Repo Doctor never auto-applies; packs are checksummed, pinned, reversible, and authority-neutral; GitHub behavior is unchanged; GitLab/local Git normalize and recover correctly; and the simulated Windows worker rejects unregistered operations and returns deterministic artifacts.

Milestones 5–6 pass when task approval precedes implementation, medium/high risk deterministically invokes the required independent stages, risk lowering and policy activation are permissioned/audited/expiring/reversible, agent and documentation claims point to stored evidence, scheduler decisions prevent unsafe co-residence, every provider family passes the same fake conformance suite, forbidden egress fails closed before DNS/connect, fallbacks never raise data exposure or authority, costs/budgets/circuits survive restart, optimizations cannot activate without quality evidence, evaluation cannot see project memory/cache, evidence paths are complete, and telemetry/support bundles contain no seeded secrets.

Final acceptance reruns all Increment 1 checks plus every Increment 2 unit, property, fuzz, integration, migration, browser, E2E, performance, soak, security, backup/restore, restart, and Compose/image gate. The 14-step browser-to-publication scenario in the Increment 2 contract must run with local fakes. External checks are not claimed unless actual authorized infrastructure is available; their fake evidence and exact finite operator procedure must still be present.

## Idempotence and Recovery

Every migration is forward-only and transactional. New state-changing services accept expected versions and idempotency keys where retries can occur. Registry apply, pack assignment, policy activation, baseline acceptance, task approval, provider/profile changes, cache purge, evaluation launch, scheduling leases, and external publication append audit in the same transaction as durable state. Rollback creates a new revision or assignment rather than mutating history. Derived indexes and caches are rebuildable from exact identities; partial runs never replace the last complete generation. Secrets are never recoverable through APIs or exports and restoration requires operator-supplied secret material.

Jobs retain their immutable configuration, task, policy, pack, route, provider-capability, baseline, context, and scheduler snapshots. On restart the controller reconciles leases and uncertain external effects from durable idempotency records instead of repeating them blindly. Provider batches, forge webhooks/polling, indexing, evaluation, and telemetry export use cursors and bounded retries. Backup manifests version every new table/artifact class and restore defaults to dry-run with checksum/schema/compatibility validation.

## Artifacts and Notes

Pre-change baseline at commit `f318466`:

    $ sudo docker compose run --rm go-tool go test ./...
    all packages passed

    $ sudo docker compose run --rm web-tool sh -c 'npm ci && npm test'
    1 test file, 2 tests passed; no critical accessibility violations

    $ sudo docker compose config --quiet
    exited 0

    $ sudo docker compose run --rm web-tool sh -c 'npm ci && npm run check:api'
    generated OpenAPI client matched

The first unauthenticated `docker compose` invocation failed because this host user cannot access `/var/run/docker.sock`; this matches the known Increment 1 development-host limitation and all successful baseline commands used `sudo`. The initial `npm run check:api` without `npm ci` also failed as expected for an ephemeral tool container and was rerun through the documented locked-install path.

## Interfaces and Dependencies

New domain packages should expose narrow, implementation-neutral interfaces. Expected packages include `internal/configregistry`, `internal/intelligence`, `internal/contextcompiler`, `internal/baseline`, `internal/testimpact`, `internal/cache`, `internal/repodoctor`, `internal/packs`, `internal/forges`, `internal/taskcontract`, `internal/risk`, `internal/policy`, `internal/testdesigner`, `internal/documentation`, `internal/scheduler`, `internal/providers`, `internal/evidence`, `internal/evaluation`, and `internal/observability`. Names may be consolidated when adjacent concepts share a cohesive boundary, but domain packages must not import browser, Docker, forge, model-provider, Hermes, Windows, or parser implementations.

Prefer the standard library and existing SQLite dependency. Add maintained pinned libraries only where they materially supply a protocol/parser/OPA/OpenTelemetry implementation that cannot be safely reproduced, record licenses and source identities, and update `go.sum` or `web/package-lock.json`. No operational `latest` tag is allowed. Generated OpenAPI clients and built web assets are never hand-edited.

Revision note (2026-07-20 20:46Z): created the Increment 2 plan after a full contract and architecture audit. Recorded the missing base-goal alias, Increment 1 configuration/UI gaps, additive registry decision, seven-milestone dependency order, external-validation boundary, and clean pre-change Go/frontend/Compose/OpenAPI baseline.

Revision note (2026-07-20 21:05Z): completed the pure registry/resolver foundation of Milestone 1. The registry now inventories every Increment 1 `System` field, distinguishes immutable bootstrap values, validates trusted metadata and values, resolves all seven scopes with provenance independent of input order, redacts secrets, rejects unknown keys, and hashes complete snapshots. Persistence, application services, API/CLI, and the workbench remain open.
