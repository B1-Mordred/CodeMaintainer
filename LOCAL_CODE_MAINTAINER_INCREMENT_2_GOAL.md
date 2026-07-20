## 1. Starting point and mission

This is the next increment of the local, quality-first, multi-project code
maintenance appliance defined in `LOCAL_CODE_MAINTAINER_GOAL.md`. Inspect the
actual repository and its tests before changing it. Preserve working Increment
1 behavior, data, permission boundaries, and APIs unless this specification
explicitly introduces a compatible migration.

Increment 2 must make the appliance substantially more accurate, efficient,
auditable, and useful across heterogeneous repositories. It adds:

- automatic repository onboarding;
- versioned capability packs for PHP/intranet, Windows/.NET/lab automation,
  R/statistics, and security/risk analysis;
- GitLab and local bare-Git support behind a forge abstraction;
- incremental semantic code intelligence and a bounded context compiler;
- baseline-aware and change-impact-aware verification;
- an independent test-design stage;
- dedicated documentation generation, review, and policy enforcement;
- evidence-linked quality and risk decisions;
- resource-aware scheduling and safe inference optimization;
- optional, policy-controlled remote and hosted model providers behind a
  provider-neutral gateway;
- historical patch evaluation;
- OpenTelemetry-based operational visibility;
- a single, versioned configuration system exposed completely through the web
  interface.

The target remains a CPU-only Linux host with two Xeon E5-2699-class processors,
128 GB RAM, local NVMe, Docker Compose, sequential large-model loading, and
long-running noninteractive work where code quality matters more than response
latency. The system must remain fully usable with local models only. Remote
models are an optional capacity/quality/fallback choice, never a requirement.

## 2. Non-negotiable outcome

An operator must be able to perform every routine Increment 2 setup,
configuration, inspection, preview, execution, approval, waiver, rollback, and
troubleshooting task from the included web interface.

Do not satisfy this requirement with a raw YAML/JSON editor. Provide typed,
accessible controls generated from or validated against authoritative schemas,
with contextual help, safe defaults, validation, effective-value previews,
revision history, diffs, audit events, and rollback.

A CLI and configuration files must remain available for bootstrap, disaster
recovery, automation, version control, and headless operation. The browser, CLI,
and automation API must call the same controller services and enforce identical
validation and authorization. No UI-only or CLI-only policy semantics are
allowed.

The only acceptable routine exceptions are host prerequisites that cannot be
safely changed by the application, such as installing Docker, enabling CPU
virtualization, creating a Windows VM, attaching a hardware simulator, or
placing model weights in an approved directory. For each exception, the web UI
must show status, exact operator instructions, a re-check action, and diagnostic
evidence. It must never silently attempt privileged host mutation.

## 3. Product principles

Apply these priorities in order:

1. Correctness, reproducibility, and evidence-backed code quality.
2. Containment of repositories, agents, plugins, credentials, and tools.
3. Minimal, relevant model context rather than maximal context.
4. Differential verification that distinguishes new failures from old ones.
5. Fast iteration through indexing, impact analysis, and safe caching.
6. Independent review of implementation, tests, documentation, and risk.
7. Explainable policy decisions and complete operator control.
8. Maintainability on a single CPU-only host.

LLM output remains advisory. Deterministic verification and policy decide
whether a job advances.

## 4. Scope boundaries

Do not:

- weaken any Increment 1 security boundary;
- give agents forge credentials, a Docker socket, host filesystem access, or
  unrestricted network access;
- expose arbitrary shell commands, container options, model paths, tool
  arguments, OPA input, or file paths through the UI;
- make OpenViking or model-generated memory authoritative over source, tests,
  policies, or Git history;
- execute generated capability packs, policies, documentation code blocks, or
  tools without allow-listing and sandboxing;
- store hidden model reasoning; store concise decisions, claims, citations, and
  artifact references instead;
- require Kubernetes, Kafka, Elasticsearch, Neo4j, Temporal, or another global
  vector database;
- keep multiple large models resident simultaneously on the target host;
- require a cloud account or remote model provider for core operation;
- send source, prompts, memories, artifacts, secrets, or metadata to a remote
  model unless the effective project policy explicitly permits that data class
  and the operator-visible egress preview matches the transmitted request;
- let an agent contact a local-network or Internet model endpoint directly;
- use model weights, real forge credentials, physical instruments, or a real
  Windows production machine in normal CI;
- sign releases or communicate with physical lab equipment automatically;
- treat a cache hit as final release evidence without a fresh policy-required
  verification pass;
- add a second configuration authority outside the controller.

## 5. Cross-cutting web configuration architecture

### 5.1 Configuration registry

Implement a controller-owned configuration registry. Every configurable
Increment 1 and Increment 2 feature must register a versioned descriptor with:

- stable namespace and schema version;
- JSON Schema for values and cross-field validation;
- UI metadata for labels, help, grouping, ordering, widgets, units, examples,
  warnings, and documentation links;
- permitted scopes;
- defaults and recommended values;
- secret/write-only classification;
- required role or permission;
- whether a change is live, applies to new jobs, requires service reload, or
  requires an explicit operator restart;
- dependency, incompatibility, and prerequisite declarations;
- dry-run/validation handler where the setting affects external systems;
- export/import and migration behavior;
- audit redaction rules.

Reject unregistered keys by default. Preserve unknown keys only during a
documented forward-compatible import mode; never apply them.

### 5.2 Scopes and precedence

Support these scopes where meaningful:

1. built-in safe default;
2. system;
3. capability pack;
4. project;
5. named environment or runner profile;
6. job template;
7. one-job operator override.

The registry descriptor must declare which scopes are legal. The controller
must calculate the effective value deterministically and return, for every
field, its value, source scope, source revision, and whether a higher-priority
override exists. Job snapshots must pin the complete effective configuration so
later edits do not alter a running or historical job.

### 5.3 Web configuration experience

Build a reusable dashboard configuration workbench with:

- schema-driven typed forms plus purpose-built editors for complex domains;
- search across settings, descriptions, and namespaces;
- basic and advanced views without hiding effective values;
- scope selector and inheritance indicators;
- effective-configuration preview;
- before/after diff and impact summary;
- inline and server-side validation;
- dependency and conflict warnings;
- prerequisite health checks;
- dry-run/test-connection/test-policy actions;
- save as draft, review, apply, and discard;
- immutable revision history, author, timestamp, reason, and audit link;
- one-click rollback by creating a new revision;
- export of a redacted declarative representation;
- controlled import with preview and validation;
- reset-to-inherited and reset-to-default actions;
- clear badges for changes that affect only new jobs or require reload/restart;
- accessible keyboard operation, focus management, errors, and status updates.

Secrets must use write-only fields. The API must never return raw values after
submission. The UI may show only “configured”, origin, last rotation time, and
the result of an explicit connection check. Export, audit, logs, traces, and
support bundles must redact secrets.

### 5.4 API, CLI, permissions, and concurrency

Expose the registry through versioned REST/OpenAPI endpoints for:

- descriptors and UI metadata;
- values at each scope;
- effective-value calculation;
- validation and dry runs;
- draft, review, apply, rollback, import, and export;
- dependency/prerequisite status;
- revision history and audit events.

Use optimistic concurrency with revision identifiers/ETags. A stale browser
must never overwrite a newer edit silently. Require explicit re-authentication
or equivalent confirmation for secret changes, signing configuration, forge
credential changes, hardware enablement, policy waivers, and destructive cache
or index operations.

The CLI must consume the same OpenAPI contract or shared application service.
Generate typed frontend and client bindings where practical. Maintain contract
tests that compare UI, CLI, and API outcomes.

### 5.5 Policy enforcement

The controller—not the frontend—must enforce configuration schemas,
permissions, policy, and state transitions. The browser must never be able to
select arbitrary images, executables, mounts, network destinations, model
arguments, or worker commands. The UI may select only trusted IDs registered by
the controller.

## 6. Repository onboarding and Repo Doctor

Add an onboarding service that analyzes a newly registered repository without
modifying it. It must identify, with confidence and evidence:

- languages, frameworks, package managers, lockfiles, build systems, and
  workspaces;
- test frameworks, linters, type checkers, formatters, documentation systems,
  migrations, generated code, and release tooling;
- Git submodules, Git LFS, large/binary files, line-ending rules, encodings,
  executable bits, and case-sensitivity risks;
- CI workflows and likely local equivalents;
- runtime services and data stores;
- likely protected paths and high-risk components;
- existing repository guidance such as `AGENTS.md`, contribution documents,
  coding conventions, and ownership files;
- candidate capability packs and verification commands.

Present findings as a proposed project configuration. Each proposal must cite
the file or observation that caused it, include confidence, and remain disabled
until the operator reviews and applies it. Support re-scan and display drift
from the accepted configuration.

The web UI must provide an onboarding wizard, scan progress, evidence viewer,
side-by-side proposal diff, command allow-list editor, pack selection, dry run,
and explicit acceptance. Malicious repository text must be displayed as
untrusted data and must not become controller instructions.

## 7. Versioned capability-pack framework

Implement signed or checksummed, versioned capability packs. A pack is
declarative and may contribute only allow-listed:

- detection rules;
- container/runner profiles identified by trusted IDs;
- commands and structured command templates;
- parsers for test, coverage, lint, build, and documentation output;
- policy fragments;
- context selectors;
- risk rules;
- documentation rules and renderers;
- UI schema extensions;
- golden/rehearsal test definitions.

Packs must not contain arbitrary controller code. If an extension requires
code, it must be an independently reviewed, pinned plugin with an explicit
permission manifest and separate lifecycle.

The UI must support catalog browsing, compatibility/prerequisite checks,
install/enable/disable/upgrade/rollback, version pinning, project assignment,
configuration, effective-content inspection, signature/checksum status, and a
preview of the workflow changes a pack introduces.

### 7.1 `php83-intranet` pack

Support PHP 8.3 intranet applications with configurable profiles for:

- Composer, lockfile verification, PSR-4/autoload validation, and dependency
  audit;
- PHPUnit, coverage through Xdebug or PCOV where available, and test-result
  parsing;
- PHPStan and/or Psalm with repository-pinned levels and baselines;
- Rector dry-run and controlled upgrade assistance;
- Infection mutation testing for selected/high-risk scopes;
- Twig template validation;
- Apache integration profiles;
- MySQL 8 disposable test services, schema/migration checks, and fixture policy;
- Redis/Predis disposable services;
- Vite/Tailwind builds;
- Playwright browser and visual regression tests;
- theme/plugin contract checks;
- CRLF, shebang, permissions, encoding, and case-sensitivity checks.

Do not require every tool for every repository. Repo Doctor proposes a profile;
the operator chooses requirements, risk thresholds, baselines, and time budgets
through the web interface.

### 7.2 `windows-dotnet-labautomation` pack

Support Windows/.NET and instrument-integration repositories through a
disposable Windows worker adapter. Include configurable support for:

- pinned .NET SDK/runtime and deterministic restore/build/test;
- Windows service installation, start/stop/recovery, and cleanup checks;
- PowerShell and Pester;
- Inno Setup build, install, upgrade, repair, uninstall, and residue checks;
- HAMILTON path/package/driver discovery expressed as explicit profiles;
- VPN-dependent workflow triggers without storing VPN secrets in agents;
- release/version/file-metadata consistency;
- installer artifact collection and IQ-style installation evidence;
- simulator-first equipment and protocol tests;
- isolated code-signing requests that agents cannot invoke directly.

Define a narrow authenticated worker protocol. The controller sends only
pre-approved job types and immutable inputs. The worker runs in disposable VM
snapshots or an equivalently resettable environment and returns structured
results and artifacts. Physical hardware access, signing, and production VPN
use require an operator gate and are disabled by default.

The web UI must manage worker profiles, health, capacity, VM template identity,
toolchain inventory, allowed job types, timeouts, simulator endpoints,
artifact-retention policy, signing-policy references, and manual gates. It must
offer connection tests and a simulated worker for normal CI.

### 7.3 `r-statistical-validation` pack

Support R packages and statistical applications with:

- `renv` lockfile restore/cache policy;
- `R CMD check`;
- `testthat` result parsing;
- lint and documentation checks;
- `roxygen2` and `pkgdown` generation where selected;
- golden datasets and reference-result versioning;
- absolute/relative numerical tolerances, missing-value policy, random-seed
  policy, locale/time-zone controls, and reproducibility metadata;
- differential comparison of tables, models, charts, and serialized results.

The UI must provide safe editors for tolerance profiles, golden dataset
references, approval of baseline updates, seed/environment controls, and
rendered result comparisons.

### 7.4 `sbom-fmea-security` pack

Add configurable security and risk evidence using pinned tools such as Syft,
Grype, Trivy, and CodeQL when supported and licensed for the target repository.
Allow projects to select tools rather than running all scanners blindly.

Generate and compare SBOMs; correlate new dependencies, vulnerabilities,
licenses, exposed interfaces, privileges, and data flows with FMEA-style
failure modes. Support operator-maintained mappings to CAPEC and ATT&CK as
references, not as automated proof of exploitability.

The UI must manage scanner profiles, databases and update status, severity and
confidence thresholds, suppressions with expiry/reason, risk matrices,
FMEA records, evidence links, SBOM diff, and release gates.

## 8. Forge abstraction and repository synchronization

Refactor Increment 1 GitHub integration behind a `ForgeProvider` interface
without weakening its credential isolation. Implement:

- `GitHubForge`, preserving existing behavior;
- `GitLabForge` for projects, issues, merge requests, discussions, pipelines,
  jobs, artifacts, webhooks, branches, tags, releases, and submodules where the
  configured GitLab edition/API supports them;
- `LocalBareGitForge` for offline evaluation and CI.

Normalize forge-neutral objects but retain provider-specific metadata needed
for fidelity. Pin API versions where possible and report unsupported features
explicitly. Handle pagination, rate limits, retries, idempotency, webhook
verification, credential rotation, and sync cursors.

The UI must configure provider type, endpoint allow-list, repository mapping,
credential status, webhook setup, sync direction, polling schedule, branch and
MR/PR conventions, label mapping, CI artifact policy, release/versioning rules,
submodules, connection tests, and sync diagnostics. Secrets remain write-only.

## 9. Incremental code-intelligence service

Add a replaceable `code-intelligence` service optimized for local incremental
analysis. Use Tree-sitter for syntax, SCIP where an ecosystem supplies a useful
indexer, and read-only Language Server Protocol integrations where they improve
symbol/reference resolution. Do not make an LSP capable of arbitrary workspace
commands.

Maintain one project-local or controller-managed SQLite index per repository
identity and revision lineage. Key source records by Git blob hash so unchanged
files are not reparsed across branches or worktrees. Store:

- files, languages, symbols, definitions, references, imports, calls, and
  inheritance where resolvable;
- tests and their likely production targets;
- configuration, schema, migration, API, UI route, documentation, installer,
  and deployment relationships;
- generated/vendor/binary classification;
- parser/indexer version and confidence;
- provenance back to commit/blob/path/range.

Support incremental update, corruption detection, rebuild, export of diagnostic
summaries, retention limits, and schema migration. Treat parse/index failures as
partial evidence rather than silently complete results.

The UI must show indexing status, freshness, language coverage, failures,
storage use, graph queries relevant to a change, rebuild/pause controls,
retention, tool versions, and per-project index configuration.

## 10. Context Compiler

Replace ad-hoc context assembly with a controller-owned Context Compiler. For
each agent stage, it must construct a bounded, deterministic context packet
from:

- the task contract and accepted clarifications;
- exact changed/target symbols and their dependency/reference neighborhood;
- relevant tests and verification commands;
- repository instructions and protected-path policy;
- recent, relevant Git history and blame only where useful;
- authoritative schemas, API contracts, and documentation;
- verified OpenViking memories with provenance and freshness;
- baseline/differential results and unresolved findings.

Every included item must have a reason, source, version, trust classification,
and token/byte cost. Deduplicate overlapping content, prefer exact symbol ranges
over whole files, reserve budget for agent output, and apply configurable
per-stage budgets. Repository content is untrusted and cannot alter controller
instructions.

Persist a redacted `context-manifest.json` containing selection reasons and
hashes, not hidden reasoning. The UI must allow operators to inspect context
composition, provenance, exclusions, budget use, truncation, stale memory, and
selection rules; compare manifests between runs; and tune safe selectors and
budgets at system/project/job-template scope.

## 11. Clarifier and task-contract stage

Add a Clarifier agent before implementation. It turns an issue or free-form
request into a versioned structured contract containing:

- requested behavior and explicit non-goals;
- affected users/systems;
- acceptance criteria;
- constraints and invariants;
- likely components and risks;
- required evidence and documentation;
- assumptions, ambiguities, and questions;
- a machine-readable completion checklist.

Use schema-constrained model output and controller validation. The agent may
propose questions but cannot broaden scope. For unattended jobs, project policy
must decide whether low-risk assumptions are allowed, whether the job pauses,
or whether it is rejected.

The UI must provide contract review/edit/approve, question-and-answer history,
assumption policy, templates, schema version, diff, and traceability to the
source issue. Human edits must be attributed and preserved.

## 12. Risk-based workflow routing

Compute a transparent risk assessment from configured, deterministic signals:

- protected or sensitive paths;
- public API/schema/database changes;
- authentication, authorization, secrets, cryptography, and network exposure;
- dependency and supply-chain changes;
- installer/service/signing/hardware behavior;
- migration/data-loss potential;
- numerical/statistical behavior;
- breadth of dependency graph and test impact;
- weak/missing tests or baselines;
- generated/binary artifacts;
- ambiguity in the task contract.

Risk determines required stages, budgets, reviewers, test depth, documentation,
manual gates, and publish permissions. Keep the scoring explanation and policy
decision as evidence. Operators may request a higher risk level. Lowering or
waiving requirements requires permission, reason, expiry where appropriate, and
an audit event.

The UI must include a risk-rule editor, matrix/threshold editor, simulation
against an example or real change, effective-policy preview, decision
explanation, waiver workflow, and history.

## 13. Baseline and differential verification

Before modifying a repository, capture a policy-defined baseline in an
equivalent clean environment. Compare the candidate against that baseline for:

- build/test/lint/type-check failures;
- coverage and mutation score;
- performance or resource budgets where configured;
- API, schema, database, UI, screenshot, and installer behavior;
- dependency, license, vulnerability, and SBOM changes;
- generated files and documentation;
- warnings, logs, and flaky-test behavior.

Classify findings as pre-existing, resolved, newly introduced, changed, or
indeterminate. A baseline never excuses a newly introduced failure. Baseline
creation/update must be explicit, versioned, attributable, and policy-gated.
Never let an agent silently approve a new golden output.

The UI must provide baseline policy, capture status, artifact comparison,
classification correction with audit, approval of intentional baseline/golden
updates, retention, and side-by-side differential views.

## 14. Test-impact analysis and staged verification

Combine coverage maps, code-intelligence relationships, changed symbols, test
history, and capability-pack rules to select a fast targeted test set for inner
repair loops. Record why each test was selected or omitted and the confidence
of the selection.

Targeted tests improve iteration speed but cannot replace policy-required final
verification. Medium/high-risk jobs and publishable outputs must run the full
required suite in a fresh environment unless an explicit policy waiver exists.

The UI must show the impact graph, proposed targeted suite, confidence,
estimated duration, full-suite requirements, historical outcomes, overrides,
and policy explanation.

## 15. Content-addressed caches

Introduce isolated caches for safe, reproducible acceleration:

- source/blob parsing and code-intelligence indices;
- BuildKit layers;
- `sccache` or ecosystem-equivalent compiler caches;
- Composer, NuGet, npm, R, and other dependency downloads;
- deterministic test/build results where policy permits;
- documentation rendering and generated API references;
- model prompt-prefix state only after exact-runtime validation.

Cache keys must include all relevant inputs: repository identity, commit/tree or
patch hash, toolchain/container digest, lockfiles, command template version,
runner profile, environment variables that affect output, policy/schema
versions, and parser/test versions. Isolate caches by project/trust domain,
verify integrity, enforce quotas and retention, and prevent credentials from
entering cache artifacts.

Final release verification must be able to require clean caches. The UI must
show hit/miss/invalid reasons, size, age, provenance, quota, retention, project
isolation, purge/verify/warm actions, and cache-policy simulation. Purge is an
audited destructive action with an exact scope preview.

## 16. Schema-constrained agent contracts

Version every inter-agent and agent-controller contract with JSON Schema.
Require grammar/schema-constrained decoding when the selected llama.cpp runtime
and model profile support it, and strict controller validation in all cases.

Structured outputs include:

- task contracts;
- plans and claimed file scope;
- test proposals;
- findings, severity, confidence, and evidence IDs;
- repair dispositions;
- documentation impacts and generated artifact manifests;
- risk assessments;
- completion summaries and unresolved limitations.

Reject malformed or unauthorized actions. A model may retry a bounded number of
times with validation errors, after which the workflow pauses or fails
according to policy. Claims must cite artifact IDs or be marked as unsupported.

The UI must expose contract versions, validation errors, retry policy,
compatible agent/model profiles, structured output viewers, and schema
migration status.

## 17. Independent Test Designer agent

For medium/high-risk jobs, run a Test Designer in a fresh context that does not
see the implementation agent's narrative. It receives the approved task
contract, relevant authoritative context, baseline evidence, and the candidate
diff. It must identify missing tests, boundary cases, failure modes, regression
risks, and suitable golden/rehearsal checks.

It may propose or author tests in an isolated branch/worktree, but those tests
must pass the same verification and review gates. It cannot weaken existing
tests, update goldens, or classify failures as acceptable.

The UI must configure triggers, model/agent profile, budgets, allowed actions,
required dispositions, independence rules, and project conventions. Display
test proposals and their resolution alongside QC findings.

## 18. Golden-master and rehearsal testing

Provide reusable, policy-controlled golden and rehearsal tests for:

- web screenshots, DOM/accessibility snapshots, and user journeys;
- API schemas, fixtures, and recorded protocol interactions;
- Windows installer/service lifecycle;
- database migrations and rollback where supported;
- statistical outputs and numerical tolerances;
- instrument contracts through simulators.

Goldens must be versioned artifacts with provenance. Updating a golden is a
reviewable change requiring reason and policy-defined approval; never a
convenient automatic response to failure.

The UI must render meaningful diffs, support candidate-vs-approved comparison,
manage tolerances/masks for unstable data, and enforce approval policy.

## 19. Documentation Agent and documentation policy

### 19.1 Dedicated documentation stage

Add a Documentation Agent that runs after implementation verification and
before final QC, in a fresh context tailored to documentation. It must:

- evaluate documentation impact even when the implementation agent claims none;
- update or create required source-controlled documentation;
- generate references from authoritative source/schema where configured;
- preserve project terminology, navigation, style, and versioning;
- update examples, configuration references, migration/upgrade notes,
  troubleshooting, changelog/release notes, and architecture records as policy
  requires;
- produce a structured manifest linking each documentation change to the task,
  code, tests, and source of truth;
- identify documentation it could not validate.

Documentation changes are ordinary reviewed code changes. The agent cannot
publish to an external site or overwrite an approved golden screenshot.

### 19.2 Documentation policy engine

Implement declarative policies that map change evidence to required documents,
checks, render targets, reviewers, and gates. Rules must support:

- paths, symbols, labels, capability packs, languages, and risk levels;
- public API, CLI, configuration schema, database, dependency, UI, installer,
  service, security, statistical, and hardware-interface changes;
- required README/user/admin/developer/API/runbook/changelog/ADR/release-note or
  validation-protocol updates;
- “documentation not required” evidence and approval;
- freshness and ownership rules;
- target formats and publication eligibility.

Git remains authoritative. OpenViking may index approved documentation and
decisions with commit/provenance, but it must not become the source of truth.

### 19.3 Documentation toolchain

Support pinned, project-selectable tooling for:

- Markdown and AsciiDoc source;
- Mermaid, PlantUML, and Graphviz diagrams;
- Pandoc rendering to HTML, PDF, and DOCX where configured;
- Vale and project-specific prose/style rules;
- link, anchor, spelling, and navigation checking;
- executable/tested code snippets in a sandbox;
- screenshot generation and freshness through Playwright;
- OpenAPI/JSON Schema validation and generated references;
- phpDocumentor, DocFX, Doxygen, TypeDoc, `roxygen2`, and `pkgdown` where
  applicable.

Use only tools selected and pinned by trusted capability packs or project
configuration. Generated documentation must record tool versions and source
hashes.

### 19.4 Documentation QC

QC must independently check accuracy against code/schema, task coverage,
broken links, stale examples/screenshots, unsupported claims, terminology,
accessibility, rendering, and policy completeness. Blocking documentation
findings follow the same resolution/waiver state machine as code findings.

### 19.5 Documentation web interface

Provide dedicated pages for:

- Documentation Agent profiles, models, budgets, triggers, and permissions;
- policy rule builder and advanced source view;
- change-to-document impact simulation;
- source-of-truth mappings;
- renderer/tool profiles and version status;
- style guides, terminology, owners, templates, and target audiences;
- live validation, render preview, artifact download, and side-by-side diff;
- screenshot/golden management;
- finding resolution, waiver, and audit history;
- publication readiness without automatic publication.

All documentation features must be configurable in the same registry and web
workbench as other features.

## 20. OPA-backed deterministic policy

Add Open Policy Agent as an embedded library or narrowly exposed sidecar. Use it
for deterministic decisions about:

- risk routing and required stages;
- protected paths and allowed runner/tool profiles;
- network/egress and external-service use;
- remote model provider, data-classification, residency, cost, and fallback
  eligibility;
- baseline/full-test/QC/documentation requirements;
- memory promotion;
- forge publication;
- waivers and approvals;
- signing, installers, Windows workers, VPN, and physical hardware.

Provide curated policy templates and safe structured editors for common rules,
plus an advanced Rego editor only for authorized operators. Validate, format,
unit-test, and simulate policies before activation. Pin the policy bundle in
each job snapshot. If OPA is unavailable or a decision fails, fail closed for
protected actions.

The UI must show policy source, structured interpretation, inputs with secret
redaction, decision, explanation, tests, coverage where available, bundle
version, activation history, simulation, staged rollout, and rollback.

## 21. Resource-aware scheduler

Extend the scheduler for the dual-socket CPU-only host. It must account for:

- NUMA topology, physical/logical cores, RAM, disk, and I/O pressure;
- loaded model and cost of model transitions;
- remote-provider health, rate limits, concurrency, quotas, cost ceilings, and
  batch availability;
- runner/test/documentation resource profiles;
- project fairness, job priority, maintenance windows, and deadlines;
- cgroup limits, CPU affinity, `nice`/`ionice`, and bounded concurrency;
- reservation of capacity for the controller and dashboard;
- thermal/health signals when available without privileged host control.

Support at least two modes:

- quality/latency: advance one important job with minimal queue delay;
- throughput/batching: group stages by compatible loaded model and runner
  profile while respecting deadlines and fairness.

Never co-reside workloads whose declared combined memory can destabilize the
host. Persist scheduling decisions and reasons.

The UI must provide topology/status, resource profiles, limits, priorities,
fairness, model-batching windows, maintenance schedules, queue simulation,
decision history, and safe benchmark-derived recommendations.

## 22. Model provider gateway and safe runtime optimization

### 22.1 Provider-neutral gateway

Add a trusted `model-provider-gateway` or an equivalent bounded module. It is
the only component allowed to call local or remote inference endpoints. Agents
and untrusted runners receive a provider-neutral inference contract and never
receive endpoint URLs, provider credentials, cloud credentials, or unrestricted
network access.

The gateway must normalize only the common semantics needed by this system:

- system/developer/user messages or equivalent structured input;
- text and structured JSON output;
- tool/function-call requests, with tool execution remaining controller-owned;
- usage, finish/stop reason, refusal, request ID, timing, and provider errors;
- streaming where useful and bounded non-streaming for unattended jobs;
- cancellation, deadlines, retries, idempotency where supported, and circuit
  breaking;
- model capability discovery/probing and explicit capability overrides;
- optional provider-native prompt caching and batch/asynchronous execution.

Do not force every provider feature into a misleading lowest-common-denominator
field. Keep validated, adapter-specific options in a namespaced profile and
record their exact effective values in the job snapshot. Reject arbitrary
provider request fields from the browser or an agent.

The gateway must support multiple named endpoint profiles of the same type,
including LAN-hosted servers, private enterprise endpoints, and public hosted
services. It must remain replaceable and must not make a single commercial
gateway library the system's configuration authority.

### 22.2 Required interface families

Implement and contract-test adapters for these commonly used interface
families, subject to their configured endpoint actually supporting the required
capabilities:

- local llama.cpp through the existing model supervisor;
- OpenAI Responses API for new OpenAI-style integrations;
- OpenAI Chat Completions for compatible/legacy endpoints;
- generic OpenAI-compatible Responses and/or Chat Completions endpoints used by
  servers such as vLLM, Ollama, Hugging Face TGI, and other LAN gateways;
- Azure OpenAI/Foundry Responses and Chat Completions, including deployment,
  endpoint, API-version, and supported enterprise authentication differences;
- Anthropic Messages, including streaming and batch mode when enabled;
- Google Gemini Interactions and `generateContent`, plus Vertex AI endpoint and
  authentication variants where configured;
- Amazon Bedrock Converse/ConverseStream and its supported Responses-compatible
  endpoint where available.

An optional LiteLLM or other broker may be configured through its exposed
OpenAI-compatible interface, but it must not be required. Provide an adapter
extension contract for future provider families. Adding an adapter requires a
versioned capability manifest, schemas, fixtures, security review, UI coverage,
and contract tests; it must not require controller changes beyond registration.

“OpenAI-compatible” is not a capability claim. Probe and record support for, at
minimum:

- Responses versus Chat Completions;
- streaming and cancellation;
- structured output/JSON Schema or constrained decoding;
- tool calls, parallel-tool semantics, and stable call IDs;
- system/developer-message behavior;
- context and maximum-output limits;
- usage/token accounting;
- reasoning controls and opaque reasoning-state continuation;
- prompt caching and cache accounting;
- synchronous, batch, and asynchronous modes;
- model listing and immutable model/deployment identifiers.

Never infer unsupported capability from a model name alone. Allow an authorized
operator to correct probe results, with warning, reason, expiry/revalidation,
and audit history.

### 22.3 Provider, endpoint, and model profiles

Represent remote inference as separate versioned objects:

- provider profile: interface family, trust/data-handling classification,
  organization/project/account metadata, and authentication method;
- endpoint profile: allow-listed base URL, region, tenant/resource/deployment,
  TLS/custom-CA/mTLS settings, timeout, proxy, DNS/IP policy, and health check;
- model profile: exact provider model or deployment ID, role eligibility,
  capabilities, context/output limits, sampling/reasoning controls, price
  assumptions, and quality/evaluation status;
- route profile: ordered eligible models, local/remote preference, data policy,
  fallbacks, retry/circuit-breaker behavior, batch policy, and cost/latency
  budgets.

Store credential references separately from profiles. Support appropriate
write-only secret types, such as API keys, Azure identity/service-principal
references, AWS credential/role profiles, and Google service-account/workload
identity references, without copying long-lived credentials into job records.
Prefer short-lived/workload identity when the deployment supports it.

Discovering a provider's model list is optional convenience, not authority.
Pin the exact chosen model/deployment and the observed capability manifest in
each job. Detect provider-side aliases or capability drift and pause protected
work until the configured policy allows the new identity.

### 22.4 Data governance and remote egress

Remote inference is disabled by default globally and for every existing
project. Enabling it requires an authorized operator and an effective OPA
decision. Support project data classifications and provider trust tiers that
control whether a remote request may contain:

- task metadata only;
- documentation/public source only;
- selected symbols or tests;
- a candidate diff;
- the complete Context Compiler packet;
- OpenViking memory excerpts;
- security findings, SBOM data, logs, or build/test artifacts.

Secrets, credentials, signing material, hidden reasoning, unrelated repository
content, and raw environment dumps are never eligible for remote transmission.
Run secret/credential and sensitive-pattern scanning after context compilation
and immediately before serialization. A remote model receives only the exact
policy-approved packet, not repository or memory access.

For each provider profile, record operator-supplied contractual facts such as
permitted region, retention, training use, subprocessors, enterprise agreement,
and approved data classes. Label them as operator assertions with evidence and
review/expiry dates; do not treat marketing claims or an API setting as proof.

Before the first request under a new or changed job snapshot, create an egress
manifest showing destination profile, model/deployment, data classes, files or
artifact IDs, redactions, byte/token estimate, purpose, policy decision, and
retention setting. Make it visible in the UI and retain its hash as evidence.
For sensitive/high-risk projects, policy may require explicit per-job approval.

Endpoint security must include HTTPS by default, certificate verification,
optional custom CA/mTLS for LAN/enterprise endpoints, redirect rejection unless
the destination is separately allow-listed, DNS/IP revalidation, response-size
limits, decompression limits, and SSRF protection. Private/LAN addresses are
allowed only in endpoint profiles explicitly classified for that network zone.

Provider-native tools, web search, file stores, code execution, computer use,
memory, or retrieval are disabled by default. Enable only individually through
a provider capability profile and OPA policy. Remote tool calls are treated as
untrusted proposals; the controller validates and executes only existing
allow-listed tools. Do not upload files or create provider-side persistent
threads/vector stores unless a separate policy explicitly permits their data
lifecycle.

### 22.5 Routing, fallbacks, cost, and unattended batch work

Allow role-specific routing for implementation, Clarifier, Test Designer,
Documentation Agent, QC, summarization, and evaluation. A route may prefer
local models, remote models, or a measured hybrid. Quality/evaluation status,
data policy, required features, availability, context size, cost, and deadline
determine eligibility before priority is considered.

Fallback must never widen data exposure. A failed local or enterprise endpoint
cannot silently fall back to a public provider, different region, lower trust
tier, weaker retention policy, incompatible model, or model lacking structured
output. Cross-provider fallback requires an explicitly configured eligible
route and must be recorded in the job evidence.

Support configurable per-request, per-job, per-project, and monthly token/cost
budgets; concurrency and rate limits; provider quotas; retry budgets; and a hard
spend circuit breaker. Pricing is versioned operator configuration and must be
shown as an estimate, then reconciled with provider-reported usage where
available. Missing usage must not be interpreted as zero cost.

Because the appliance runs noninteractively, support provider-native batch or
asynchronous APIs when the adapter can preserve the same contract and policy.
Batch submission, polling, cancellation, expiry, result correlation, partial
failure, and billing evidence must be resumable. Never mix projects or trust
domains in a provider batch merely to improve discounts or throughput.

### 22.6 Remote-model web interface

All remote model functionality must be configurable and operable through the
same configuration registry and web workbench. Provide purpose-built pages for:

- provider and endpoint creation from trusted interface-family templates;
- write-only authentication setup and rotation status;
- endpoint allow-list, TLS/custom CA/mTLS, proxy, region, and network-zone
  configuration;
- safe connection test using a non-sensitive synthetic prompt;
- model discovery/import, manual pinning, exact identity, and capability matrix;
- capability probes, incompatibility diagnostics, overrides, and revalidation;
- role assignment and visual route/fallback editor;
- project data classifications, provider trust tiers, egress rules, and policy
  simulation;
- preflight egress manifest and redaction preview;
- rate, concurrency, retry, timeout, circuit-breaker, batch, and quota controls;
- price tables, token/cost budgets, estimates, actual usage, and alerts;
- health, latency, error/rate-limit history, request IDs, and provider drift;
- audit, configuration revisions, dry run, effective values, export with
  redaction, and rollback.

The UI must label local, LAN/private, and public remote profiles distinctly and
show which data boundary a selected route crosses. It must never expose a raw
credential or permit an arbitrary URL/model argument in a job submission.

### 22.7 Local model/runtime optimization

Retain the Increment 1 model supervisor and allow-listed local profiles. Add a
reproducible benchmark lab for:

- prompt ingestion and generation speed;
- task-quality fixtures;
- context size and memory use;
- thread count, CPU affinity, NUMA policy, batch sizes, and mmap behavior;
- compatible prompt/prefix caching;
- quantization/model alternatives.

Enable prompt-prefix caching only when model identity, tokenizer, template,
llama.cpp build, runtime arguments, policy, and prefix hash match exactly.
Invalidate safely on any mismatch.

Do not enable speculative decoding, multi-token prediction, KV-cache
quantization, experimental kernels, or other optimizations by default merely
because they benchmark faster. Each optimization requires capability detection,
quality fixtures, determinism checks, memory measurement, documented tradeoffs,
and per-profile opt-in.

The UI must configure approved local model/runtime profiles, role assignments,
benchmarks, quality thresholds, caching, experimental-feature gates, and
rollbacks. It must display exact build/model/template/hash identity and compare
quality/speed/memory results. Historical evaluations must compare local, remote,
and hybrid routes without automatically changing the active route.

## 23. Evidence and traceability graph

Build an evidence layer linking:

`issue/request → task contract → requirement → risk → symbol/file → test → result
→ finding/disposition → documentation → memory → commit → PR/MR/release → SBOM`

A relational SQLite representation with indexed edges is sufficient; do not add
a graph database unless measurements prove it necessary. Every edge must record
type, provenance, producer, timestamp, version, and confidence where relevant.
Artifacts use content hashes and stable IDs.

Use the graph to detect orphan requirements, unsupported claims, missing tests,
unresolved findings, stale documentation, and unexplained release artifacts.
Optionally emit in-toto/SLSA-compatible attestations for build/release evidence,
without claiming a higher assurance level than the system actually achieves.

The UI must offer task-centric traceability, filters, missing-edge warnings,
artifact inspection/download, provenance, attestation status, and redacted
export. A compact graph visualization is useful, but an accessible table view is
mandatory.

## 24. Historical patch evaluation lab

Create an offline evaluation harness that samples historical commits or curated
maintenance fixtures, hides the known patch, runs selected workflow profiles,
and compares proposed changes and verification outcomes with known history.

Measure at least:

- task completion and test success;
- regression rate;
- diff scope and unnecessary churn;
- finding precision/recall where labeled;
- context tokens/bytes and irrelevant-context ratio;
- wall time, CPU time, memory, and cache effect;
- documentation-policy compliance;
- operator interventions and unresolved uncertainty.

Prevent training/evaluation leakage from OpenViking memories and cached outputs
by using an isolated evaluation namespace. Results compare profiles; they do
not automatically promote a model or policy.

The UI must configure datasets, repository/revision ranges, exclusions, profile
matrix, budgets, concurrency, scoring, retention, and promotion gates; launch
and monitor runs; compare results; and export a reproducible report.

## 25. OpenTelemetry observability

Instrument the controller, scheduler, model supervisor, model-provider gateway,
code intelligence, Context Compiler, runners, forge adapters, agents,
documentation pipeline, cache, and policy engine with OpenTelemetry traces,
metrics, and correlated structured logs.

Capture operational facts such as stage duration, queue time, retries, model
load/prompt/decode metrics, remote-provider latency/status/rate limits/usage,
context size, index work, cache results, test counts, documentation renders,
policy decisions, and resource usage. Never capture secrets, raw hidden
reasoning, unrestricted source content, remote request bodies, or sensitive
prompts by default.

Provide a lightweight local default suitable for one host, configurable
retention and sampling, redaction tests, support-bundle export, and documented
optional external OTLP export that is disabled by default and endpoint
allow-listed.

The UI must expose health, job timeline, bottlenecks, resource trends, model and
cache performance, errors, retention/sampling/redaction, export status, and
trace-to-artifact navigation.

## 26. Dashboard information architecture

Integrate Increment 2 into one coherent dashboard. At minimum provide these
routes or equivalent navigable areas:

1. **Setup and health** — prerequisites, service health, migrations, backups,
   runner/worker/model/tool inventory, and diagnostics.
2. **Repositories** — onboarding, Repo Doctor, effective packs, forge sync,
   code index, conventions, and drift.
3. **Capability packs** — catalog, versions, trust, compatibility, assignment,
   configuration, and upgrade/rollback.
4. **Jobs** — task contract, risk, context manifest, timeline, agents,
   verification, findings, docs, evidence, and publish gates.
5. **Quality** — baselines, differential verification, test impact, Test
   Designer, QC, golden/rehearsal artifacts, waivers, and conventions.
6. **Documentation** — agent, policies, source mappings, tools, preview,
   findings, and readiness.
7. **Code intelligence** — indices, coverage, graph queries, failures, and
   maintenance.
8. **Forges** — GitHub, GitLab, local Git, credentials, mappings, webhooks,
   synchronization, and diagnostics.
9. **Runners and Windows** — Linux profiles, disposable services, Windows VM
   workers, simulators, capacity, and gated capabilities.
10. **Models and agents** — local and remote providers, endpoints, credentials,
    capabilities, egress policy, routing/fallbacks, role assignments, contracts,
    budgets/cost, llama.cpp profiles, benchmarks, and safe optimizations.
11. **Scheduling and resources** — queue, NUMA/CPU/RAM/I/O, fairness, batching,
    priorities, and maintenance windows.
12. **Policy and risk** — OPA bundles, structured rules, simulations, decisions,
    approvals, and waivers.
13. **Security and SBOM** — scan profiles, findings, suppressions, FMEA, SBOM
    diff, and release evidence.
14. **Evaluation** — historical patch datasets, runs, comparisons, and profile
    promotion proposals.
15. **Memory and evidence** — OpenViking provenance/correction plus the evidence
    and traceability views.
16. **Observability** — health, traces, metrics, logs, bottlenecks, and support
    bundles.
17. **Configuration** — registry-wide search, effective values, drafts,
    revisions, imports/exports, dependencies, audit, and rollback.

Every domain page must link directly to its filtered configuration view. Every
configuration field must link back to the feature/status that consumes it.
Preserve responsive design and WCAG-oriented accessibility: labels, keyboard
support, focus order, high-contrast status that does not rely on color,
screen-reader announcements, reduced motion, and accessible tables/diffs.

## 27. Data model and migrations

Add versioned records for at least:

- configuration descriptors, scoped values, drafts, revisions, and migrations;
- configuration dependencies, dry runs, and effective snapshots;
- repo scans, findings, proposals, and accepted onboarding revisions;
- capability packs, trust metadata, assignments, and effective contributions;
- forge providers, mappings, cursors, webhook events, and sync diagnostics;
- code indices, blobs, symbols, references, relationships, and tool versions;
- context manifests and selection entries;
- task contracts, questions, answers, assumptions, and approvals;
- risk assessments, policy inputs/decisions, waivers, and bundle versions;
- baselines, comparisons, classifications, and golden approvals;
- test-impact selections and historical test outcomes;
- caches, keys, provenance, quotas, and maintenance events;
- agent contracts, outputs, validation failures, and dispositions;
- documentation impacts, sources, renders, manifests, and findings;
- workers, resource profiles, benchmarks, and scheduling decisions;
- model providers, endpoints, credential references, deployments, observed
  capabilities, routes, egress manifests, usage/cost, and drift events;
- evidence nodes/edges and attestations;
- evaluation datasets, runs, profiles, metrics, and reports;
- telemetry configuration and support-bundle metadata.

Use forward-only, transactional migrations with preflight checks, backup
guidance, idempotent retry where safe, and explicit rollback instructions.
Preserve job/config history and provenance. Add migration tests from every
supported Increment 1 schema version.

## 28. Security requirements

Threat-model every new trust boundary. In addition to Increment 1 requirements:

- treat repo onboarding, documentation, diagrams, LSP/indexers, test discovery,
  forge content, SBOM metadata, and evaluation fixtures as untrusted inputs;
- treat remote-model output, tool calls, usage records, provider model lists,
  capability claims, redirects, errors, and streamed events as untrusted input;
- sandbox parsers/renderers/indexers and disable active content in previews;
- validate archives against traversal, symlinks, decompression bombs, and size
  limits;
- enforce outbound endpoint allow-lists and DNS/IP revalidation;
- route every inference request through the trusted model-provider gateway;
  block direct agent/runner access to provider or LAN inference endpoints;
- make remote inference opt-in per system and project, scan the final request
  for secrets/sensitive patterns, and enforce data-classification policy before
  any bytes leave the host;
- verify TLS by default, constrain redirects/proxies/custom CAs, and prevent
  DNS rebinding or cross-zone endpoint changes;
- keep provider credentials write-only, scoped, rotatable, and unavailable to
  agents, runners, prompts, job snapshots, support bundles, and telemetry;
- isolate cache, index, evaluation, and memory namespaces by project and trust
  domain;
- require checksummed/pinned toolchains, packs, images, and model manifests;
- use short-lived scoped credentials where supported;
- protect policy activation, golden updates, waivers, signing, hardware, and
  forge publication with RBAC and explicit gates;
- render Markdown/HTML/SVG/diagram output safely with scripts, remote resources,
  and unsafe links disabled;
- scan generated downloadable artifacts and enforce retention/size quotas;
- redact secrets and sensitive repository content from telemetry, exports, and
  support bundles;
- maintain an append-only or tamper-evident audit chain for privileged events.

Add automated authorization, injection, path traversal, SSRF, archive, webhook,
secret-redaction, cross-project isolation, unsafe-preview, stale-edit, and
policy-fail-closed tests.

## 29. Suggested implementation shape

Prefer extending the existing repository layout. Introduce bounded modules or
services such as:

```text
services/
  code-intelligence/
  windows-worker-adapter/
packages/
  config-registry/
  capability-packs/
  context-compiler/
  forge-providers/
  policy/
  evidence/
  documentation/
  telemetry/
capability-packs/
  php83-intranet/
  windows-dotnet-labautomation/
  r-statistical-validation/
  sbom-fmea-security/
evals/
  historical-patches/
```

This is illustrative, not permission to duplicate existing modules. Reuse the
controller, runner, UI, agent harness, storage abstractions, and security
boundaries. Prefer SQLite, JSON Schema/OpenAPI, OPA, Tree-sitter/SCIP/LSP,
content-addressed local storage, and OpenTelemetry over new infrastructure.

## 30. Delivery milestones

Keep the system runnable and migrations reversible or recoverable after each
milestone. Use feature flags defaulting new high-risk integrations to off until
configured.

### Milestone 1 — configuration and UI foundation

- Inventory every existing setting and migrate it into the configuration
  registry or explicitly document why it is immutable bootstrap state.
- Implement scopes, precedence, snapshots, descriptors, API, permissions,
  revisions, audit, redaction, validation, dry runs, import/export, and rollback.
- Implement the reusable web configuration workbench and contract tests.
- Migrate Increment 1 UI/config without changing effective behavior.

### Milestone 2 — intelligence and fast feedback

- Implement code intelligence and incremental blob-hash indexing.
- Implement Context Compiler and context manifests.
- Add content-addressed caches, baseline/differential verification, and
  test-impact selection.
- Expose and configure all components in the dashboard.

### Milestone 3 — onboarding and capability packs

- Implement the capability-pack framework and trust/version lifecycle.
- Implement Repo Doctor and proposal review.
- Deliver and test `php83-intranet`.
- Add golden/rehearsal framework primitives.

### Milestone 4 — heterogeneous repositories and forges

- Implement the forge abstraction, preserve GitHub, and add GitLab and local
  bare Git.
- Deliver Windows/.NET, R/statistical, and SBOM/FMEA/security packs.
- Implement the simulated Windows worker, then the documented real-worker
  adapter without requiring it in CI.

### Milestone 5 — quality workflow expansion

- Implement Clarifier/task contract and risk-based routing.
- Add schema-constrained agent contracts.
- Add independent Test Designer and integrate findings/dispositions.
- Add Documentation Agent, documentation policy, render/validation toolchain,
  documentation QC, and full documentation UI.
- Integrate OPA policy simulation, activation, and fail-closed enforcement.

### Milestone 6 — evidence, efficiency, and operations

- Implement evidence/traceability graph and optional attestations.
- Add resource-aware scheduling and model-phase batching.
- Add the provider-neutral model gateway, fake adapters for every required API
  family, remote-data governance, cost/routing controls, and complete web UI.
- Add safe local/remote model benchmark and optimization management.
- Add historical patch evaluation lab.
- Add OpenTelemetry instrumentation and observability UI.

### Milestone 7 — hardening and release readiness

- Complete end-to-end UI, API, CLI, migration, security, accessibility,
  backup/restore, upgrade/rollback, and recovery tests.
- Verify performance and resource behavior on a representative dual-socket,
  128-GB profile or a documented constrained simulation.
- Complete operator, administrator, contributor, API, security, pack-author,
  Windows-worker, and troubleshooting documentation.

## 31. Testing requirements

### 31.1 Configuration and web coverage

Maintain a machine-readable inventory mapping every configurable capability to:

- registry descriptor;
- API endpoints;
- dashboard route/control;
- permission;
- validation and dry-run tests;
- persistence/effective-value test;
- audit/redaction test;
- rollback test;
- user documentation.

CI must fail if a registered public feature lacks web configuration coverage or
if a UI setting bypasses controller validation. Exercise forms with browser
end-to-end tests, including keyboard and screen-reader-relevant semantics.

### 31.2 Unit and property tests

Cover schema validation, precedence, migrations, cache keys, index updates,
context selection/budgets, risk signals, policy inputs, differential
classification, test impact, evidence edges, pack resolution, redaction,
worker protocol, forge normalization, model-provider normalization, route
eligibility, capability negotiation, cost ceilings, egress classification, and
fallback non-escalation. Use property/fuzz tests for parsers, imports,
manifests, webhook payloads, paths, archives, streamed provider events, tool
calls, and untrusted Markdown.

### 31.3 Integration tests

Use small fixture repositories representing:

- PHP 8.3/Composer/MySQL/Redis/Vite/Tailwind;
- .NET/PowerShell/installer behavior through a fake Windows worker;
- an R package with deterministic statistical goldens;
- mixed-language and malformed repositories;
- GitHub/GitLab adapters through recorded/fake servers;
- fake OpenAI Responses/Chat, Azure OpenAI, Anthropic Messages, Gemini
  Interactions/`generateContent`, Bedrock Converse/Responses, and partially
  OpenAI-compatible servers;
- local bare Git for complete offline sync and publish rehearsal.

Test baseline-vs-candidate classification, partial index failures, stale config
edits, interrupted jobs, service restart/resume, pack upgrades, policy rollback,
cache corruption, OpenViking staleness, renderer failure, remote-provider
timeouts/rate limits/stream interruption/batch partial failure, model or
capability drift, credential rotation, forbidden redirect/DNS rebinding,
cross-trust fallback rejection, cost circuit breaking, egress redaction, and
telemetry redaction.

### 31.4 End-to-end workflow tests

At minimum prove these flows without external credentials or large models:

1. onboard a fixture repo entirely from the UI;
2. accept detected packs and review effective configuration;
3. clarify a task and approve the task contract;
4. compute risk, baseline, code impact, context, and targeted tests;
5. run implementation with a deterministic fake model;
6. run independent Test Designer, verification, Documentation Agent, docs QC,
   and code QC;
7. block on a deliberately unresolved finding;
8. repair or explicitly waive according to policy;
9. create a local bare-Git publish artifact;
10. inspect the complete evidence chain;
11. change a setting in the UI, preview/apply it, verify API/CLI consistency,
    inspect the audit event, and roll it back;
12. configure a fake remote endpoint entirely through the UI, probe and pin its
    capabilities, assign an eligible role route, inspect/approve an egress
    manifest, complete a job, and verify usage/cost/evidence without exposing
    its credential;
13. prove that remote inference is disabled by default and that a forbidden
    data class, endpoint, redirect, model drift, or lower-trust fallback fails
    closed;
14. back up, migrate/restart, restore, and resume without losing provenance.

### 31.5 Performance and soak tests

Use repeatable synthetic and fixture workloads to verify:

- unchanged blobs are not re-indexed;
- targeted tests reduce inner-loop time while final required tests still run;
- caches are invalidated by every relevant input and cannot cross projects;
- context packets remain within configured budgets;
- model batching respects fairness/deadlines;
- remote routing respects provider concurrency/rate/cost limits and can use
  asynchronous batches without mixing projects or trust domains;
- dashboard/controller remain responsive under a long model or test job;
- telemetry and evidence collection remain bounded;
- a multi-project queue survives restarts and extended execution.

## 32. Documentation deliverables

Update or add:

- architecture and trust-boundary diagrams;
- configuration registry and precedence reference;
- complete dashboard operator guide;
- repository onboarding and capability-pack authoring guide;
- GitHub, GitLab, and local Git setup;
- Windows worker/simulator setup and security guide;
- PHP, R/statistical, and SBOM/FMEA pack guides;
- code intelligence and Context Compiler internals;
- baseline, test-impact, cache, risk, OPA, QC, and waiver behavior;
- Documentation Agent and documentation-policy authoring guide;
- local and remote model-provider setup, interface compatibility, credential
  handling, capability probing, routing, data classification, egress approval,
  cost control, and troubleshooting;
- model benchmarking and resource scheduler guide for the target host;
- evidence, evaluation, observability, backup/restore, upgrade, rollback, and
  troubleshooting guides;
- API/OpenAPI and configuration schema references;
- a limitations document distinguishing CI fakes from operator-validated
  external integrations.

Documentation examples must match tests and the shipped UI. Generate screenshots
only from deterministic test data and keep them covered by freshness checks.

## 33. Definition of done

Increment 2 is complete only when all of the following are true:

1. Increment 1 tests and supported workflows still pass.
2. Existing durable data migrates with documented backup and recovery.
3. Every routine feature in this specification is configurable and operable
   through the web interface, not just through files or CLI.
4. The configuration coverage inventory has no unexplained gaps.
5. UI, API, and CLI produce identical validated effective configuration.
6. Effective values show scope/provenance; job snapshots remain immutable.
7. Secrets are write-only and absent from responses, exports, logs, telemetry,
   caches, screenshots, artifacts, and support bundles.
8. Repo Doctor creates evidence-backed proposals and never silently applies
   them.
9. Capability packs are pinned, inspectable, reversible, and cannot introduce
   arbitrary controller execution.
10. GitHub behavior is preserved; GitLab and local bare Git pass adapter and
    workflow tests.
11. Incremental indexing, Context Compiler, differential verification,
    test-impact selection, and caches demonstrate correct invalidation and
    measurable efficiency improvement on fixtures.
12. Medium/high-risk fixtures invoke the Clarifier, Test Designer,
    Documentation Agent, documentation QC, code QC, and all policy-required
    deterministic gates.
13. A new failure cannot be hidden as pre-existing, a targeted suite cannot
    replace required final verification, and a golden cannot be updated without
    approval.
14. Documentation policies correctly require, generate, render, validate,
    review, and trace target-specific documentation.
15. OPA decisions are versioned, explainable, simulatable, and fail closed for
    protected actions.
16. The scheduler prevents unsafe resource combinations and records decisions.
17. Model/runtime optimizations are benchmarked, quality-gated, explicitly
    enabled, and reversible.
18. Local-only operation remains complete, while optional OpenAI-style,
    Azure OpenAI, Anthropic, Gemini/Vertex, and Bedrock provider families pass
    their fake-server contract and web-configuration tests.
19. Remote inference is disabled by default; credentials remain write-only;
    egress is classified, scanned, previewable, policy-approved, and evidenced;
    and fallback cannot widen exposure or bypass capability/cost requirements.
20. The evidence view traces a request through code, tests, findings,
    documentation, memory, commit, forge artifact, and SBOM where applicable.
21. Historical evaluation is isolated from project memory and produces a
    reproducible comparison report.
22. OpenTelemetry provides useful bounded diagnostics without leaking secrets,
    source content by default, or hidden reasoning.
23. Security, cross-project isolation, redaction, authorization, injection,
    archive, SSRF, unsafe-preview, webhook, and stale-edit tests pass.
24. Browser accessibility checks and manual keyboard workflows pass for all
    critical configuration and job-control paths.
25. Backup/restore and upgrade/rollback preserve configuration history,
    evidence, jobs, indices or rebuildability, and audit records.
26. The stack remains startable through the documented Compose workflow and
    usable on the target CPU-only hardware without Kubernetes or cloud services.
27. External items unavailable in CI have safe fakes plus precise, finite
    operator validation procedures; they are never falsely reported as tested.

## 34. Codex operating instructions

When implementing this goal:

1. Read `LOCAL_CODE_MAINTAINER_GOAL.md`, repository guidance, current code,
   migrations, tests, and working-tree state first.
2. Produce a requirement-to-implementation/test matrix and keep it current.
3. Reconcile this logical design with existing architecture; do not duplicate
   working services merely to match suggested names.
4. Preserve unrelated operator changes and never use destructive Git commands.
5. Implement milestone by milestone with small, reviewable changes and a
   runnable repository after each milestone.
6. Prefer pinned, maintained dependencies and record license/security
   implications.
7. Use fake local models, protocol-accurate fake remote-provider endpoints,
   fake forge endpoints, local bare Git, disposable containers, and a simulated
   Windows worker in CI.
8. Never download large model weights or contact real model providers,
   repositories, VPNs, signing services, or physical instruments without a
   separate explicit operator instruction.
9. Treat UI completion as part of each feature, not a final cosmetic phase.
10. Do not mark a feature complete until its schema, API, UI, permissions,
    validation, audit, rollback, tests, and documentation are complete.
11. Run the narrowest relevant tests during development and the full required
    suite before final handoff.
12. Report commands run, test results, migrations, security-relevant decisions,
    measured performance, known limitations, and operator-only validation.
13. If a requirement cannot be implemented safely with the repository's actual
    constraints, stop at the safe boundary, document the blocker and evidence,
    and do not substitute an insecure approximation.
