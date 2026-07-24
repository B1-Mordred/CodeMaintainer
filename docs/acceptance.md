# Acceptance evidence and operator-only validation

`project.md` is the authoritative contract. This page records the release gate and separates deterministic local evidence from checks that require operator infrastructure, credentials, or multi-gigabyte weights.

The runnable Compose and acceptance runbook is maintained in `docs/compose-acceptance.md`. Its checked coverage matrix is `config/compose-acceptance-coverage.json`.

## Deterministic local gate

Run from a clean checkout after `./maintainctl bootstrap`:

```sh
./scripts/acceptance.sh
./scripts/soak.sh
./scripts/agent-image-acceptance.sh
./scripts/vulnerability-scan.sh
./scripts/sbom.sh
```

The first command formats/checks all Go, runs every unit/integration/migration/workflow test, runs Go vet, verifies generated OpenAPI parity, builds and tests the React application with automated accessibility checks, validates Compose, builds the control plane, starts the mock stack, checks authenticated diagnostics, and drives the desktop/mobile application in pinned Chromium. The soak command retains `.data/acceptance/soak-report.json` from the bounded deterministic `local-bounded-soak-v1` report runner. The agent-image gate proves separate immutable implementation and read-only QC containers against the fake OpenAI-compatible model. The security inventory commands cover Go/npm and record the OCI follow-up when no image scanner is installed.

The Go suite covers the complete local bare-remote lifecycle, exact-SHA worktrees and publication, independent model families, malformed output, deterministic verification, stable QC finding repair/waiver rules, publication gates, restart reconciliation, queue and wall/token budgets, GitHub App signing/token/replay/polling fakes, protected paths and secrets, cross-project memory isolation, OpenViking adapter/outbox behavior, Hermes' ten-tool authority boundary, schedules/notifications, authentication/RBAC/CSRF/rate limits, backup dry-run and actual restore, and migrations. Migration coverage also checks `config/migration-coverage.json` against every embedded SQLite migration and migration file so schema additions cannot ship without restart/resume, backup/recovery, durable-record, documentation, and rollback evidence. Redaction coverage checks `config/redaction-coverage.json` across configuration, provider egress, observability/support bundles, memory, authentication, forge, Windows-worker, and policy-decision outputs. Security/adversarial coverage checks `config/security-adversarial-coverage.json` across authorization, reauthentication, stale edits, injection, worker isolation, SSRF/egress/fallback/cost, webhook authentication, cross-project isolation, restore path safety, and no-write preview boundaries. Unit/property/fuzz coverage checks `config/unit-property-fuzz-coverage.json` and executes seed fuzz corpora for declarative imports, model manifests, webhook payloads, restore archive paths, task packets with untrusted Markdown, fake-model task-packet extraction, and streamed provider event parsing during normal Go tests. Fixture/failure coverage checks `config/integration-failure-coverage.json` against the heterogeneous fixture repositories and integration proof for baseline/candidate classification, partial indexing, stale configuration, restart/resume, pack and policy lifecycle, cache/memory staleness, provider routing denial, timeout and rate-limit retry circuits, streamed interruption parsing, batch partial failures, retained-probe model drift, lower-trust fallback denial, egress redaction, and telemetry redaction. Local-fake E2E coverage checks `config/local-fake-e2e-coverage.json` against the 14-step Increment 2 workflow, tying the deterministic coordinator workflow, controller-level browser-originated onboarding/config/provider/restore harness, browser surfaces, provider default-off/egress proofs, per-worker provider route snapshots for implementation/repair/Test Designer/Documentation/QC, configuration rollback, evidence graph, and backup/restart/restore checks to explicit remaining browser/provider workflow gaps. Performance/soak coverage checks `config/performance-soak-coverage.json` against scheduler resource decisions, runtime benchmark gates, historical evaluation metrics, observability timing/resource records, deterministic acceptance smoke evidence, and the retained bounded soak report for queue latency, scheduler stability, memory growth, provider circuit denials, support-bundle size, and local-fake throughput. No default gate fetches a model weight, uses a real GitHub repository, or requires external inference.

Validated Compose views are:

```sh
docker compose config --quiet
docker compose --profile tools config --quiet
docker compose -f compose.yaml -f compose.observability.yaml --profile observability config --quiet
MAINTAINER_REMOTE_HOST=maintainer.example.invalid \
OIDC_GATEWAY_URL=https://auth.example.invalid/verify \
MAINTAINER_TLS_CERT=/absolute/path/to/cert.pem \
MAINTAINER_TLS_KEY=/absolute/path/to/key.pem \
  docker compose -f compose.yaml -f compose.remote.yaml --profile remote config --quiet
MAINTAINER_DATA_ROOT=/absolute/path/to/data \
MODEL_MANIFEST_ROOT=/absolute/path/to/model-manifests \
RUNNERD_POLICY_FILE=/absolute/path/to/runner-policy.json \
RUNNERD_WORKER_SOCKET=/absolute/path/to/dedicated-worker.sock \
  docker compose -f compose.yaml -f compose.production.yaml config --quiet
```

The resolved core publishes only `127.0.0.1:8080`; credential/model/index/Hermes/runner/database boundaries publish no host port. The optional remote edge defaults to loopback TLS and delegates to an operator-supplied OIDC/passkey gateway while retaining local appliance RBAC. Runtime services are non-root, capability-free, `no-new-privileges`, read-only where possible, resource bounded, and do not mount the general Docker socket. Production runnerd accepts only a separately configured dedicated rootless worker-daemon socket.

## Operator-only validation

These are deliberately not claimed by the fake gate:

`config/external-validation-coverage.json` is the machine-readable map from each operator-only prerequisite to the safe fake evidence and finite operator procedure that must exist before release.

1. Configure a dedicated rootless worker daemon and the named dependency-egress/inference-only networks, apply the production runner policy, then repeat the offline fixture and inspect every resulting worker's UID, mounts, capabilities, seccomp, resource limits, and network membership.
2. Import licensed implementation and QC GGUF files whose manifests use different model families. Verify hashes, benchmark physical-core versus SMT and NUMA profiles, exercise sequential load/unload, and record RAM, disk, prompt, and decode measurements. No release process downloads these large weights automatically.
3. Install a GitHub App in a disposable authorized repository with the documented minimal permissions. Test token expiry, webhook replay, polling fallback, upstream movement, branch protection/CI/review reporting, idempotent draft publication, merge, rejection, and memory disposition. Never point the test at an unapproved real repository.
4. Configure an embedding provider for OpenViking and run the controller's health/write/scoped-search/delete smoke plus a rebuild while inspecting exact project namespaces and source/license obligations.
5. Configure the official pinned Hermes profile and verify that discovery exposes exactly ten controller tools, proposed skills stay inert, and Hermes receives no controller token, GitHub credential, worktree, host port, or Docker socket.
6. Configure real certificates and the trusted OIDC/passkey forward-auth gateway. Test remote login, local session/RBAC/CSRF behavior, WebSocket proxying if enabled later, certificate renewal, firewall policy, and loss of the gateway.
7. Store the production backup key separately, create an encrypted backup, restore it on a separate host, verify users/projects/audit/memory/artifacts/mirror state and a smoke job, then exercise update promotion and known-good image rollback. Repeat after every schema or key rotation.
8. Run an approved OCI vulnerability/signature scanner over every resolved production image if Docker Scout is unavailable, review high/critical exceptions, licenses, SBOM/provenance, and host kernel/container-runtime advisories before promotion.

Record dates, exact versions/digests, operators, commands, results, and retained evidence for every production check. A missing external prerequisite is not a reason to weaken or bypass the corresponding controller gate.
