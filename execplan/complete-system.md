# Build the local multi-project code maintenance appliance

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept current while implementation proceeds. The product contract is the checked-in `project.md`; this plan restates the implementation path and evidence needed to make that contract operational.

## Purpose / Big Picture

The finished repository runs a self-hosted maintenance service that accepts an issue or free-form code task, checks out an isolated disposable worktree, obtains dependencies only in an explicitly networked phase, asks an implementation agent for a minimal patch, verifies the exact patch with deterministic commands, asks a separate read-only quality-control agent to review it, repairs supported blocking findings, and pauses for an authenticated operator before any publication. A browser dashboard at `http://127.0.0.1:8080` is the normal interface; `maintainctl` provides the same operations for bootstrap, recovery, and automation. The default evaluation profile uses fake inference, memory, execution, and GitHub adapters plus a local bare Git remote, so the complete lifecycle is testable without credentials or model weights.

The security boundary matters as much as the workflow. The controller is the sole workflow authority but cannot submit arbitrary container specifications. It calls a narrow `runnerd` service whose server-side policy fixes images, mount roots, networks, capabilities, resource limits, and timeouts. Implementation, verification, and QC workers are separate non-root containers with no Docker socket, GitHub credentials, host home directory, controller database, other worktrees, or general internet. Only the GitHub bridge may handle GitHub credentials, and it never shares them with a worker. The model supervisor accepts only versioned allow-listed model profiles and owns at most one `llama-server` child at a time. OpenViking and Hermes remain replaceable integrations behind narrow controller interfaces.

## Progress

- [x] (2026-07-20 09:30Z) Read the complete product contract, inspected the initially empty repository, and verified the available host toolchain and Docker support.
- [x] (2026-07-20 09:35Z) Selected a small Go control plane with SQLite, a statically built TypeScript dashboard, narrow HTTP/Unix-socket service contracts, and deterministic in-process fakes for default tests.
- [x] (2026-07-20 10:31Z) Milestone 1 foundation: repository conventions, pinned containerized Go/Node toolchains, system/QC schemas, versioned SQLite migration and WAL store, audited configuration apply/validate/rollback, durable queue leases, controller API and valid OpenAPI contract, generated typed frontend client with drift gate, replayable SSE, React/TypeScript UI shell, narrow fake adapters, and restart/reclaim tests for every resumable phase. The complete foundation unit, migration, API, frontend, schema, image-build, live-migration, CLI, and runtime acceptance run passed.
- [ ] Milestone 2 secure execution (completed 2026-07-20 10:53Z: authenticated narrow runnerd API over a mode-0600 Unix socket, mode-0600 token-file authentication, strict request/response bounds, server-owned digest/mount/network/resource policy, project-scoped dependency cache paths, fake executor, controller-side Unix client, containment/race/API tests, hardened no-network mock image and live probes; remaining: dedicated rootless worker-daemon backend, immutable real worker images, worktree lifecycle, verifier command classes and language runners, content-addressed artifacts, and the offline fixture integration test).
- [ ] Milestone 3 inference and agents: implement model manifests/supervision, pinned llama.cpp build, separate implementation and QC images/prompts/contracts, locked QA criteria, finding lifecycle, repair loop, and deterministic fake-model acceptance workflow.
- [ ] Milestone 4 publication: implement GitHub App authentication, mirror synchronization, local bare provider, publication policy, protected-path checks, webhook validation, upstream movement handling, and idempotent draft publication.
- [ ] Milestone 5 memory and scheduling: implement repository-scoped memory namespaces, quarantine and promotion, provenance, retrieval traces and context budgets, OpenViking adapter/profile, Hermes tools, schedules, and reviewed skill proposals.
- [ ] Milestone 6 product completion: finish every dashboard workflow, RBAC and reauthentication, backup/restore, upgrades and rollback, diagnostics, observability, accessibility, documentation, SBOM/license inventory, hardened Compose profiles, and the full acceptance suite.
- [ ] Run a requirement-by-requirement completion audit against every item in `project.md`, recording commands, results, limitations, and operator-only tests; do not claim completion while any evidence is missing.

## Surprises & Discoveries

- Observation: the initial repository contained only `project.md`; there was no implementation, local guidance, or prior plan to preserve.
  Evidence: `git ls-files` returned only `project.md` at commit `d6661ae`.
- Observation: the execution host has Docker Engine 29.1.3 and Compose 2.40.3 but lacks Go, npm, SQLite, and Make on the host.
  Evidence: `go`, `npm`, `sqlite3`, and `make` returned command-not-found while both Docker version commands succeeded. Builds and tests therefore need containerized entrypoints.
- Observation: the repository arrived owned by root while the execution identity is `mordred`, despite the workspace being declared writable.
  Evidence: directory mode was `root:root 755` and a write check failed. Ownership was corrected only for `/srv/coder` so implementation could proceed.
- Observation: this host exposes a rootful Docker socket that `mordred` cannot access directly, even though passwordless `sudo` can run local build/test containers.
  Evidence: the first `docker run` failed with `permission denied ... /var/run/docker.sock`; the same pinned command via `sudo docker` succeeded. Shipped bootstrap still requires an explicitly configured rootless or otherwise hardened operator setup and never modifies the host daemon.
- Observation: Docker Engine 29.1.3 records but does not activate a host port binding when the container is attached only to an `internal: true` network.
  Evidence: `HostConfig.PortBindings` contained `127.0.0.1:8080`, while `NetworkSettings.Ports` was null and no listener existed. Adding a separate ingress network for only the trusted controller activated `127.0.0.1:8080`; the internal control network remains isolated and untrusted workers will never join ingress.
- Observation: Go tests execute temporary binaries and the pure-Go SQLite compiler needs more than 128 MiB of temporary space.
  Evidence: the first tool profile failed with `permission denied` from `/tmp/go-build...` under `noexec` and `no space left on device` while compiling `modernc.org/libc`. A build-tool-only 1 GiB executable tmpfs fixed the suite; runtime tmpfs remains smaller and `noexec`.
- Observation: TypeScript 7 rejects CSS side-effect imports unless Vite client types are explicit and rejects `allowImportingTsExtensions` in an emitting composite config.
  Evidence: the first frontend build reported TS2882 and TS5096. Adding `vite-env.d.ts`, removing the unnecessary option, and switching the type check to `tsc --noEmit` produced a successful Vite build.
- Observation: `openapi-typescript` 7.13.0 declares TypeScript `^5.x` as its peer range, so retaining the initial TypeScript 7 selection would require bypassing dependency validation.
  Evidence: `npm install` rejected TypeScript 7.0.2 with `ERESOLVE`; the official registry identified TypeScript 5.9.3 as the latest compatible stable release. Pinning 5.9.3 produced a zero-vulnerability lockfile, deterministic schema generation, a clean type check, and a successful production Vite build.
- Observation: standards-based `Request` construction in Node rejects relative URLs before a mocked fetch implementation sees the request.
  Evidence: the first generated-client frontend test failed with `Failed to parse URL from /api/v1/system/status`. Building a same-origin absolute API base from `window.location.origin` preserves browser routing and made the Node/jsdom accessibility test pass.

## Decision Log

- Decision: use Go 1.25 for the controller, runnerd, GitHub bridge, model supervisor, and CLI, with `modernc.org/sqlite` for a CGO-free SQLite build.
  Rationale: the contract prefers a small compiled control plane. Sharing domain packages avoids a second CLI workflow, and the pure-Go SQLite driver permits a non-root distroless runtime without a C runtime dependency.
  Date/Author: 2026-07-20 / Codex
- Decision: use a React/TypeScript/Vite static single-page application built into the controller image rather than a runtime Node service.
  Rationale: this provides a maintained accessible component ecosystem and a future maintained diff component while keeping one localhost web endpoint and no Node process in production.
  Date/Author: 2026-07-20 / Codex
- Decision: keep external systems behind domain interfaces and ship deterministic fakes selected by the development configuration.
  Rationale: the full lifecycle must run in CI without large weights, network credentials, or external repositories, while production adapters remain independently replaceable.
  Date/Author: 2026-07-20 / Codex
- Decision: keep `runnerd` out of the controller process in production and communicate through an authenticated local Unix socket; expose an in-process fake only for tests and explicit development mode.
  Rationale: mounting a worker Docker socket into the browser-facing controller would turn controller compromise into host control. A narrow server-owned job specification is the required containment boundary.
  Date/Author: 2026-07-20 / Codex
- Decision: use one canonical OpenAPI document and JSON Schema contracts, with generated clients and validators checked for reproducibility.
  Rationale: controller, CLI, dashboard, and worker contracts must not drift or reinterpret state and policy independently.
  Date/Author: 2026-07-20 / Codex
- Decision: authenticate the local runner boundary with both operating-system socket permissions and a separately mounted private bearer-token file; keep the mock runnerd completely offline and refuse any non-mock profile until the dedicated worker-daemon backend exists.
  Rationale: a Unix socket alone is vulnerable to accidental permission broadening, while an environment token risks exposure in process/container inspection. Layered file permissions and constant-time token comparison give the controller a narrow authenticated channel. Explicit refusal avoids silently running a fake executor when production containment is expected.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

Milestone 1 is closed. The repository produces a real non-root distroless controller image serving a React/TypeScript dashboard, persists jobs/transitions/config/audit data through container restarts and schema upgrades, exposes a bounded API/SSE stream and CLI, enforces state transitions and queue leases transactionally, and applies or rolls back audited configuration revisions without allowing the API to change bootstrap-controlled host paths or listen settings. The UI imports types generated from the canonical OpenAPI contract, and CI rejects generated-client drift. A live v1 database upgraded to v2; an acceptance transaction applied a safe workflow change, rejected an unsafe data-root change, created a provenance-bearing rollback revision, and restored the original document. No agent execution, verification, QC repair, Git publication, production auth, or durable memory behavior is claimed yet.

## Context and Orientation

The repository root is `/srv/coder`. `project.md` is the immutable product, architecture, security, test, and definition-of-done contract. `cmd/` contains small process entrypoints. Shared trusted application code lives under `internal/`; browser code lives under `web/`; untrusted worker harnesses and their schemas live under `agents/`; integration-specific configuration lives under `integrations/`; container build contexts live under `images/`; declarative schemas and safe examples live under `config/`; migrations live under `migrations/`; and acceptance fixtures live under `test/`.

A job is one maintenance request against one immutable base commit. The controller persists a job and every state transition in one SQLite transaction. A transition is a validated move from one named workflow state to another. A durable artifact is a content-addressed file plus database metadata that records the exact evidence used by later gates. Idempotency means that retrying an operation with the same key returns the previously recorded result instead of repeating an external side effect.

The controller owns four replaceable interfaces. `Runner` starts, inspects, streams, stops, and collects artifacts for an allow-listed job kind; callers never provide raw images, mounts, networks, or capabilities. `ModelManager` lists, loads, unloads, reports, and smoke-tests named model profiles. `GitProvider` registers and synchronizes repositories, creates per-job branches/worktrees, detects upstream changes, and publishes an approved branch as a draft change request. `MemoryStore` searches only within an already authorized project namespace and supports quarantine, promotion, correction, deletion, invalidation, export, and restore. Production services implement these interfaces over narrow authenticated local APIs; deterministic fakes implement them in process.

Configuration is trusted data stored outside worktrees. Each accepted change creates a revision with actor, timestamp, schema version, prior and new documents, a machine-readable difference, validation result, and rollback target. Browser forms map to schema-defined fields; neither UI nor API accepts arbitrary shell commands, Docker specifications, mount paths, image names, model paths, or llama.cpp arguments.

## Plan of Work

Milestone 1 creates a buildable repository and trusted workflow core. Add the Go module, containerized build scripts, CI, formatting and lint commands, schema examples, generated OpenAPI document, and a version-one SQLite migration. Define all required workflow states and an explicit transition graph in `internal/jobs`; keep external side effects out of that package. Implement a SQLite repository with WAL, foreign keys, a busy timeout, transactional state changes, append-only transitions, append-only audit events, configuration revisions, approval records, and idempotency records. Implement controller endpoints for health, readiness, service status, configuration, jobs, actions, artifacts, and bounded SSE events. Serve a keyboard-usable static dashboard shell and use fakes to prove restart/resume behavior at every durable phase.

Milestone 2 creates the local execution boundary and deterministic verifier. `runnerd` authenticates a Unix-socket caller, accepts only a job kind plus opaque job ID and predeclared input artifact IDs, resolves a fixed immutable image digest and mounts below configured roots, applies non-root user, read-only root, dropped capabilities, no-new-privileges, seccomp, PID/CPU/memory/disk/time/log limits, and either no network or the dependency-only egress profile. It rejects unknown images, mount roots, capabilities, networks, environment secrets, or command classes. The repository service creates a trusted bare mirror and never-reused worktree. The verifier maps repository policy command classes to fixed argument arrays and emits command-result JSON plus JUnit, SARIF, and coverage artifacts. Fixture tests prove forbidden specifications are rejected and that an offline repository can be checked out and verified.

Milestone 3 makes the fake lifecycle behaviorally complete and adds real-model plumbing without weights. Versioned model manifests contain an ID, role, filename basename, SHA-256, quantization, configured context, thread/NUMA/batch settings, sampling policy, source URI, license metadata, and minimum RAM/disk. Import and download tools refuse path traversal, verify hashes before atomic placement, and never run in CI by default. The supervisor owns one allow-listed child and serializes load/unload. Separate implementation and QC images consume bounded task packets and emit JSON validated against versioned schemas. The controller locks acceptance criteria before patching, runs targeted and full gates, loads a different reviewer family, validates evidence-bearing findings, enforces stable finding lifecycles and review-cycle limits, returns blocking findings for repair, reruns exact-commit verification, and requires operator action after the configured limit. Fake model scripts deterministically produce an initial flawed patch, one supported QC finding, a repair, and a clean second report.

Milestone 4 completes safe Git publication. The bridge stores GitHub App secrets only as runtime file secrets, signs short-lived installation-token requests, redacts credentials, and owns remote operations. The local provider uses a bare remote and fake draft-request record to run without network. Before publication, policy inspects the exact commit for protected paths, workflows, CODEOWNERS, submodules, hooks, binaries, symlinks, size, secrets, current verification, and upstream movement. Publication requires reviewer-role reauthentication, an unexpired approval bound to the exact job commit, and an idempotency key. A retry cannot duplicate a push or draft PR. Webhooks require HMAC validation and update memory candidates only after merge/rejection state is authenticated.

Milestone 5 adds memory and Hermes without expanding authority. A memory record includes repository identity, source/base/merged commit, source URI, status, affected paths, timestamps, invalidation rule, content hash, secret-scan result, and provenance. Retrieval filters by project namespace before ranking and records a bounded trace and token allocation. Automatic candidates remain provisional until deterministic verification, authenticated human promotion, or an authenticated merged-PR event. Cross-project and quarantine tests prove no leakage or automatic promotion. Hermes receives only submit/list/status/cancel/report/review/publication-request/project-memory-read tools. Scheduled jobs use the same controller authorization and queue. Proposed skills are inert versioned records until administrator review.

Milestone 6 completes the daily product. Implement first-run, overview, projects, jobs, QC/QA, models, memory, GitHub, scheduling/Hermes, and administration pages with safe and schema-validated expert modes. Add one-time bootstrap, secure cookie sessions, CSRF tokens, expiry, rate limiting, RBAC, and recent reauthentication for waivers, publication, secrets, policy, protected paths, restore, and upgrades. Add backup manifests, checksums, version/schema dry-run compatibility, actual restore tests, retention, health/metrics, update preflight and rollback. Harden Compose so only `127.0.0.1:8080` is published, all services run non-root with dropped capabilities and read-only roots where possible, secrets are files, internal networks are isolated, and no UI/controller/worker/Hermes/verifier service mounts a Docker socket. Complete the required operational, security, threat-model, licensing, and update documentation and run accessibility and end-to-end acceptance tests.

## Concrete Steps

All commands run from `/srv/coder`. On a rootless Docker installation, omit `sudo`; this execution host requires it for its rootful daemon. The current developer entrypoints are:

    sudo docker compose --profile tools build controller maintainctl
    sudo docker compose --profile tools run --rm go-tool sh -c 'gofmt -w cmd internal && go test ./... && go vet ./...'
    sudo docker compose config --quiet
    sudo docker compose up -d controller
    curl --fail http://127.0.0.1:8080/healthz
    sudo docker compose --profile tools run --rm maintainctl doctor
    ./scripts/acceptance.sh

During Milestone 1, use the pinned Go build image to format, resolve lockfiles, test, and build:

    docker run --rm -v "$PWD:/src" -w /src golang:1.25-bookworm gofmt -w cmd internal
    docker run --rm -v "$PWD:/src" -w /src golang:1.25-bookworm go mod tidy
    docker run --rm -v "$PWD:/src" -w /src golang:1.25-bookworm go test ./...

During frontend work, use the pinned Node LTS image and the committed npm lockfile:

    docker run --rm -v "$PWD:/src" -w /src/web node:24.18.0-bookworm npm ci
    docker run --rm -v "$PWD:/src" -w /src/web node:24.18.0-bookworm npm test
    docker run --rm -v "$PWD:/src" -w /src/web node:24.18.0-bookworm npm run build

Later milestone commands are added here when their images and profiles exist. Every command recorded as successful must include its short result in `Artifacts and Notes`; unavailable operator-only commands remain explicit and unclaimed.

## Validation and Acceptance

Foundation acceptance requires all Go and frontend unit tests, migrations from every retained schema, API contract tests, configuration validation tests, and restart/resume tests to pass. Starting the mock profile must bind only `127.0.0.1:8080`, return healthy component status, render the dashboard, persist a submitted job, stream bounded state changes over SSE after a controller restart, reclaim expired queue leases, and apply and roll back a safe versioned configuration change. Administrator bootstrap and authentication remain Milestone 6 work, matching the development sequence in `project.md`.

Secure-execution acceptance requires tests proving every forbidden mount/network/capability/image/command is rejected server-side, dependency acquisition has explicit egress while implementation/verifier/QC have none, caches are repository scoped, and each job gets a never-reused worktree. The verifier must emit machine-readable artifacts and detect protected paths, secrets, symlinks, binaries, submodules, oversized diffs, and workflow changes.

Workflow acceptance requires the seeded defect fixture to reproduce, patch, pass targeted/full verification, receive one evidence-supported blocking QC finding, repair it, rerun exact-commit checks, pass a fresh QC cycle, await operator approval, and publish once to a local bare remote. Restart tests interrupt and resume every durable state. Cancellation, timeout, malformed model output, review-cycle exhaustion, waiver reauthentication/audit, upstream movement, and duplicate publication all have explicit tests.

Product acceptance requires every dashboard page and CLI command named in `project.md` to call the same application/API contracts, keyboard and automated accessibility checks to pass, backup dry-run and actual restore to preserve durable configuration/job/audit/memory state, and Compose validation plus every image smoke build to succeed. Inspection of the resolved Compose model must prove that only the localhost dashboard is published, internal services have no host ports, all required security options are present, and forbidden Docker socket/secret/worktree mounts are absent.

The final audit maps every numbered requirement, named artifact, command, test class, invariant, and definition-of-done item in `project.md` to direct current evidence. Real GitHub publication, real multi-gigabyte model loading/benchmarking, production TLS/OIDC/passkeys, host rootless-daemon installation, encrypted host storage, and production backup-key handling remain operator-only unless separately authorized and available; their adapters, validation, fakes, and exact operator commands must still be complete.

## Idempotence and Recovery

Schema migrations run transactionally and record their version. Configuration revisions are append-only and roll back by creating a new validated revision. Worktrees, worker containers, and temporary downloads carry job IDs, live only below configured data roots, and may be safely reconciled after restart. Artifacts are written to a temporary file, hashed, atomically renamed, and never overwritten. External publication uses a database idempotency record created before the bridge call and reconciles uncertain results by querying the intended branch and draft request; it never blindly repeats a push or PR creation. Backups are additive archives with manifests and checksums. Restore defaults to dry-run and refuses incompatible schemas, missing files, failed hashes, or a running controller.

If a build or test fails, preserve its evidence, update `Surprises & Discoveries`, fix only the implicated layer, and rerun the narrow test before the larger suite. Do not delete operator data or reset the repository to recover. Mock-profile state lives below `.data/` and is disposable only when the operator explicitly chooses the documented clean command.

## Artifacts and Notes

Initial evidence:

    $ git ls-files
    project.md

    $ docker --version && docker compose version
    Docker version 29.1.3, build 29.1.3-0ubuntu4.1
    Docker Compose version 2.40.3+ds1-0ubuntu1

    $ go version; npm --version; sqlite3 --version; make --version
    command not found for each host tool

Foundation unit and integration evidence:

    $ sudo docker compose run --rm go-tool go test ./...
    ok  internal/api
    ok  internal/config
    ok  internal/jobs
    ok  internal/memory
    ok  internal/models
    ok  internal/queue
    ok  internal/runners
    ok  internal/storage/sqlite
    ok  internal/workflow

    $ npm test
    Test Files  1 passed (1)
    Tests       1 passed (1)

    $ npm run build
    vite v8.1.5 ... 1777 modules transformed
    internal/ui/dist/assets/index-XxnhIH3Y.js 203.62 kB
    built in 426ms

    $ npm run check:api && npm test && npx tsc --noEmit -p tsconfig.app.json
    generated OpenAPI types match web/src/api/schema.d.ts
    Test Files  1 passed (1)
    Tests       1 passed (1)
    TypeScript exited 0

    $ npx redocly lint ../internal/api/openapi.yaml
    Your API description is valid. One warning remains because the project license is intentionally not chosen by the implementer.

    $ ./scripts/acceptance.sh
    Foundation acceptance passed.

Container/runtime evidence:

    $ sudo docker compose build controller maintainctl
    controller Built
    maintainctl Built

    $ curl http://127.0.0.1:8080/healthz
    {"status":"ok"}

    $ sudo docker inspect local-code-maintainer-controller-1 --format ...
    user=1000:1000 readonly=true caps=["ALL"] security=["no-new-privileges:true"]
    ports={"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}]}

The runtime restart test submitted `job_6fc2fbe41a7e4af18a008cd860bdc6e3`, recreated the controller, and then `maintainctl inspect` returned the same queued job and its initial durable transition. The resolved container has no Docker socket mount; its only mount is `/srv/coder/.data` to `/var/lib/maintainer`.

The version-two migration acceptance reused that version-one database and brought the rebuilt controller back to Docker `healthy`. Queue tests proved exclusive acquisition, ownership checks, renewal, release, and expired-lease recovery. Live configuration acceptance created revision `config_e54b20a4665f4fe3aa426a25d04eef44`, rejected `deployment.data_root` through the API, then created rollback revision `config_9603d1f06b194bd1bad53209dda2faa1` bound to the original revision and restored the original workflow document. `maintainctl config validate -`, `maintainctl config export`, and `maintainctl doctor` all succeeded against the live controller.

Secure-runner boundary evidence:

    $ go test -race ./internal/runners ./internal/runnerd ./cmd/runnerd
    ok  internal/runners
    ok  internal/runnerd
    cmd/runnerd [no test files]

    $ docker inspect local-code-maintainer-runnerd-1 --format ...
    user=1000:1000 readonly=true network=none capdrop=["ALL"]
    security=["no-new-privileges:true"] ports={}

    $ stat .data/run/runnerd.sock .data/secrets/runnerd.token
    socket mode=600 owner=1000:1000
    token regular file mode=600 owner=1000:1000

The live Unix-socket probe returned `{"status":"ok"}`, rejected an unauthenticated start with HTTP 401, and accepted the same bounded request with the private token as run `run_2648f02b9373ebdc80204c071604e353`. Unit and race tests prove unknown caller fields such as image, command, mounts, network, capabilities, and environment are rejected before executor invocation; traversal and mutable image inputs are denied; only dependency preparation receives the named egress network; and implementation, verification, and QC remain offline. The mock service has no Docker socket and deliberately refuses a non-mock profile until the dedicated worker backend lands.

Official release checks on 2026-07-20 selected Node 24.18.0 LTS, Go 1.25, `modernc.org/sqlite` v1.54.0, and llama.cpp release `b9637` commit `aedb2a5` as initial pins. Image digests and every remaining application pin must be resolved and recorded before production Compose acceptance; no operational `latest` tag is permitted.

## Interfaces and Dependencies

`internal/jobs` defines `type State string`, every required state constant, `CanTransition(from, to State) bool`, and terminal/resumable predicates. `internal/storage` defines transactional repository methods for jobs, transitions, findings, approvals, idempotency, audit, configuration, artifacts, and leases. Storage methods accept `context.Context`, return typed domain errors, and never expose SQL rows outside the adapter.

`internal/runners.Runner` supports only `Start(ctx, JobRequest)`, `Inspect(ctx, RunID)`, `Logs(ctx, RunID, Cursor, Limit)`, `Stop(ctx, RunID)`, and `Artifacts(ctx, RunID)`. `JobRequest` contains a server-recognized job kind, job ID, project ID, and input artifact IDs; it contains no image, command, mount, network, capability, host path, or arbitrary environment field.

`internal/models.Manager` supports `Profiles`, `Load`, `Unload`, `Status`, and `SmokeTest` using allow-listed profile names and bounded options. `internal/repositories.Provider` supports repository registration/sync, exact-base worktree creation, upstream comparison, safe branch publication, and idempotent draft request creation. `internal/memory.Store` requires a `ProjectScope` on every query or mutation and exposes candidate/quarantine/canonical lifecycle operations. `internal/verification.Verifier` accepts policy-defined command-class identifiers and a trusted worktree handle, never browser-supplied shell text.

The first Go dependency is `modernc.org/sqlite` v1.54.0. Additional libraries are added only when they materially provide a maintained security or protocol implementation and are pinned in `go.mod`/`go.sum`. The frontend uses pinned React, TypeScript, Vite, an accessible component approach, a maintained diff viewer/editor, Testing Library, Vitest, and axe tooling with an npm lockfile. Runtime images use immutable digests after the initial build bootstrap. The llama.cpp build pins release `b9637` / commit `aedb2a5`, builds an AVX2/Haswell portable binary in a multi-stage image, and offers host-native only as an explicit optional build target.

Revision note (2026-07-20): updated the initial plan after the first foundation implementation. Recorded the durable controller/API/SQLite/React/fake-adapter outcomes, exact test and runtime evidence, rootful-daemon limitation, Docker internal-network port behavior, tool tmpfs correction, TypeScript adjustments, and the remaining work required before closing Milestone 1.

Revision note (2026-07-20 10:31Z): closed Milestone 1 after implementing configuration apply/rollback, SQLite migration v2 and durable queue leases, canonical OpenAPI type generation, the typed frontend client, and generated drift checks. Recorded the TypeScript peer compatibility decision and full source/image/live-runtime acceptance evidence, and corrected the plan's foundation acceptance boundary so administrator bootstrap remains in Milestone 6 as required by `project.md`.

Revision note (2026-07-20 10:53Z): began Milestone 2 with the runnerd containment boundary. Added its authenticated Unix API, server-owned immutable policy, bounded fake executor, controller client, private bootstrap token, hardened offline Compose service, race/containment tests, image build, and live authentication probes. Kept the milestone open for the real dedicated worker-daemon adapter, worktrees, verifier/artifacts, language images, and offline fixture acceptance.
