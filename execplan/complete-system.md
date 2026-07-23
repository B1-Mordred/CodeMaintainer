# Build the local multi-project code maintenance appliance

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept current while implementation proceeds. The product contract is the checked-in `project.md`; this plan restates the implementation path and evidence needed to make that contract operational.

## Purpose / Big Picture

The finished repository runs a self-hosted maintenance service that accepts an issue or free-form code task, checks out an isolated disposable worktree, obtains dependencies only in an explicitly networked phase, asks an implementation agent for a minimal patch, verifies the exact patch with deterministic commands, asks a separate read-only quality-control agent to review it, repairs supported blocking findings, and pauses for an authenticated operator before any publication. A browser dashboard at `http://127.0.0.1:8080` is the normal interface; `maintainctl` provides the same operations for bootstrap, recovery, and automation. The default evaluation profile uses fake inference, memory, execution, and GitHub adapters plus a local bare Git remote, so the complete lifecycle is testable without credentials or model weights.

The security boundary matters as much as the workflow. The controller is the sole workflow authority but cannot submit arbitrary container specifications. It calls a narrow `runnerd` service whose server-side policy fixes images, mount roots, networks, capabilities, resource limits, and timeouts. Implementation, verification, and QC workers are separate non-root containers with no Docker socket, GitHub credentials, host home directory, controller database, other worktrees, or general internet. Only the GitHub bridge may handle GitHub credentials, and it never shares them with a worker. The model supervisor accepts only versioned allow-listed model profiles and owns at most one `llama-server` child at a time. OpenViking and Hermes remain replaceable integrations behind narrow controller interfaces.

## Progress

- [x] (2026-07-20 09:30Z) Read the complete product contract, inspected the initially empty repository, and verified the available host toolchain and Docker support.
- [x] (2026-07-20 09:35Z) Selected a small Go control plane with SQLite, a statically built TypeScript dashboard, narrow HTTP/Unix-socket service contracts, and deterministic in-process fakes for default tests.
- [x] (2026-07-20 10:31Z) Milestone 1 foundation: repository conventions, pinned containerized Go/Node toolchains, system/QC schemas, versioned SQLite migration and WAL store, audited configuration apply/validate/rollback, durable queue leases, controller API and valid OpenAPI contract, generated typed frontend client with drift gate, replayable SSE, React/TypeScript UI shell, narrow fake adapters, and restart/reclaim tests for every resumable phase. The complete foundation unit, migration, API, frontend, schema, image-build, live-migration, CLI, and runtime acceptance run passed.
- [ ] Milestone 2 secure execution (completed through 2026-07-20 17:20Z: authenticated runnerd boundary and hardened server policy; SQLite artifact index and audited content-addressed store; exact-SHA offline worktrees with durable claim/retirement and never-reuse semantics; fixed Go/Python/Node/C/C++/Rust verifier registry; protected-path and secret scans; standard verification artifacts; a production Docker-API executor restricted to an operator-supplied dedicated Unix socket; deterministic runner identities and restart reattachment; digest-only images, fixed mounts and separate dependency-egress/inference-only/offline networks, non-root/read-only/capability/seccomp/CPU/RAM/PID/tmpfs/log/artifact/wall/disk-growth limits; immutable verifier, language, dependency, implementation, and QC workers; controller dispatch through the authenticated runner client; a real-daemon runnerd-backed offline Go fixture; and hardened agent-image acceptance. Remaining: operator validation against a dedicated rootless rather than this host's rootful development daemon and final named-egress topology acceptance).
- [x] (2026-07-20 17:20Z) Milestone 3 inference and agents: strict model manifests and verified imports; sequential authenticated model supervision; pinned llama.cpp image without weights; separate implementation and read-only QC roles, images, prompts, contexts, and schema contracts; atomic hash-bound edits; fresh-family QC; append-only finding histories; durable exact-commit verification and repair cycles; atomic phase/state/result persistence; restart reconciliation; and a deterministic full lifecycle that reaches authenticated approval.
- [x] (2026-07-20 17:52Z) Milestone 4 publication: authenticated narrow Git bridge; allow-listed local bare remotes; mirror sync and exact-base worktrees; deterministic commit recovery; upstream movement checks; exact-SHA append-only approvals; policy-gated idempotent intended-branch push and draft identity; controller restart recovery; live local single-publication acceptance; disabled-by-default GitHub App configuration with 2048-bit RS256 assertion signing, narrow installation-token permissions, expiry-aware caching, repository diagnostics, and issue/PR metadata marked untrusted; credential-free remotes and temporary askpass transport; fake GitHub API token-expiry and draft-idempotency tests; exact-byte webhook HMAC validation isolated in the bridge; append-only delivery replay protection; and five-minute authenticated API polling fallback. Real GitHub network publication remains an explicitly documented operator-only validation because no credentials or external repository are authorized.
- [x] (2026-07-20 18:05Z) Milestone 5 memory and scheduling: migration-backed project memory with strict registered-repository namespaces; provenance, secret scanning, quarantine, verified/human/merged promotion bases, optimistic corrections and lifecycle changes, content-clearing tombstones, append-only history and retrieval traces; 4K-token bounded project-before-ranking API retrieval; deterministic idempotent workflow extraction of verified and sanitized failed cases into quarantine; exact-repository/default-branch/publication-number/job-branch/result-SHA authenticated merge promotion and rejection invalidation from webhooks or polling; manifest-hashed schema-v1 project export and same-project dry-run/atomic restore that deduplicates and re-quarantines all imports behind recent reauthentication; generated OpenAPI client; OpenViking v0.3.21 fixed-route adapter, fake, durable leased indexing outbox, rebuild API, exact source/image pin, AGPL notice, hardened no-host-port optional profile, and a 15-second health/write/scoped-search/delete smoke operation; atomic recurring schedules with UTC maintenance windows and serial-queue dispatch; append-only automation history; configurable transactionally delivered local notifications for schedule, job-attention, terminal, review, and publication events with idempotent acknowledgement; durable per-job deadlines and append-only idempotent model-token reservations that fail closed on exhaustion; narrow opaque-token Hermes controller tools; non-authoritative review/publication requests; versioned, secret-scanned, database-enforced inert skill proposals with administrator review; and an official pinned Hermes v2026.7.7.2 profile whose isolated MCP bridge exposes exactly ten tools while the agent has no built-in tools, host ports, worktrees, controller token, GitHub credentials, or Docker socket. The real OpenViking smoke remains an operator-only run until an embedding provider and weights are configured; its fake exercises the same bounded contract.
- [x] (2026-07-20 20:10Z) Milestone 6 product completion: completed the authenticated safe/expert operator console and full named CLI surface; finding decisions and escalation; detailed jobs, artifacts, topology, model manifests, memory, GitHub, scheduling, notification, user, backup, restore, update, and rollback operations; offline staged restore application; enriched health and Prometheus metrics; encrypted bounded backups; known-good image promotion/rollback; hardened Prometheus and TLS/OIDC edge profiles; complete operator, security, threat-model, licensing, backup, update, and troubleshooting documentation; generated inventories; pinned desktop/mobile Chromium acceptance; and patched Go 1.25.12 after the final vulnerability scan found the original toolchain stale.
- [x] (2026-07-20 20:10Z) Completed the requirement-by-requirement audit in `docs/acceptance.md`. All deterministic local gates pass. The eight production checks that inherently require operator infrastructure, credentials, licensed weights, a second restore host, or an approved OCI scanner remain precisely listed and unclaimed rather than weakened.
- [x] (2026-07-20 22:42Z) Increment 2 Milestone 1: additive typed configuration registry, seven-scope resolver, scoped revisions, contextual conditional dependencies, compiled fail-closed prerequisites/dry runs, drafts/checks/review/apply/rollback, immutable accepted-job snapshots, ETag-bound REST/OpenAPI/CLI, redacted import/export, dedicated accessible workbench, and exhaustive 13-setting coverage gate through migration 17. Automated Go/vet/frontend/build/OpenAPI/Compose checks pass; the host-missing-`npx` real-browser overflow/focus check remains a precisely documented final acceptance item without a passing claim.
- [ ] (2026-07-20 23:10Z) Increment 2 Milestone 2 in progress: migration-backed project/blob/parser intelligence, bounded exact Git snapshots, partial-failure query/status evidence, production workflow-stage deterministic redacted context manifests, five-way verification differentials, explainable test impact, isolated cache provenance, and REST/OpenAPI/CLI/browser operations are implemented. Workflow baseline/impact production, richer replaceable parsers, verified cache-object wiring, lifecycle controls, and final-gate enforcement remain open.
- [ ] (2026-07-20 23:45Z) Increment 2 Milestone 2 production spine: workflow sync indexing, exact pre-change baselines, restart-idempotent targeted/full/final differentials, per-candidate impact evidence, mandatory fresh full-suite final enforcement, real parsed-blob cache hashes, quotas, retention/pruning, cancellation, configuration pause, and immutable snapshot-driven budgets are implemented through migration 19 and six additional typed settings. Rich parser/reference enrichment and remaining compare/correct/override/verify/warm/simulate operations keep the milestone open.
- [ ] (2026-07-21 00:01Z) Increment 2 Milestone 2 operator actions: project-scoped context comparison plus cache integrity verify, quota simulate, and trusted-snapshot warm are implemented with REST/OpenAPI/generated-client/CLI/browser parity and regression coverage. Rich parser/reference enrichment, broader context sources, baseline correction/update, and test-impact overrides/history keep the milestone open.
- [ ] (2026-07-21 00:16Z) Increment 2 Milestone 2 evidence review: migration 20 and append-only attributed/audited baseline supersession, differential correction, and targeted-test override histories are implemented across REST/OpenAPI/generated client/CLI/browser. Safety policy prevents classifying new failures away or overriding the fresh full-suite gate. Rich parser/reference enrichment and broader context sources keep the milestone open.
- [x] (2026-07-21 00:46Z) Increment 2 Milestone 2 closed: real pinned multi-language Tree-sitter runs in an isolated no-mount internal service while the controller remains static non-CGO; bounded SCIP/read-only-LSP facts are controller-validated; tool provenance is visible; and production context draws from immutable config, policy, instructions/docs, exact symbol ranges, verified memory, findings, baselines/differentials, and impact evidence. Normal/non-CGO tests, Compose, image builds, and live service probes pass.
- [x] (2026-07-21 01:31Z) Increment 2 Milestone 3 closed: canonical checksummed authority-neutral capability packs, reauthenticated/pinned/reversible lifecycle, independent exact project assignments, bounded trusted-snapshot Repo Doctor, disabled evidence-backed optimistic proposals, PHP/R/security manifests, rehearsal primitives, and complete browser/API/CLI/docs parity are implemented through migration 21. Full Go/vet, focused race, frontend accessibility/build, generated-client/OpenAPI, and Compose gates pass.
- [ ] (2026-07-22 08:40Z) Increment 2 Milestone 4 paused at a tested Windows-worker checkpoint: migration 23, ten fixed non-executable job types, optimistic/audited profiles, durable idempotent results, authenticated bounded protocol, deterministic simulator, .NET lab automation pack, and REST/OpenAPI/generated-client/CLI/accessible-browser parity are integrated. Full Go tests/vet, six frontend files with nine tests/build, API drift, zero-warning OpenAPI lint, and Compose validation pass. Remote-adapter selection, heterogeneous fixtures, remaining forge/restart failures, docs, and the full milestone gate remain open.
- [ ] (2026-07-22 09:07Z) Increment 2 Milestone 4 paused and synchronized at a tested remote-worker/forge checkpoint: fail-closed authenticated remote Windows routing now resolves only opaque credential references from a fixed secret directory; PHP/.NET/R/malformed heterogeneous fixtures exercise Repo Doctor; GitLab merge-request webhooks normalize through the credential-isolated bridge and forward-only migration 24; operator docs, generated bindings, and the coverage map are current. Full Go tests/vet, focused race, six frontend files with nine tests/build, Redocly/client drift, and Compose validation pass. Specialized capability-editor review, the broader migration/restart/failure matrix, and the full milestone gate remain open.
- [x] (2026-07-22 10:02Z) Increment 2 Milestone 4 closed: R/security pack configuration is now manifest-driven and controller-normalized across typed browser, REST/OpenAPI, CLI, optimistic audited storage, and restart replay; forge, Windows-worker, capability, migration, malformed-input, credential, idempotency, and namespace failures have current evidence. Full Go/vet, focused races, ten frontend accessibility tests/build, OpenAPI/client drift, Compose, control-plane images, authenticated health/doctor, and isolated desktop/mobile Chromium acceptance pass while the retained pre-rename appliance remains healthy.
- [ ] (2026-07-22 10:11Z) Increment 2 paused at the operator-requested post-Milestone-4 sync point. Commit `3e19b15` is on `origin/dev`, CI run `29910740641` passed, and Milestone 5 is the next unstarted scope.
- [ ] (2026-07-23 21:40Z) Increment 2 Milestone 5 task-contract/risk foundation checkpoint: migration 25, durable versioned task contracts, append-only contract events, deterministic risk assessments, expiring reauthenticated risk waivers, `awaiting_task_approval` workflow gating, REST/OpenAPI/generated-client/CLI/browser visibility, and operator documentation are implemented. Full Go tests and frontend API drift/tests/typecheck/OpenAPI lint pass. Milestone 5 remains open for schema-constrained agent contracts, Test Designer, golden/rehearsal, documentation policy/QC, and OPA.

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
- Observation: this Docker installation has no buildx plugin, and Docker 29 rejects `compress=true` for the `local` log driver even though older examples commonly include it.
  Evidence: `docker buildx bake` was unavailable; legacy multi-stage builds completed every runner target. A real create call rejected the original local-log options until runnerd retained `max-size` and `max-file=1` while setting `compress=false`.
- Observation: a containerized integration test that asks the host daemon to create bind-mounted workers must use paths visible identically to both the test container and host daemon and a numeric worker identity that owns those paths.
  Evidence: mounting `/srv/coder` at the identical path and using the test process UID/GID let the production executor run the offline fixture; profile user `65532` remains the image default and production policy must match the owner of the deployment data root.
- Observation: composing a useful full runner from Debian packages silently selected old distro Go and Rust releases.
  Evidence: the first full-runner smoke test reported the distro toolchains. Re-basing full on the pinned Rust 1.92 stage and copying the pinned Go 1.25 tree preserved current compiled toolchains while adding Python, Node, GCC, and CMake.
- Observation: the Debian snapshot image retained the default live Debian sources unless they were explicitly removed, so an apparently dated build could still fetch current packages.
  Evidence: the first inference build contacted the default Debian mirrors as well as the snapshot. Replacing every source entry before `apt-get update` made the build use only `snapshot.debian.org/archive/debian/20260505T000000Z`.
- Observation: the legacy Docker builder evaluates earlier stages even when a later target is requested, and llama.cpp's `LLAMA_BUILD_UI=OFF` does not disable its separately enabled prebuilt UI download.
  Evidence: putting optional native and full-runner stages before portable targets caused unnecessary work; moving independent targets immediately after their parent fixed it. The llama build still fetched the SHA-verified b9637 UI while the runtime is fixed to `--no-webui`.
- Observation: the host toolchain remains intentionally absent in fresh shells, so all final Go and frontend checks must continue through pinned tool containers.
  Evidence: `go`, `gofmt`, and `npm` were not on `PATH`; the pinned Compose tool services completed format, race, vet, frontend test/build, OpenAPI lint, and generated-client parity checks.
- Observation: a random runner ID made retrying the same durable phase capable of launching a second worker after a controller timeout.
  Evidence: the runner request already contains a bounded stable identity, so deriving the run ID from its canonical content made retries converge; Docker create conflicts can now only reattach to a container whose trusted labels match that identity.
- Observation: the workflow-state enumeration and the explicitly required default workflow order disagree about whether dependency preparation precedes acceptance-criteria generation.
  Evidence: the contract's default sequence says sync, worktree, dependencies, then criteria. The coordinator follows that explicit default while keeping every completion durable and independently resumable.
- Observation: the distroless browser-facing controller cannot and should not contain Git or repository credentials.
  Evidence: repository synchronization, commits, and publication moved into an authenticated internal Git bridge with narrowly validated project/job/SHA operations; only that process receives mirror/worktree/remote mounts.
- Observation: changing `maintainctl doctor` from a public status probe to an authenticated diagnostic made the controller's original self-healthcheck fail closed before bootstrap.
  Evidence: the rebuilt controller correctly returned 401 for `/api/v1/system/status`, so Compose marked it unhealthy and would not start the CLI bootstrap container. A separate minimal `maintainctl health` command now probes only public `/healthz`; `doctor` remains authenticated.
- Observation: OpenViking's current official deployment is AGPL-3.0-only and its upstream Compose example uses a mutable `latest` image plus published host ports.
  Evidence: the v0.3.21 `LICENSE` begins with the GNU Affero General Public License v3; the official tag resolves to commit `cb5ea7206c6c00d4f64f29878725e484aed7715f`, and its Linux/amd64 GHCR manifest is `sha256:1f734afb59463893b765e72f1e19445efd8cfb64d19c2088d7115b917407661a`. The appliance profile pins that manifest, publishes no port, records source correspondence, and runs the unmodified service as a replaceable boundary.
- Observation: the official Hermes image's normal s6 entrypoint starts as root so it can repair volume ownership before dropping to its service account, which conflicts with the appliance's container-level non-root invariant.
  Evidence: upstream Docker documentation describes `/init` as the root first process. The appliance instead invokes the official `hermes` executable directly as the deployment UID, runs `gateway run --no-supervise` in the foreground behind Docker's unprivileged init, and pre-creates the private writable data directory during bootstrap. The live resolved container reported `user=1000:1000`, a read-only root, all capabilities dropped, no published ports, and only `/opt/data` mounted.
- Observation: the long-running development database had applied an early migration-10 draft before its queue ordering column was finalized, while fresh-database tests only exercised the final migration text.
  Evidence: the rebuilt controller logged `SQL logic error: no such column: sequence (1)` although fresh race tests passed. Inspecting the live SQLite schema showed migration 10 recorded without `memory_index_operations.sequence`; forward-only migration 14 now rebuilds and preserves that queue for both historical and fresh databases.
- Observation: a fresh `govulncheck` database identified 22 reachable vulnerabilities in the originally pinned Go 1.25.0 standard library, and the scanner container initially could not create checksum-database state because its `GOPATH` still pointed outside the writable cache.
  Evidence: assigning `GOPATH=/cache/gopath` made the scanner reproducible; updating every Go build/tool image and the module directive to Go 1.25.12 at immutable digest `sha256:ea341baa...` reduced the rerun to zero reachable vulnerabilities. npm also reported zero vulnerabilities; Docker Scout is absent and the production OCI scan remains operator-only.

- Observation: the Windows worker capability pack needs domain types but must not import the simulator implementation.
  Evidence: the simulator now lives in `internal/windowsworker/simulator`; `internal/capabilities` imports only the pure closed contract and controller composition explicitly chooses the safe CI adapter.
- Observation: repeatable live acceptance cannot assume the canonical loopback port and CLI session are unused or fresh on a development host that retains the pre-rename appliance.
  Evidence: the first run found `local-code-maintainer-controller-1` healthy on `127.0.0.1:8080`; an alternate-port run then correctly rejected an expired retained CLI session. A disposable data/session root and Compose `!override` on port 18080 produced fresh bootstrap, authenticated doctor, and desktop/mobile Chromium evidence without interrupting or mutating the retained stack.

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
- Decision: represent worktree reuse prevention with a durable job claim plus a retirement marker written before removal, and permit idempotent recreation only while the claim is not retired and no prior checkout remains.
  Rationale: path existence cannot distinguish a new job from a previously used and cleaned workspace. Persistent claims bind project, job, and exact base SHA; retirement makes reuse impossible across restarts, while a pre-checkout crash can still be recovered safely for the same request.
  Date/Author: 2026-07-20 / Codex
- Decision: keep verifier commands in a compiled language/class registry and make policy scanning intrinsic to every verification run rather than accepting project- or browser-supplied argument arrays.
  Rationale: this preserves the no-arbitrary-command boundary. The same registry can run through the explicit local fixture executor or inside a runnerd-managed worker, while deterministic policy findings remain controller-verifiable and independent of model output.
  Date/Author: 2026-07-20 / Codex
- Decision: translate runner policy directly into Docker Engine API v1.44 requests over only a configured Unix socket, with no general Docker client library or arbitrary container-specification route.
  Rationale: the small adapter makes every accepted image, bind root, target, user, network, capability, security option, resource bound, and operation directly auditable. Production Compose mounts only the dedicated worker socket into runnerd; the controller and every worker remain socket-free.
  Date/Author: 2026-07-20 / Codex
- Decision: enforce writable bind growth with a runnerd-owned baseline watchdog in addition to bounded tmpfs, logs, and artifact validation.
  Rationale: read-only container roots and tmpfs sizes do not limit job worktree and artifact bind mounts. Runnerd records a trusted start baseline, stops growth beyond the kind-specific ceiling, and reattaches the watchdog from validated labels after inspection following a restart. Production operators should still place the data root on a quota-controlled filesystem for a hard storage backstop.
  Date/Author: 2026-07-20 / Codex
- Decision: assign implementation and QC workers only the dedicated inference network, keep verifier workers at `network=none`, and reserve the distinct dependency-egress network for dependency preparation.
  Rationale: local model access is a required capability but does not justify general egress. Separate server-selected names prevent a worker request from broadening its network authority and make deployment topology auditable.
  Date/Author: 2026-07-20 / Codex
- Decision: accept model weights only through bounded manifest-verified import/download operations and make the supervisor resolve allow-listed profile IDs rather than paths or llama.cpp arguments.
  Rationale: model paths, redirect targets, process arguments, and mutable files are all authority-bearing input. Exact byte length, SHA-256, read-only installation, role/family constraints, fixed runtime arguments, and one-child ownership make model selection deterministic and inspectable.
  Date/Author: 2026-07-20 / Codex
- Decision: require implementation responses to contain only complete, expected-hash-bound file replacements and run every QC cycle in a fresh read-only process with a different model family.
  Rationale: whole-file atomic replacement is easier to constrain than model-supplied patch commands, while the prior-content hash prevents stale writes. Separate contracts and images prevent the implementation agent from grading itself or editing during review.
  Date/Author: 2026-07-20 / Codex
- Decision: persist QC as append-only observations plus a versioned finding-state machine keyed by stable IDs.
  Rationale: a current-row overwrite would erase review history. Stable category/location identity, explicit repair-regression reasons, optimistic transition versions, and recent reauthentication for severe waivers preserve evidence across repair cycles and restarts.
  Date/Author: 2026-07-20 / Codex
- Decision: derive runner identities deterministically from canonical bounded requests and treat a matching pre-existing worker as the same attempt.
  Rationale: durable phase retries must reconcile uncertain starts instead of duplicating execution. A deterministic identity plus trusted container-label comparison makes the start boundary idempotent across controller and runnerd restarts.
  Date/Author: 2026-07-20 / Codex
- Decision: complete each workflow phase by atomically writing its immutable phase record, job metadata, state transition, and audit event.
  Rationale: independently updating result SHAs, criteria hashes, phase evidence, and workflow state creates crash windows in which the controller cannot tell whether an external effect was accepted. One transaction makes restart behavior unambiguous.
  Date/Author: 2026-07-20 / Codex
- Decision: bind publication approvals to an exact result SHA and require resolution of every blocking finding before the state may enter publishing.
  Rationale: approval of a job name or mutable branch could be reused after repair or upstream movement. Exact content binding and transactional finding checks ensure that only the reviewed commit can be published.
  Date/Author: 2026-07-20 / Codex
- Decision: keep the credential-free local provider in the Git bridge and accept only an allow-listed remote basename below the configured remote root.
  Rationale: the mock lifecycle needs real Git semantics without network credentials, but callers must not turn repository registration into arbitrary filesystem or URL access. The same narrow bridge interface can later host a disabled-by-default GitHub App adapter.
  Date/Author: 2026-07-20 / Codex
- Decision: use opaque random cookie sessions backed by SHA-256 token hashes, independently rotated hash-only CSRF tokens, and Argon2id local password hashes rather than browser bearer tokens or self-contained JWTs.
  Rationale: HttpOnly cookies keep session authority out of JavaScript and server-side records permit immediate revocation, expiry, exact roles, and recent-reauthentication checks. CSRF tokens, SameSite cookies, Fetch Metadata/origin checks, and disabled CORS protect ambient-cookie mutations without storing long-lived credentials in browser storage.
  Date/Author: 2026-07-20 / Codex
- Decision: keep operator and reviewer capabilities separate instead of treating roles as a simple privilege ladder; only administrators include every permission.
  Rationale: code execution and publication approval are distinct duties. A reviewer must not silently gain task-execution authority, and an operator must not gain approval authority merely because both can read job evidence.
  Date/Author: 2026-07-20 / Codex
- Decision: keep SQLite as the authoritative memory provenance and lifecycle ledger while treating OpenViking as a replaceable project-scoped index.
  Rationale: semantic indexing must not become workflow authority or erase correction, promotion, invalidation, deletion, and retrieval evidence. Deriving the namespace from the registered project before search and retaining an append-only local trajectory preserves isolation even when the external ranker is unavailable or rebuilt.
  Date/Author: 2026-07-20 / Codex
- Decision: reconcile OpenViking through a durable leased outbox instead of coupling SQLite commits to synchronous index calls.
  Rationale: a canonical promotion or deletion must not become ambiguous when the external index is down or the controller restarts after one side effect. Version-bound upsert/forget operations retry safely, skip superseded versions, and allow an operator rebuild without making OpenViking the source of truth.
  Date/Author: 2026-07-20 / Codex
- Decision: let Hermes request controller actions through a separate service identity, but represent review and publication requests as append-only notifications rather than workflow approvals.
  Rationale: conversational automation may coordinate work without acquiring reviewer authority. A constant-time file-token boundary, registered-project derivation, the existing serial queue, and a database constraint that keeps generated skills inert preserve the controller as the sole authority even if Hermes is compromised.
  Date/Author: 2026-07-20 / Codex
- Decision: terminate Hermes MCP calls in a small isolated bridge that owns the controller service token instead of mounting that token into Hermes or letting Hermes call the general controller API.
  Rationale: the official agent can discover standard MCP tools without gaining a reusable controller credential. An internal network, fixed JSON-RPC methods, exact tool schemas, server-side path construction, bounded responses, disabled resources/prompts, and a duplicate Hermes-side allowlist constrain both discovery and execution. OpenViking remains controller-mediated because direct provider access would bypass registered-project filtering and retrieval traces.
  Date/Author: 2026-07-20 / Codex
- Decision: reserve the configured worst-case completion tokens transactionally before every implementation, repair, or QC model phase and bind the wall deadline to the job at creation.
  Rationale: schedule limits must survive restart and cannot depend on an agent reporting its own usage. Unique phase-version reservations make retries idempotent, conservative preallocation fails closed after an uncertain model call, and the queue propagates the durable deadline into every worker context while the engine records timeout failure using a short cancellation-independent transaction.
  Date/Author: 2026-07-20 / Codex
- Decision: treat project memory export as a portable import format, not as a way to transfer canonical authority.
  Rationale: the schema-versioned bundle is bound to one project and a deterministic manifest hash, but a hash is not an external signature. Dry-run validates every record and total size without writes; actual restore requires an administrator's recent reauthentication, runs atomically, deduplicates content, records provenance, and imports every record as unverified quarantine regardless of its prior status.
  Date/Author: 2026-07-20 / Codex
- Decision: keep GitHub App signing, installation tokens, webhook secrets, authenticated Git transport, and GitHub API access entirely inside `git-bridge`, and let the controller receive only normalized events and publication metadata.
  Rationale: workers and the browser-facing controller do not need reusable GitHub credentials. Exact-byte HMAC validation, token permissions, repository diagnostics, askpass-based Git authentication, exact publication binding, append-only delivery IDs, and API polling provide recoverable synchronization without placing a token in process arguments, remotes, logs, artifacts, or controller storage.
  Date/Author: 2026-07-20 / Codex
- Decision: deliver automation notifications first to a durable controller-local inbox, generated in the same SQLite transaction as the schedule run, human-attention transition, or authority-free request.
  Rationale: local delivery works offline and cannot introduce an SSRF or secret-bearing outbound webhook channel. Configuration can disable generation, every delivery/read event is durable, and later optional transports can consume this authoritative inbox without changing workflow authority.
  Date/Author: 2026-07-20 / Codex
- Decision: advance the entire Go build and developer toolchain from 1.25.0 to the patched 1.25.12 release and pin the resolved Bookworm image digest everywhere.
  Rationale: the final reachability scan found standard-library security fixes that cannot be safely backported in application code. One version and digest across control-plane, inference-supervisor, runner, and developer builds prevents a stale stage from silently reintroducing the vulnerable library.
  Date/Author: 2026-07-20 / Codex
- Decision: constrain Windows work to ten controller-owned operation classes with immutable source/pack/toolchain identities and structured checks/artifacts.
  Rationale: simulator and future remote adapters can prove .NET, PowerShell, service, installer, HAMILTON, VPN, release, IQ, equipment, and signing workflows without exposing arbitrary PowerShell, commands, images, mounts, paths, networks, environment, or arguments.
  Date/Author: 2026-07-22 / Codex
- Decision: route remote Windows profiles only through the bounded authenticated worker protocol and resolve credentials from an operator-mounted fixed directory by opaque reference, with no simulator fallback.
  Rationale: controller requests cannot choose paths or transmit raw stored credentials, and real-worker failures remain visible rather than being replaced by simulated success.
  Date/Author: 2026-07-22 / Codex
- Decision: treat trusted capability-manifest UI fields as the authoritative closed schema for both initial proposal acceptance and later project-assignment configuration.
  Rationale: generating safe controls is insufficient if API/CLI paths can store extra fields. Controller normalization, default expansion, cross-field validation, optimistic persistence, and digest-only audit make every operator surface converge without adding runner or scanner authority.
  Date/Author: 2026-07-22 / Codex

## Outcomes & Retrospective

All implementation milestones are closed except for the explicitly operator-owned production topology validation retained under Milestone 2. The repository now produces a non-root distroless authenticated control plane, a responsive generated-client React console, the full recovery/automation CLI, policy-bound runner and model supervisors, safe Git publication, project-scoped durable memory, bounded scheduling/Hermes integration, encrypted restore-capable backups, and preflighted update/rollback operations. State, evidence, findings, approvals, configuration, notifications, memory, audit, and external-effect reconciliation survive migrations and restarts.

Increment 1 remains the preserved base for a new active Increment 2 effort. The additive implementation plan is `execplan/increment-2.md`; it keeps this completed plan intact while extending configuration, intelligence, heterogeneous repository support, quality policy, provider routing, evidence, evaluation, observability, and the browser information architecture.

Increment 2 Milestone 3 adds a controller-trusted capability catalog and no-write onboarding path without changing the Increment 1 execution authority. Pack lifecycle and project assignment history are durable and audited; Repo Doctor treats repository content only as bounded hash-cited evidence and cannot silently apply its own proposals.

Increment 2 Milestone 4 is closed. Its credential-isolated normalized forge domain, forward-only migrations, deterministic Windows simulator and fail-closed remote router, heterogeneous fixtures, GitHub/GitLab webhook normalization, typed R/security assignments, API/CLI/browser workbenches, restart/failure evidence, and operator documents are durable and schema-bound. The complete local gate includes isolated authenticated desktop/mobile Chromium acceptance without disturbing the retained appliance.

The deterministic local lifecycle and separate implementation/QC container boundary pass end to end. The final gate also passes full Go unit/integration/race/vet, frontend build/API drift/WCAG automation, all Compose views, authenticated live diagnostics, desktop/mobile pinned-Chromium interaction, hardened agent-image execution, Go/npm vulnerability checks, and source/package/image inventory generation. Real GitHub installation behavior, licensed GGUF inference, OpenViking embeddings, official Hermes runtime, remote OIDC/TLS, separate-host encrypted restore/update rollback, dedicated rootless worker topology, and OCI image scanning are intentionally not simulated as production proof; `docs/acceptance.md` gives their exact validation boundary.

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

    $ sudo docker inspect codemaintainer-controller-1 --format ...
    user=1000:1000 readonly=true caps=["ALL"] security=["no-new-privileges:true"]
    ports={"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}]}

The runtime restart test submitted `job_6fc2fbe41a7e4af18a008cd860bdc6e3`, recreated the controller, and then `maintainctl inspect` returned the same queued job and its initial durable transition. The resolved container has no Docker socket mount; its only mount is `/srv/coder/.data` to `/var/lib/maintainer`.

The version-two migration acceptance reused that version-one database and brought the rebuilt controller back to Docker `healthy`. Queue tests proved exclusive acquisition, ownership checks, renewal, release, and expired-lease recovery. Live configuration acceptance created revision `config_e54b20a4665f4fe3aa426a25d04eef44`, rejected `deployment.data_root` through the API, then created rollback revision `config_9603d1f06b194bd1bad53209dda2faa1` bound to the original revision and restored the original workflow document. `maintainctl config validate -`, `maintainctl config export`, and `maintainctl doctor` all succeeded against the live controller.

Secure-runner boundary evidence:

    $ go test -race ./internal/runners ./internal/runnerd ./cmd/runnerd
    ok  internal/runners
    ok  internal/runnerd
    cmd/runnerd [no test files]

    $ docker inspect codemaintainer-runnerd-1 --format ...
    user=1000:1000 readonly=true network=none capdrop=["ALL"]
    security=["no-new-privileges:true"] ports={}

    $ stat .data/run/runnerd.sock .data/secrets/runnerd.token
    socket mode=600 owner=1000:1000
    token regular file mode=600 owner=1000:1000

The live Unix-socket probe returned `{"status":"ok"}`, rejected an unauthenticated start with HTTP 401, and accepted the same bounded request with the private token as run `run_2648f02b9373ebdc80204c071604e353`. Unit and race tests prove unknown caller fields such as image, command, mounts, network, capabilities, and environment are rejected before executor invocation; traversal and mutable image inputs are denied; only dependency preparation receives the named egress network; and implementation, verification, and QC remain offline. The explicit mock profile has no Docker socket; the production profile refuses startup unless it can load a strict immutable policy and resolve the configured data root and dedicated worker-socket path.

Production-executor and immutable-runner evidence:

    $ go test -race ./internal/runnerd ./cmd/runnerd ./cmd/maintainer-worker ./internal/verification
    ok  internal/runnerd
    ok  cmd/maintainer-worker
    ok  internal/verification

    $ RUNNERD_DOCKER_INTEGRATION=1 ... go test -v -run TestDockerExecutorRunsOfflineGoFixture ./internal/runnerd
    --- PASS: TestDockerExecutorRunsOfflineGoFixture (21.31s)

The real-daemon fixture used the production executor, an immutable local SHA-256 image ID, network `none`, fixed read-only root/capability/security/resource policy, an exact job worktree, and a read-only bounded verification packet. It compiled and tested an offline Go module and returned a hash-verified immutable command-result artifact. This proves the adapter against the available rootful development daemon only; production Compose requires a separately provisioned dedicated worker socket and never mounts the host's main socket.

Legacy multi-stage builds produced all required base, Python, Node, C, C++, Rust, Go, and full worker tags from digest-pinned bases and a fixed Debian snapshot. Inspection reports user `65532:65532` and entrypoint `["/usr/local/bin/maintainer-worker","verification"]`. A read-only, network-none, cap-drop-all smoke of the full image reported Python 3.11.2, pytest 7.2.1, flake8 5.0.4, Node 18.20.4, npm 9.2.0, GCC/G++ 12.2.0, Rust/Clippy 1.92.0, and Go 1.25.0; the language-specific tags independently reported their pinned or snapshot-selected tools. The disk-growth race test creates more than a one-byte allowance and observes runnerd stop the worker within the watchdog interval.

Artifact evidence:

    $ go test -race ./internal/artifacts ./internal/storage/sqlite
    ok  internal/artifacts
    ok  internal/storage/sqlite

Migration v3 adds deduplicated SHA-256 objects and immutable job-scoped artifact associations. Artifact tests prove bounded ingestion, no-overwrite atomic publication, mode-0400 objects, two associations sharing one content object, cross-job lookup denial, append-only metadata, audit creation, and size/hash verification that rejects on-disk tampering. The existing live database upgraded from v2 to v3 and the rebuilt controller returned `{"status":"ok"}`. Authorized runner inputs are staged under a per-job directory, so runnerd never turns an untrusted artifact ID directly into a global object-store mount.

Offline worktree and verifier evidence:

    $ go test -race ./internal/repositories ./internal/verification ./internal/artifacts ./internal/api
    ok  internal/repositories
    ok  internal/verification
    ok  internal/artifacts
    ok  internal/api

`TestWorktreeManagerCreatesExactOfflineNeverReusedWorktrees` creates a local bare mirror, checks out an exact SHA without fetching, resumes the same claim idempotently, retires it before cleanup, rejects reuse, and gives a second job a distinct path. Companion tests reject traversal, symbolic-link mirror escapes, non-SHA revisions, and wrong commits. `TestOfflineFixtureChecksOutFixVerifiesAndEmitsStandardArtifacts` checks out a seeded Go arithmetic defect, commits the repair, runs fixed compile and full-test classes offline, binds evidence to the exact base/head and patch hash, and produces two command results plus JUnit, SARIF, coverage status, and a verification report. Scanner fixtures prove protected workflow/submodule configuration, secrets, symlinks, binaries, and oversized patches block. API tests prove artifact metadata is job scoped, content is integrity checked before download, the SHA-256 response header matches, and a different job receives 404. Generated frontend types and the OpenAPI drift gate include these artifact endpoints.

Official release checks on 2026-07-20 selected Node 24.18.0 LTS, Go 1.25, `modernc.org/sqlite` v1.54.0, and llama.cpp release `b9637` commit `aedb2a5` as initial pins. Image digests and every remaining application pin must be resolved and recorded before production Compose acceptance; no operational `latest` tag is permitted.

Supervised-inference and isolated-agent evidence:

    $ docker build -f images/inference/Dockerfile --target inference-haswell ...
    llama-server version 9637 (aedb2a5e), GNU 12.2.0, x86_64
    image sha256:3b467cb6b2b089... user=65532:65532 size=43,999,012 bytes

    $ docker build -f images/runners/Dockerfile --target implementation-agent ...
    image sha256:8b65271... user=65532:65532
    $ docker build -f images/runners/Dockerfile --target qc-agent ...
    image sha256:5328e415... user=65532:65532

    $ ./scripts/agent-image-acceptance.sh
    Agent image acceptance passed.

The acceptance fixture started a hardened fake model on an internal-only inference network, ran the implementation image with only its worktree writable, verified a hash-bound arithmetic repair and immutable result artifact, ran a fresh QC image with the worktree read-only to produce an evidence-supported blocking finding for cycle zero, then ran another fresh QC process for cycle one and obtained pass. No agent received a Docker socket or controller database. Manifest and supervisor tests reject mutable weights, incorrect byte/hash metadata, credential-bearing or redirected sources, non-allow-listed profiles, arbitrary paths/arguments, multiple child processes, unhealthy loads, and malformed smoke output. Finding-store tests preserve every observation and permit only the documented open/fixed/verified/closed, dispute, rejection, acceptance, and recently reauthenticated human-waiver paths.

    $ sudo docker compose --profile tools run --rm --no-deps go-tool go test -race ./...
    all packages passed
    $ sudo docker compose --profile tools run --rm --no-deps go-tool go vet ./...
    exited 0
    $ sudo docker compose --profile tools run --rm --no-deps web-tool sh -c 'npm ci && npm test && npm run build && ...'
    1 frontend test passed; Vite built 1777 modules; OpenAPI valid with the existing unchosen-license warning; generated client matched
    $ sudo docker compose -f compose.yaml -f compose.dev.yaml config --quiet
    exited 0
    $ sudo env MAINTAINER_DATA_ROOT=... RUNNERD_POLICY_FILE=... RUNNERD_WORKER_SOCKET=... MODEL_MANIFEST_ROOT=... docker compose -f compose.yaml -f compose.production.yaml config --quiet
    exited 0

Durable workflow and local-publication evidence:

    $ go test -race ./internal/workflow ./internal/gitbridge ./internal/projects ./internal/storage/sqlite ./internal/api
    all packages passed

`TestCoordinatorCompletesImplementRejectRepairApproveAndLocalPublish` uses a real local bare remote and exercises synchronization, exact-base worktree creation, dependency preparation, locked acceptance criteria, expected-failure reproduction, implementation, deterministic commit, targeted and full gates, a separate-family blocking QC result, repair, a second exact commit, final verification, fresh QC pass, exact-SHA approval, and one idempotent local publication. The stored finding reaches `closed` through four append-only versions, and every workflow completion stores its result metadata, transition, phase record, and audit event atomically.

The live mock stack repeated the lifecycle through the real controller and Git bridge. Job `job_942d3de1c48a4c3aa0ab651f1e41ed54` began at `c9e823f34999dfed6589163cae3327395813eb49`, produced initial result `acb9fee8ad063ea1da70b64f242bc578323402ce`, repaired result `9174b6a74c3fd1725f854178b9ab35f93fb33bec`, and waited for an operator with review cycle `1`. The controller was rebuilt and restarted before approval. Approval `approval_856eb93b07d9416abee38357593a821f` was bound to the repaired SHA with reviewer role and reauthentication recorded, after which the job completed and the bare remote contained exactly branch `maintainer/job_942d3de1c48a4c3aa0ab651f1e41ed54` at the repaired SHA. This is credential-free local publication only; no real GitHub action was attempted.

Authentication-boundary evidence:

    $ go test -race ./internal/auth ./internal/storage/sqlite ./internal/api ./cmd/maintainctl
    all packages passed

Unit and API integration tests prove a single atomic bootstrap, case-normalized users, Argon2id verification, random session and CSRF rotation, revocation/expiry, five-failure rate limiting, forged actor/administrator-header rejection, missing-token and cross-site mutation rejection, and exact role separation. Browser tests render both the bootstrap form and authenticated overview with no automated WCAG A/AA violations. The generated OpenAPI contract declares cookie authentication and never exposes a session token in JSON.

The existing live database upgraded to migration v8 and reported `bootstrapped=false`. `maintainctl bootstrap` consumed a generated mock password only from stdin, stored a mode-0600 session record under `.data/cli`, and reported the resulting administrator identity without printing either token or password. An unauthenticated system-status request then returned 401, the same authenticated CLI returned the healthy bounded component report, public `/healthz` kept the container healthy, and `/api/v1/auth/status` reported `bootstrapped=true`. The disposable first validation account was removed only after a failed CLI mount check, with an append-only `auth.bootstrap_reset` audit event; the final mock administrator is the successfully persisted account.

Memory, scheduling, and Hermes evidence:

    $ go test -race ./internal/memory ./internal/storage/sqlite ./internal/api ./internal/hermesbridge
    all packages passed
    $ docker compose --profile hermes run --rm --no-deps hermes mcp test maintainer
    Connected; Tools discovered: 10
    $ docker exec -it codemaintainer-hermes-1 hermes tools --summary
    CLI (1/25): maintainer

Migration v9 and v10 tests prove strict project namespaces, cross-project denial, quarantined-record exclusion, secret rejection, optimistic lifecycle changes, append-only provenance and retrieval traces, content-clearing tombstones, bounded 4K-token retrieval, durable leased index retries, and superseded-version skipping. The optional OpenViking profile pins v0.3.21 without a host port and the controller remains authoritative.

Migration v11 tests prove atomic no-duplicate schedule dispatch, UTC maintenance-window skips, append-only schedule history, version conflicts, inert skill proposals, database rejection of activation, proposal secret rejection, and automation requests that create no real approval. API integration proves the Hermes token is server-derived, missing credentials fail, project registration is enforced, publication requests confer no authority, and proposed skills remain inactive.

Migration v12 binds every new job to a wall-time deadline and token ceiling. Tests prove schedule budgets propagate to the queued job, a 16K model-call reservation is durable and idempotent by phase version, a later over-budget reservation fails closed, reservation history cannot be changed or deleted, and an underfunded implementation phase transitions durably to `failed` without invoking the model.

Project-memory bundle tests prove deterministic manifest validation, tamper rejection, exact same-project enforcement, bounded dry runs with zero writes, content-hash deduplication, atomic restore after a tombstone, new record IDs, unverified quarantine status, append-only restore provenance, administrator-only export/restore routes, CSRF enforcement, and a recent-reauthentication gate for actual restore. OpenAPI and the generated frontend client contain the export and restore contracts.

The live local MCP bridge completed the 2025-06-18 initialize and tools/list handshake, advertised exactly ten fixed tools, and forwarded a read-only list-jobs request to the authenticated controller. The pinned official Hermes v2026.7.7.2 image then connected to the bridge and discovered the same ten tools. Its runtime tool summary showed only `maintainer`; inspection reported `user=1000:1000`, read-only root, all capabilities dropped, no host ports, and only its private `/opt/data` bind mount. The bridge has no host port and only the mode-0600 controller token mount; Hermes has neither that token nor a Docker socket, worktree, database, or GitHub credential.

## Interfaces and Dependencies

`internal/jobs` defines `type State string`, every required state constant, `CanTransition(from, to State) bool`, and terminal/resumable predicates. `internal/storage` defines transactional repository methods for jobs, transitions, findings, approvals, idempotency, audit, configuration, artifacts, and leases. Storage methods accept `context.Context`, return typed domain errors, and never expose SQL rows outside the adapter.

`internal/runners.Runner` supports only `Start(ctx, JobRequest)`, `Inspect(ctx, RunID)`, `Logs(ctx, RunID, Cursor, Limit)`, `Stop(ctx, RunID)`, and `Artifacts(ctx, RunID)`. `JobRequest` contains a server-recognized job kind, job ID, project ID, and input artifact IDs; it contains no image, command, mount, network, capability, host path, or arbitrary environment field.

`internal/models.Manager` supports `Profiles`, `Load`, `Unload`, `Status`, and `SmokeTest` using allow-listed profile names and bounded options. `internal/repositories.Provider` supports repository registration/sync, exact-base worktree creation, upstream comparison, safe branch publication, and idempotent draft request creation. `internal/memory.Store` requires a `ProjectScope` on every query or mutation and exposes candidate/quarantine/canonical lifecycle operations. `internal/verification.Verifier` accepts policy-defined command-class identifiers and a trusted worktree handle, never browser-supplied shell text.

The first Go dependency is `modernc.org/sqlite` v1.54.0. Additional libraries are added only when they materially provide a maintained security or protocol implementation and are pinned in `go.mod`/`go.sum`. The frontend uses pinned React, TypeScript, Vite, an accessible component approach, a maintained diff viewer/editor, Testing Library, Vitest, and axe tooling with an npm lockfile. Runtime images use immutable digests after the initial build bootstrap. The llama.cpp build pins release `b9637` / commit `aedb2a5`, builds an AVX2/Haswell portable binary in a multi-stage image, and offers host-native only as an explicit optional build target.

Revision note (2026-07-20): updated the initial plan after the first foundation implementation. Recorded the durable controller/API/SQLite/React/fake-adapter outcomes, exact test and runtime evidence, rootful-daemon limitation, Docker internal-network port behavior, tool tmpfs correction, TypeScript adjustments, and the remaining work required before closing Milestone 1.

Revision note (2026-07-20 10:31Z): closed Milestone 1 after implementing configuration apply/rollback, SQLite migration v2 and durable queue leases, canonical OpenAPI type generation, the typed frontend client, and generated drift checks. Recorded the TypeScript peer compatibility decision and full source/image/live-runtime acceptance evidence, and corrected the plan's foundation acceptance boundary so administrator bootstrap remains in Milestone 6 as required by `project.md`.

Revision note (2026-07-20 10:53Z): began Milestone 2 with the runnerd containment boundary. Added its authenticated Unix API, server-owned immutable policy, bounded fake executor, controller client, private bootstrap token, hardened offline Compose service, race/containment tests, image build, and live authentication probes. Kept the milestone open for the real dedicated worker-daemon adapter, worktrees, verifier/artifacts, language images, and offline fixture acceptance.

Revision note (2026-07-20 11:00Z): added migration v3 and the artifact evidence core. Objects are content-addressed, bounded, atomically published without overwrite, read-only, integrity-checked on access, and associated immutably with jobs in audited SQLite metadata. Kept artifact API/staging and verifier-produced JUnit/SARIF/coverage/command results in the remaining Milestone 2 scope.

Revision note (2026-07-20 11:15Z): completed the trusted worktree, deterministic verifier, standard artifact-format, artifact API, and authorized staging portions of Milestone 2. Added an offline Go defect checkout/fix/verification integration test and policy fixtures for the required high-risk patch classes. Kept Milestone 2 open because actual immutable worker images and a production executor attached only to a dedicated rootless worker daemon are not yet implemented or accepted.

Revision note (2026-07-20 13:05Z): implemented the production Unix-socket Docker API executor, strict bootstrap policy file, immutable verification worker, all required language runner targets, production Compose separation, real-daemon offline fixture, and writable disk-growth watchdog. Recorded the exact rootful-development evidence and retained the dedicated-rootless-daemon check as an operator production validation. Milestone 2 remains open until dependency acquisition, controller dispatch, and the Milestone 3 implementation/QC images complete every job kind.

Revision note (2026-07-20 17:20Z): closed Milestone 3 and recorded the completed local-provider portion of Milestone 4. Added deterministic runner reconciliation, idempotent artifacts, atomic durable workflow phases, registered projects, the authenticated Git bridge, dependency worker, production container backend, full coordinator and repair loop, exact-SHA approvals, local publication, and both in-process and live restart-spanning lifecycle evidence. Real GitHub App, browser authentication, memory/Hermes, and product-operations work remain explicitly open.

Revision note (2026-07-20 15:35Z): completed the core Milestone 6 authentication boundary before expanding operator UI actions. Replaced caller-supplied identity headers in the running controller with one-time bootstrap, server-side sessions, CSRF, exact RBAC, recent reauthentication, rate limits, private CLI session persistence, audited lifecycle operations, and accessible bootstrap/login screens. Kept user administration and every remaining product-operations page open.

Revision note (2026-07-20 21:10Z): added durable schedules, automation history, non-authoritative review/publication requests, inert reviewed skill proposals, narrow Hermes controller routes, a standard isolated MCP bridge, and the pinned official Hermes profile. Recorded unit, live network, upstream-client discovery, enabled-tool summary, and runtime-containment evidence. Kept memory extraction/export/restore, notifications, and enforceable per-job schedule budgets open.

Revision note (2026-07-20 21:35Z): made schedule token/time budgets enforceable on the actual job lifecycle. Added migration-backed deadlines, model-token reservations, replay-safe accounting, worker-context cancellation, cancellation-independent timeout failure persistence, API visibility, and over-budget workflow tests. Memory extraction/export/restore and notification delivery remain open in Milestone 5.

Revision note (2026-07-20 22:00Z): added bounded project memory export, manifest validation, dry-run restore, atomic deduplicated import into quarantine, restore provenance/audit, administrator route separation, and recent reauthentication. Kept workflow extraction, authenticated merge-event promotion, OpenViking live configuration, and notification delivery open.

Revision note (2026-07-20 20:10Z): closed Milestone 6 and the contract audit after completing the operator console, restore/update/rollback paths, metrics and optional profiles, operational/security/licensing documentation, responsive real-browser acceptance, and SBOM/vulnerability tooling. The final deterministic gate reported: all Go unit/integration and race tests passed; Go vet passed; generated OpenAPI matched; React built and both WCAG A/AA tests passed; every Compose view validated; the authenticated mock appliance and desktop/mobile Chromium flow passed; hardened agent images passed; `govulncheck` on patched Go 1.25.12 found zero reachable vulnerabilities; and npm found zero vulnerabilities. Docker Scout was unavailable, so the approved OCI scan remains the eighth documented operator-only validation.

Revision note (2026-07-20 20:46Z): began Increment 2 without reopening or weakening the completed Increment 1 contract. Added `execplan/increment-2.md` and `config/increment-2-coverage.json`, recorded the clean pre-change baseline and additive configuration-registry direction, and retained all Increment 1 operator-only production validations as explicit boundaries.

Revision note (2026-07-20 22:00Z): advanced Increment 2 Milestone 1 through deterministic redacted export and reviewed import, immutable forward-key preservation, migration 17, REST/OpenAPI/CLI parity, and current supply-chain remediation. Migration 16 remains unchanged for retained databases; the complete browser workflow, setting inventory enforcement, prerequisite checks, and job-acceptance snapshot binding remain the configuration milestone's open work.

Revision note (2026-07-20 22:25Z): added the dedicated typed Configuration workbench, exhaustive setting-level machine-readable coverage, and atomic accepted-job configuration snapshots for direct, Hermes, and scheduled paths. Job inspection now shows the immutable redacted snapshot identity and provenance document. Dependency evaluation, concrete external prerequisite handlers, and the real-browser pass remain open before the first Increment 2 milestone closes.

Revision note (2026-07-20 22:42Z): closed Increment 2 Milestone 1 with conditional dependency enforcement at every lifecycle boundary, contextual import validation, compiled fail-closed prerequisite/dry-run checks, safe-default and inherited browser resets, true before/after impact previews, and corrected system inheritance for non-system views. The complete automated gate is green; only the explicitly unrun host Playwright overflow/focus check carries into final acceptance.

Revision note (2026-07-20 23:10Z): started Increment 2 Milestone 2 with migration-backed project-separated syntax evidence, bounded exact Git snapshots, deterministic redacted context manifests, baseline/differential and test-impact records, isolated cache provenance, and matching API/CLI/browser operations. Kept workflow production, richer parser adapters, and cache-object enforcement open and visible.

Revision note (2026-07-20 23:45Z): connected Increment 2 intelligence evidence to the real durable workflow and accepted-job configuration snapshot. Migration 19 and the coordinator now produce replay-safe baselines, differentials, impacts, and parsed-blob cache evidence through the final verification gate; typed pause, retention, quota, context-budget, and clean-cache controls are operational. Rich adapters and the remaining comparison/correction/override/cache actions remain open.

Revision note (2026-07-21 00:01Z): completed the context comparison and cache verify/warm/simulate operator actions across typed service, storage, REST/OpenAPI/generated client, CLI, and browser surfaces. Cache verification recomputes controller-owned parsed-object identities and warming reuses the trusted exact-snapshot refresh path. Rich adapters, broader context sources, baseline correction/update, and impact overrides/history remain open.

Revision note (2026-07-21 00:16Z): added forward-only migration 20 and append-only evidence-review ledgers for baseline supersession, differential correction, and targeted-test overrides, including attribution, reasons, reauthentication, audit, and all operator surfaces. Safety invariants keep new failures and full-suite gates non-waivable. Rich adapters and broader context sources remain before Increment 2 Milestone 2 closes.

Revision note (2026-07-21 00:46Z): closed Increment 2 Milestone 2 by isolating pinned real Tree-sitter grammars behind a bounded internal service, preserving static non-CGO controller binaries, validating SCIP/read-only-LSP fact adapters, exposing tool versions, and expanding production Context Compiler inputs to every available controller-authorized evidence family. Both images and live internal probes pass.

Revision note (2026-07-21 01:31Z): closed Increment 2 Milestone 3 with checksummed authority-neutral capability packs, append-only and reauthenticated lifecycle changes, exact project assignments, bounded Git-snapshot Repo Doctor evidence and explicit optimistic reviews, PHP/R/security profiles, rehearsal primitives, and full operator-surface parity. Milestone 4 forge normalization and the constrained simulated Windows worker are now the active boundary.

Revision note (2026-07-21 02:38Z): paused Increment 2 Milestone 4 at a tested forge-foundation checkpoint for the operator-requested CodeMaintainer rename and initial GitHub synchronization. Migration 22, normalized credential-isolated forge profiles/objects/sync history, GitLab bridge support, exact endpoint and reauthentication controls, REST/OpenAPI/generated client, and CLI operations are retained as partial work; hosted inventory, browser parity, Windows simulation, fixtures, and milestone closure remain open.

Revision note (2026-07-21 02:49Z): renamed the product and module to CodeMaintainer and `github.com/B1-Mordred/CodeMaintainer` before the first push to the empty canonical repository. Branding, imports, Compose/package/service identities, schemas, examples, and generated metadata now match; compatibility-sensitive `MAINTAINER_*` variables, executables, database contracts, goal filenames, and backup encryption context remain unchanged.

Revision note (2026-07-22 08:13Z): resumed Increment 2 Milestone 4 and added bounded paginated hosted GitHub/GitLab inventory, safe transient retry and rate-limit continuation, registered-project identity binding, explicit provider capability gaps, and a provider-neutral browser workbench with write-only credential-reference handling. GitLab webhook normalization, restart/API failures, Windows simulation, heterogeneous fixtures, documentation, and milestone acceptance remain open.

Revision note (2026-07-22 08:40Z): paused Increment 2 at a tested Milestone 4 Windows-worker checkpoint. Migration 23, ten fixed non-executable operation classes, optimistic profiles, durable idempotent structured evidence, authenticated bounded protocol, deterministic simulator, .NET lab automation pack, and API/CLI/browser parity are integrated. Full Go tests/vet, frontend accessibility tests/build, generated API drift, zero-warning OpenAPI lint, and Compose validation pass; the remaining remote adapter, heterogeneous fixtures, forge/restart failures, docs, and milestone gate stay open.

Revision note (2026-07-22 09:07Z): paused and synchronized Increment 2 at a tested Milestone 4 remote-worker/forge checkpoint. Added fixed-directory opaque-secret remote routing with no simulator fallback, heterogeneous PHP/.NET/R/malformed fixtures, GitLab merge-request webhook normalization through migration 24, operator docs, client parity, and updated coverage evidence. Full Go tests/vet, focused race, frontend accessibility tests/build, Redocly/client drift, and Compose validation pass; specialized capability-editor review, the broader migration/restart/failure matrix, and milestone closure stay open.

Revision note (2026-07-22 10:02Z): closed Increment 2 Milestone 4 with controller-authoritative typed R/security configuration, optimistic audited assignment updates, complete forge/Windows/capability restart and failure evidence, specialized pack documentation, and updated machine-readable coverage. Full Go/vet/focused-race, frontend accessibility/build, OpenAPI/client, Compose/image, authenticated health/doctor, and isolated desktop/mobile Chromium gates pass; the temporary acceptance stack/data were removed and the retained pre-rename appliance was rechecked healthy.

Revision note (2026-07-22 10:11Z): recorded the operator-requested Increment 2 pause immediately after the Milestone 4 closure was pushed and CI run `29910740641` passed. Milestone 5 remains unstarted and is the next resumption boundary.

Revision note (2026-07-23 21:40Z): resumed Increment 2 Milestone 5 and added the task-contract/risk foundation with migration 25, an implementation-blocking approval gate, exact-version contract approval, deterministic risk routing, reauthenticated expiring waivers, API/OpenAPI/generated-client/CLI/browser support, and quality workflow documentation. `go test ./...` and frontend check:api/tests/typecheck/Redocly passed; the remaining Milestone 5 independent agent, golden, documentation, and OPA stages stay open.

Revision note (2026-07-23 21:45Z): paused at the operator-requested Increment 2 Milestone 5 foundation checkpoint for git synchronization. The checkpoint remains partial and resumable: task contracts and risk routing are present, while independent quality stages and OPA-backed policy activation are still open.
