## 1. Starting point and mission

This is the third increment of the local, quality-first, multi-project code
maintenance appliance defined by:

- LOCAL_CODE_MAINTAINER_GOAL.md;
- LOCAL_CODE_MAINTAINER_INCREMENT_2_GOAL.md;
- the repository's actual implemented architecture, migrations, tests, and
  security boundaries.

Read both earlier goals and inspect the repository before changing anything.
Produce an inherited-requirements status report. If an Increment 1 or Increment
2 requirement on which this increment depends is incomplete, implement or
repair the smallest necessary prerequisite rather than creating a parallel
substitute. Do not falsely mark inherited work as complete.

Increment 3 must deepen correctness, containment, reproducibility, and
evidence. It adds:

- observable agent-trajectory evaluation and deterministic replay;
- risk-tiered Linux execution using rootless containers, gVisor where
  configured, and Landlock defense in depth where supported;
- formal controller workflow invariants with executable model checks;
- controlled chaos testing and a system-wide emergency stop;
- continuous API, protocol, parser, archive, and webhook fuzzing;
- explicit API/schema compatibility gates;
- dependency admission, exploitability disposition, and controlled update
  intake;
- reproducible artifact verification;
- structural convention and architecture enforcement;
- systematic flakiness, .NET concurrency, and property-based testing;
- optional bounded patch minimization;
- governed MCP compatibility as a controlled tool boundary;
- version-pinned OpenTelemetry GenAI conventions;
- a closed-world Model Selection Optimizer that evaluates only models already
  visible through the configured model gateway;
- progressive task-assurance profiles with deterministic, evidence-triggered
  escalation; and
- a simplified operating model that presents one task flow, one findings
  inbox, one approval surface, shared workers, and explicit effective plans.

The target remains a CPU-only Linux host with two Xeon E5-2699-class
processors, 128 GB RAM, local NVMe, Docker Compose, sequential large-model
loading, and unattended work where quality is more important than latency.
Local-only operation remains complete. Remote models and MCP servers remain
optional and policy controlled.

## 2. Non-negotiable outcomes

At completion:

1. The appliance must produce stronger evidence about how an agent reached a
   result, not merely whether its final text looked plausible.
2. High-risk repository execution must have a stronger selectable isolation
   boundary than an ordinary container.
3. Workflow safety properties must be explicitly specified, automatically
   checked, and reflected in executable controller tests.
4. Failure recovery must be tested under injected faults without duplicate
   publication, lost provenance, widened egress, or policy bypass.
5. Dependency, contract, architectural, reproducibility, and flakiness changes
   must become first-class review evidence.
6. MCP tools, if enabled, must be registered, fingerprinted, scoped,
   intercepted, audited, and unable to bypass the controller.
7. The Model Selection Optimizer must recommend the best evidenced model
   configuration for a purpose only from models visible to the system.
8. Every routine operational control introduced here must be available through
   the existing configuration registry and web interface.
9. Normal maintenance must be startable with repository, task, and confirmation
   while the system automatically chooses and explains the least costly
   workflow that satisfies quality, risk, and policy requirements.
10. Advanced capabilities must remain available without forcing every routine
    task to execute, configure, or understand the entire platform.

The browser must remain a typed, accessible operational interface—not a raw
YAML/JSON editor. API, CLI, and web behavior must use the same controller
services, validation, authorization, revisions, and audit semantics.

Foundational safety invariants may be immutable. They must be visible,
versioned, documented, and reported in the web interface, but must not be
casually disableable. Operator-tunable thresholds, schedules, resource budgets,
profiles, and project policy must use the normal configuration workflow.

## 3. Product principles

Apply these priorities in order:

1. Correctness, safety, reproducibility, and evidence.
2. Containment and least privilege.
3. Purpose-specific measured quality.
4. Reliability across repeated trials and failure conditions.
5. Minimal, relevant context and deterministic preprocessing.
6. Efficient use of the dual-socket CPU-only host.
7. Explainable recommendations and reversible activation.
8. Progressive depth: pay verification cost in proportion to evidenced risk.
9. One coherent operational experience over internal specialist capabilities.
10. Cost and latency after quality requirements are satisfied.

LLM output remains advisory. A language model must not be the sole authority
for policy, functional correctness, security clearance, dependency admission,
or model selection.

## 4. Inherited boundaries and additional prohibitions

All Increment 1 and Increment 2 boundaries remain in force. Additionally, do
not:

- record hidden chain-of-thought or provider reasoning traces;
- confuse observable actions and concise decisions with hidden reasoning;
- treat seccomp alone as a complete sandbox;
- expose the Docker socket, container runtime socket, host Unix sockets,
  arbitrary devices, privileged mode, or raw host networking to an agent;
- copy the broad host-socket examples from demonstration gVisor deployments;
- silently fall back from a required gVisor or Windows-VM profile to a weaker
  runtime;
- inject chaos into production jobs, real provider accounts, real forge
  publication, or physical equipment;
- permit fuzzers to contact unapproved endpoints or escape their runner;
- let a vulnerability scanner's absence or stale database mean “no
  vulnerabilities”;
- permit dependency-update automation to merge or publish autonomously;
- use a syntax-aware display diff as the authoritative Git patch;
- let retries convert an intermittently passing test into a clean pass;
- execute an MCP server discovered from repository text or model output;
- pass bearer tokens through an MCP proxy without audience validation;
- let an MCP tool description become a controller instruction;
- let a model under evaluation score its own functional correctness;
- search the Internet, Hugging Face, OpenRouter, public leaderboards, or other
  catalogues for candidate models in the Model Selection Optimizer;
- automatically download, pull, install, or register a model as part of model
  optimization;
- recommend a model that was not present in the pinned visible-model inventory
  used by the experiment;
- enable learned or exploratory live routing for protected production tasks;
- claim formal verification of implementation code merely because a bounded
  workflow model passed.

## 5. Cross-cutting configuration and web requirements

Every configurable feature in this increment must register a versioned
configuration descriptor using the Increment 2 registry. Each descriptor must
include schema, UI metadata, legal scopes, permissions, defaults, secret
classification, validation, dry-run behavior, dependencies, activation impact,
audit redaction, export/import, and migration rules.

At minimum, add configuration namespaces for:

- trajectory evaluation and replay;
- isolation and runner runtime profiles;
- formal-model bounds and reports;
- chaos profiles and emergency-stop behavior;
- fuzz targets, corpora, schedules, and resource budgets;
- compatibility gates and waiver rules;
- dependency admission and update intake;
- reproducible-build profiles;
- structural and architectural rules;
- flakiness and concurrency testing;
- patch minimization;
- MCP registry, permissions, authentication, and drift handling;
- OpenTelemetry GenAI schema/version and content policy;
- Model Selection Optimizer purpose profiles, candidate selection, evaluation
  suites, resource/cost budgets, scoring, approval, activation, and drift;
- deployment and task-assurance profiles, resolver/escalation policy, effective
  plans, Basic/Expert views, findings/approvals, shared-worker resource policy,
  setup/doctor behavior, Compose profiles, recovery, and notifications.

Every feature must provide:

- typed web controls and contextual help;
- effective-value and source-scope display;
- validation and prerequisite status;
- dry-run, simulation, or test action where meaningful;
- revision history, diff, audit, and rollback;
- permission checks and re-authentication for privileged changes;
- direct links between configuration, operational status, evidence, and
  troubleshooting;
- API and CLI parity;
- accessible keyboard and screen-reader behavior.

Immutable invariants must appear as read-only signed/versioned facts with their
source revision and latest check result.

## 6. Observable agent-trajectory evaluation

### 6.1 Normalized trajectory contract

Extend the historical patch evaluation lab with a versioned observable
trajectory contract. Capture only controller-visible events:

- task-contract revision and risk classification;
- context-manifest identity;
- agent/model configuration and prompt/scaffold version;
- tool availability presented to the agent;
- requested tool name and validated arguments;
- policy decision and resulting transformed/denied request;
- tool start, completion, timeout, cancellation, and normalized result status;
- content/artifact hashes and bounded redacted summaries;
- file/diff/test/documentation artifacts created;
- verification gates invoked, skipped, passed, failed, or waived;
- concise agent claims and their cited evidence IDs;
- retries, loops, handoffs, escalation, and termination reason;
- resource, token, time, and cost measurements where available.

Never store hidden reasoning. Provider “thinking” fields must be discarded
unless a future explicit policy permits a bounded derived summary; raw hidden
reasoning is out of scope.

### 6.2 Trajectory metrics

Score at least:

- correct tool selection;
- argument/schema accuracy;
- required-step coverage;
- policy compliance;
- grounding of claims in actual artifacts or tool results;
- behavior after empty, partial, malformed, or failed tool results;
- forbidden or unnecessary calls;
- retry and loop efficiency;
- correct escalation or safe stop;
- publication-gate compliance;
- final deterministic task outcome.

Functional tests and deterministic evidence dominate. Optional model-judge
metrics must be separately labeled, reproducible where possible, capped in
weight, and never substitute for executable verification.

### 6.3 Safe replay

Implement deterministic replay against pinned repository snapshots and
protocol-accurate fake tools. Replay must:

- never repeat an external side effect;
- replace forge, provider, signing, network, and MCP mutations with fakes;
- preserve tool responses or fixture versions;
- detect contract drift;
- compare two agent/model/runtime configurations step by step;
- produce a portable redacted report;
- isolate replay data from project memory and live jobs.

The UI must provide trajectory timelines, filters, per-step evidence, metric
breakdown, two-run comparison, replay controls, redaction status, and export.

## 7. Risk-tiered execution isolation

### 7.1 Runtime profiles

Add controller-owned runtime profiles with at least:

1. rootless restricted container for ordinary trusted builds;
2. gVisor runsc container for high-risk or untrusted Linux execution;
3. disposable Windows VM/worker for Windows-specific execution;
4. deterministic fake runner for CI and tests.

Risk policy and capability packs choose the minimum required profile. A job may
use a stronger compatible profile, never a weaker one without an explicit
policy-approved waiver. Record the selection and reasons in evidence.

### 7.2 gVisor profile

The gVisor profile must:

- use a pinned, health-checked runsc installation and OCI integration;
- default to no network;
- expose only exact controller-approved mounts;
- use read-only roots and read-only source mounts where the phase permits;
- create a separate writable worktree/output mount;
- deny host Unix sockets, Docker/container sockets, arbitrary devices,
  privileged mode, raw packet access, and host PID/IPC/network namespaces;
- apply cgroup CPU, memory, process, I/O, file-size, and time limits;
- use a dedicated identity and per-job filesystem namespace;
- clean up deterministically after cancellation or host restart;
- publish compatibility diagnostics without exposing host internals.

Do not silently enable broad host UDS access or networking to make a tool work.
Record compatibility exceptions per pinned tool and require explicit approval.

### 7.3 Landlock and defense in depth

Use Landlock for controller-side helpers, parsers, renderers, or suitable
runner processes where the host kernel supports the required ABI. Detect the
available ABI and enforce only explicitly supported rights.

Landlock augments, rather than replaces, namespaces, cgroups, AppArmor/SELinux,
seccomp, no-new-privileges, read-only mounts, and gVisor. If policy requires a
specific Landlock capability and it is unavailable, fail closed and show exact
operator guidance.

The application must not alter host boot configuration or install gVisor
itself. The web UI must show prerequisites, install guidance, detected version,
compatibility checks, benchmark overhead, and re-check controls.

## 8. Formal workflow invariants

Create a small, reviewable TLA+/PlusCal model of the controller's protected
workflow states. Include at least:

- job creation, approval, execution, cancellation, retry, resume, and terminal
  states;
- implementation, verification, Test Designer, documentation, QC, waiver, and
  publication gates;
- local/remote egress approval;
- emergency stop;
- lease/claim ownership and restart recovery;
- publication idempotency.

Specify and check invariants including:

- no publish without every policy-required successful gate;
- unresolved blocking findings cannot advance;
- a waiver applies only to its exact scope and validity period;
- a stopped system starts no new model, tool, runner, MCP, or forge actions;
- remote egress never occurs before applicable approval;
- fallback never widens exposure or drops required capabilities;
- a job is published at most once for an idempotency key;
- cancellation and restart cannot manufacture a successful result;
- a credential is never part of agent-visible state;
- terminal state and evidence remain consistent.

Run TLC in CI with bounded representative configurations. Store counterexamples
as test artifacts and translate every invariant into corresponding executable
controller property/state-machine tests.

The formal model is design evidence, not proof of the full implementation. The
UI must show model revision, invariant list, latest CI/check result, bounds,
counterexample links, and implementation-test mapping. Invariants themselves
are version-controlled and read-only in routine UI.

## 9. Chaos testing and emergency stop

### 9.1 Controlled fault injection

Implement chaos profiles for test and rehearsal environments only. Support
bounded injection of:

- model timeout, stream interruption, malformed JSON, invalid tool call, and
  repeated call;
- provider 429, 5xx, capability drift, partial batch failure, and usage
  omission;
- runner crash, forced cancellation, process leak, and OOM;
- SQLite/database busy, transaction failure, and delayed commit;
- disk-full, artifact-store failure, and cache corruption;
- forge timeout, conflict, duplicate webhook, and rate limit;
- MCP timeout, schema drift, server crash, and denied capability;
- controller restart during execution, verification, commit, or publication.

Each chaos scenario must declare blast radius, environment restrictions,
resource/time limits, expected invariant, cleanup, and success criteria.
Production endpoints, real publication, signing systems, and physical
instruments are forbidden targets.

### 9.2 Emergency stop

Provide a highly visible, RBAC-protected emergency stop that:

- atomically blocks new model, agent, tool, runner, MCP, remote-egress, and
  forge-publication actions;
- supports configured drain or cancel behavior for in-flight safe work;
- can still permit read-only inspection, deterministic verification, evidence
  export, backup, and recovery operations;
- persists across controller restart;
- records initiator, reason, time, affected jobs, and recovery approval;
- requires explicit authorized recovery and health re-check.

Expose status prominently in the dashboard and CLI. Test races between stop,
job claim, provider request, runner start, and publication.

## 10. Continuous fuzzing

### 10.1 Fuzz targets

Add bounded fuzzing for:

- the controller OpenAPI surface;
- supported test-repository OpenAPI and GraphQL endpoints;
- configuration imports and migrations;
- capability-pack and model/provider manifests;
- MCP definitions and messages;
- forge webhooks;
- streamed provider events and tool calls;
- archives, paths, symlinks, and decompression limits;
- Markdown, HTML, SVG, diagrams, documentation metadata, and previews;
- SBOM, VEX, scanner, and dependency metadata;
- worker protocols and serialized evidence.

Use schema-aware API testing such as Schemathesis where compatible and a
ClusterFuzzLite-style corpus/minimization workflow for native/parser targets.
Pin tools and run them inside restricted runners.

### 10.2 Execution policy

Provide:

- short per-change fuzz budgets;
- scheduled deeper local runs;
- corpus versioning and deduplication;
- deterministic seed capture;
- minimized reproductions;
- sanitizer support where the target language permits;
- coverage reporting;
- resource, disk, and retention limits;
- promotion of confirmed crashes into regression tests.

Fuzzers must use fake authentication and disposable data. They must not discover
or contact arbitrary endpoints. A finding must identify target/tool/version,
seed/corpus, minimized input, stack/result, affected boundary, disposition, and
regression-test status.

## 11. API and schema compatibility gates

Add a versioned compatibility-gate abstraction and concrete adapters for:

- OpenAPI through oasdiff or an equivalent pinned engine;
- GraphQL through GraphQL Inspector or an equivalent pinned engine;
- Protobuf through Buf breaking checks or an equivalent pinned engine;
- JSON Schema through the existing schema tooling;
- database migrations through capability-pack-specific migration checks.

Compare the candidate against the correct merge base, approved release, or
configured contract baseline. Classify changes as breaking, dangerous,
warning, informational, or documentation-only.

Protected breaking changes require:

- explicit affected consumers and migration plan;
- versioning/deprecation analysis;
- targeted compatibility tests;
- scoped approval or waiver with owner and expiry;
- evidence and release-note/documentation linkage.

The UI must show semantic contract diffs, raw source diff, affected consumers,
policy decision, waiver status, and downloadable machine-readable results.

## 12. Dependency admission and exploitability disposition

### 12.1 Admission trigger

Invoke dependency admission whenever a patch changes:

- package manifests or lockfiles;
- container base images or image digests;
- downloaded tools, plugins, MCP servers, models, or capability packs;
- vendored libraries;
- installer payloads;
- transitive dependency resolution.

Compare baseline and candidate, not just absolute state.

### 12.2 Required checks

For each introduced, removed, or changed component, evaluate:

- declared necessity and rejected alternatives;
- source registry/repository and integrity identity;
- direct and transitive expansion;
- known vulnerabilities using OSV-Scanner or configured equivalent;
- license and project policy compatibility;
- install/build scripts and implicit executable behavior;
- suspicious/ambiguous naming and registry confusion risk;
- maintenance/security-health signals, including an optional OpenSSF Scorecard
  adapter;
- supported runtime/platform compatibility;
- SBOM and release-evidence impact.

Scorecard and model-generated assessments are signals, not proof. Stale,
unavailable, or incomplete metadata must be reported as unknown, never clean.
Network metadata retrieval must occur through the controller's governed egress
path, not through an agent or runner.

### 12.3 VEX and exceptions

Support versioned OpenVEX-compatible exploitability dispositions. Every
not-affected or accepted-risk statement must include:

- component and vulnerability identity;
- status and justification;
- supporting evidence;
- affected product/version scope;
- owner, reviewer, creation time, expiry/review date;
- source and policy versions.

Expired or invalidated VEX must reopen the finding. Suppression without
structured justification and expiry is not allowed.

The UI must provide dependency diff, transitive graph, evidence, VEX workflow,
policy simulation, approvals, expiry dashboard, and export.

## 13. Controlled Renovate intake

Add optional self-hosted Renovate integration as a maintenance-task source,
not as an autonomous publisher.

The integration must:

- run in a separate restricted container without a Docker socket;
- use only approved forges, repositories, registries, and credentials;
- default install scripts, arbitrary plugins, shell post-upgrade commands, and
  unsafe executions to disabled;
- import proposals into the controller as normal versioned task contracts;
- include release notes/metadata references where policy permits;
- pass every proposal through dependency admission, implementation, tests,
  documentation, QC, and publication gates;
- enforce schedule, concurrency, grouping, rate, and repository filters;
- support dry run and local bare-Git/fake-forge CI.

No Renovate proposal may merge, publish, or widen repository access without the
same approval path as any other maintenance job.

## 14. Reproducible artifact verification

Add reproducible-build profiles for configured release or regulated artifacts.
For an eligible job:

1. build the same source revision twice in independent fresh sandboxes;
2. pin toolchain, image, dependency, environment, and configuration identities;
3. normalize configured time, locale, timezone, path, UID/GID, ordering, and
   other known nondeterminism;
4. compare artifact sets, names, metadata, and hashes;
5. on mismatch, run diffoscope or an equivalent pinned recursive comparator;
6. classify and evidence the nondeterministic cause.

Never overwrite original artifacts. Store both build manifests and bounded
comparison output. Profiles must set artifact filters, normalization rules,
timeouts, disk budgets, accepted nondeterminism, severity, and required gates.

An accepted nondeterminism exception requires scope, cause, risk, owner,
reviewer, expiry, and evidence. A mismatch must not be hidden by excluding the
entire artifact.

## 15. Structural conventions and architecture enforcement

### 15.1 Structural rules

Integrate ast-grep or an equivalent pinned syntax-aware engine for:

- project-owned structural lint rules;
- protected patterns and forbidden constructs;
- deterministic convention checks;
- controlled codemod proposals;
- rule tests and example fixtures.

Rules are versioned project/capability-pack artifacts. Repository-provided rules
are untrusted until reviewed and approved. Automated rewrites require preview,
ordinary diff review, verification, and rollback.

### 15.2 Architecture adapters

Add a generic architecture-rule result contract and at least:

- Deptrac integration for the PHP/intranet pack;
- an adapter point and fixtures for .NET architecture checks;
- dependency-graph based rules for other supported packs.

Support a versioned legacy-debt baseline so existing violations can be visible
without allowing new violations. Baseline changes require approval.

### 15.3 Review presentation

Add Difftastic or an equivalent syntax-aware diff as a secondary review view.
The raw Git diff remains authoritative. Preserve both views and clearly label
parser failures or unsupported languages. QC and users may use the structural
view to understand changes, never to conceal textual changes.

## 16. Flakiness laboratory

Add a flakiness analysis stage that can:

- repeat changed, failed, and policy-selected tests;
- vary deterministic seeds, execution order, shard, locale, timezone, and
  bounded concurrency;
- record environment and runner identity;
- estimate repeat failure probability and confidence;
- distinguish likely test nondeterminism, product race, infrastructure fault,
  and unknown cause;
- preserve every individual outcome.

Do not implement “retry until green.” The original failure remains visible.
A job may proceed only according to explicit policy and evidence.

Quarantine requires test identity, owner, reason, linked issue, scope, creation
time, review date, expiry, and permitted workflows. Expired quarantine blocks
or reopens according to policy. New or worsened flakiness must be compared with
the baseline and included in QC.

## 17. .NET concurrency and property-based verification

Extend the Windows/.NET lab-automation capability pack with:

- Microsoft Coyote or an equivalent pinned systematic concurrency test
  adapter;
- FsCheck or an equivalent pinned property-based test adapter;
- deterministic fake timers, devices, transports, failures, and cancellation;
- replayable schedules/seeds and minimized failing cases.

Cover representative invariants for:

- cancellation and timeout;
- retry and idempotency;
- service start/stop/recovery;
- message ordering and duplicate delivery;
- state-machine transitions;
- serialization and protocol round trips;
- installer upgrade/uninstall preservation;
- simulated instrument safety boundaries.

Normal CI uses the fake Windows worker or an available disposable Windows
worker. Physical instruments and production Windows machines are out of scope.
The UI must manage profiles, schedules, budgets, findings, reproduction data,
and operator-only real-worker validation.

Capability packs may add equivalent property/metamorphic checks for PHP and R.
For statistical workflows, support configured invariants such as seeded
determinism, serialization round trips, unit conversion, monotonicity, bounds,
and reference-result tolerance.

## 18. Bounded patch minimization

Add an optional finalizer for medium/high-risk patches. It must:

- operate on an isolated copy of the already passing candidate;
- preserve the original candidate unchanged;
- attempt bounded removal of files, hunks, or edits;
- rerun configured impacted checks after each candidate reduction;
- run all policy-required final verification on the selected smaller patch;
- accept only a strictly smaller patch that preserves every required result;
- stop on budget, ambiguity, nondeterminism, or new failure;
- send the result through independent QC and documentation impact again.

Record attempted reductions, checks, outcomes, resource use, original/selected
diff identities, and reason for selection. Patch minimization is never allowed
to weaken tests, policy, documentation, or evidence merely to reduce size.

## 19. Governed MCP compatibility

### 19.1 Scope and architecture

MCP is an optional controlled compatibility plane for approved tools,
resources, or integrations. It must not replace the controller's internal
workflow contracts, runner protocol, provider gateway, forge abstraction, or
policy engine.

Every MCP interaction flows:

~~text
agent request
→ controller validation
→ identity/capability check
→ OPA/policy decision
→ MCP gateway
→ approved server
→ normalized untrusted result
→ evidence and trajectory record
~~

Agents and runners must not connect directly to MCP servers.

### 19.2 Server registry

Every server registration must pin:

- stable server ID, owner, purpose, and trust classification;
- local stdio or remote HTTP transport;
- exact approved local command/image and arguments, or allow-listed remote
  endpoint;
- version and digest where available;
- expected capabilities and tool/resource/prompt schemas;
- authentication reference;
- project/agent/role capability scopes;
- network, filesystem, runner, timeout, rate, and size policy;
- approval status and review/expiry date.

Repository text, model output, one-click links, and untrusted imports may
propose a server but can never execute or approve it. Local MCP servers run in
the configured restricted/gVisor profile with exact mounts and no ambient
credentials.

### 19.3 Fingerprints and drift

Fingerprint names, descriptions, annotations, input/output schemas, and server
identity. On any material change:

- mark the server/tool as drifted;
- block protected use;
- show a semantic and raw diff;
- rerun security and contract checks;
- require explicit reapproval.

Treat descriptions and returned content as untrusted data. Scan for hidden
instructions, suspicious Unicode, typosquatting, schema broadening, and
capability expansion.

### 19.4 Authentication and network security

For remote HTTP servers:

- use standards-compliant OAuth/OIDC flows where supported;
- require TLS verification and protected-resource metadata validation;
- use short-lived, audience-bound, least-privilege tokens;
- prohibit token passthrough;
- validate redirect URIs exactly;
- enforce DNS/IP revalidation, private-range policy, redirect limits, and SSRF
  protection;
- keep credentials write-only and outside agent-visible state.

Local stdio servers receive only controller-provided scoped environment and
must not inherit the controller's full environment.

The UI must provide registry inventory, schema viewer/diff, capability matrix,
approval, health, invocation trail, drift/quarantine, credentials status,
policy simulation, disable, and emergency-stop integration.

## 20. OpenTelemetry GenAI conventions

Extend Increment 2 observability with a version-pinned OpenTelemetry GenAI
semantic-convention profile and compatibility layer.

Normalize:

- provider and model/deployment identity;
- agent and operation type;
- request IDs and response IDs where safe;
- input/output/cache token counts;
- tool and MCP spans;
- retries, fallbacks, errors, and rate limits;
- local runtime load, prompt evaluation, generation, memory, and total job
  timing;
- evaluation suite, trial, recommendation, and trajectory links.

Prompt, response, source, tool payload, and hidden reasoning content are off by
default. Opt-in content capture, if ever permitted, must pass data
classification, redaction, retention, and access policy. High-cardinality and
sensitive attributes must be bounded or hashed appropriately.

Pin the semantic-convention schema version in each trace/export. Support
migration/normalization when conventions evolve without rewriting historical
evidence.

## 21. Closed-world Model Selection Optimizer

### 21.1 Purpose

Implement a separate Model Selection Optimizer that identifies the strongest
evidenced candidate model configurations for a declared purpose from models already
visible through the system.

It must produce recommendations, confidence, evidence, alternatives, and
activation proposals. It must not autonomously activate a recommendation for a
protected workflow.

The optimizer must reuse the provider-neutral model gateway, resource
scheduler, historical evaluation lab, trajectory evaluator, capability packs,
evidence graph, policy engine, telemetry, configuration registry, and
dashboard. It must not call model providers directly.

### 21.2 Closed-world visibility invariant

A model is visible only when it is present in the model gateway's normalized
inventory from one of:

- an installed and registered local llama.cpp/Ollama-compatible endpoint;
- the list-models result of an enabled, authenticated, approved remote endpoint;
- a manually registered model profile for an endpoint without listing support.

Visibility is further constrained by endpoint, system, project, purpose,
data-classification, capability, and operator allow-lists.

The optimizer must not:

- search external model catalogues or the public Internet;
- inspect public leaderboards to add candidates;
- query an unconfigured endpoint;
- auto-create an endpoint or account;
- download, pull, install, or register model weights;
- recommend an invisible, disabled, forbidden, unhealthy, or stale model;
- widen a candidate set beyond the operator-selected visible inventory.

Public or local benchmark data may evaluate visible models but may never
introduce new candidate models. This closed-world rule is a non-disableable
controller invariant with explicit tests.

### 21.3 Inventory snapshots

Before every experiment, create an atomic, immutable visible-model inventory
snapshot containing:

- gateway and configuration revision;
- endpoint ID and trust zone;
- normalized provider/model/deployment ID;
- exact local digest or provider-exposed version/revision where available;
- observed capabilities and source of each claim;
- context limits;
- local format, parameter size, quantization, file size, and runtime
  compatibility where available;
- pricing/rate metadata revision where configured;
- health and latest probe;
- allowed data classes, projects, purposes, and roles;
- deprecation/preview status;
- inclusion/exclusion reason.

Every trial and recommendation references this snapshot. If identity,
capability, policy, endpoint health, digest, version, pricing, or availability
changes, mark affected recommendations stale according to policy.

### 21.4 Candidate model configurations

Optimize a complete candidate model configuration, not only a model name:

- model revision/deployment;
- endpoint/provider;
- local quantization and runtime build;
- context size and context policy;
- CPU thread, batch, NUMA, and memory settings where applicable;
- prompt/scaffold and agent-contract version;
- tool/structured-output mode;
- reasoning budget or supported provider control;
- sampling and retry policy.

Search only approved bounded values registered by the controller. Do not expose
arbitrary model/runtime arguments in the UI.

### 21.5 Purpose profiles

Create versioned purpose profiles for at least:

- clarification/planning;
- implementation;
- Test Designer;
- code QC;
- documentation generation;
- documentation QC;
- security/risk analysis;
- repository summarization/context assistance;
- capability-pack-specific maintenance, including PHP/intranet,
  Windows/.NET/lab automation, R/statistical validation, and SBOM/FMEA.

A profile defines:

- task distribution and fixtures;
- languages/frameworks;
- risk and data classes;
- required capabilities and minimum context;
- local/remote eligibility;
- hard quality/safety thresholds;
- scoring dimensions and weights;
- repeat count and confidence policy;
- resource/time/cost budgets;
- allowed candidate-selection modes;
- activation and re-evaluation policy.

### 21.6 Candidate-selection modes

The web UI and API must support:

- all visible and eligible models;
- local visible models only;
- remote visible models only;
- selected endpoints;
- selected models/candidate configurations;
- currently assigned profiles plus selected challengers;
- tag/capability filters;
- exclusion of preview, deprecated, unhealthy, unversioned, or over-budget
  candidates.

The final included set and every exclusion reason must be visible before
starting an experiment.

### 21.7 Safe staged evaluation

Use staged successive elimination to conserve CPU and remote budget:

1. metadata and policy feasibility;
2. controlled local load/health/resource probe or remote capability probe;
3. small capability and contract suite;
4. approved general anchor suite where configured;
5. purpose-specific repository/fixture suite;
6. repeated finalist trials;
7. optional non-publishing shadow/rehearsal on approved work.

Early elimination requires an explainable rule and evidence. Do not eliminate a
candidate merely because it is slow if it remains within the purpose's hard
budget and quality evidence is incomplete.

Use separate calibration and final holdout tasks. The optimizer must not expose
gold patches, hidden tests, expected tool trajectories, or holdout answers to
the candidate. Prevent evaluation artifacts from entering project memory.

For remote candidates, use only public, synthetic, redacted, or explicitly
approved repository material according to egress policy. Being visible does not
grant permission to receive a given data class.

### 21.8 Evaluation dimensions

Measure at least:

- functional task resolution against hidden deterministic tests;
- regressions;
- robustness and edge-case coverage;
- test quality and mutation/property evidence where available;
- tool selection and argument/schema accuracy;
- structured-output reliability;
- policy compliance and safe failure;
- unsupported claims or fabrication after empty/failed tools;
- patch minimality and architecture/convention compliance;
- documentation accuracy;
- trajectory efficiency and required-gate coverage;
- repeated-trial reliability;
- total job wall time;
- local model load time, prompt processing, generation rate, peak RSS, and
  failures/OOM;
- remote token usage, cost, latency, rate/error behavior, and missing usage.

Time-to-first-token is secondary for unattended maintenance; total successful
job time is more relevant. Missing cost or usage is unknown, not zero.

### 21.9 Scoring and statistical policy

Apply:

1. hard eligibility, safety, capability, data, and quality gates;
2. quality ranking using a configured lower confidence bound rather than mean
   score alone;
3. reliability/variance as the next discriminator;
4. total time, cost, and resource use as tie-breakers;
5. a retained Pareto frontier for quality, reliability, time, cost, and memory.

Provide a safe default coding-maintenance quality composition based primarily
on:

- hidden functional resolution;
- absence of regressions;
- robustness/test quality;
- tool/contract accuracy;
- security, architecture, and minimality;
- documentation accuracy.

Weights are configurable by purpose but deterministic verification must
dominate. Security violations may be hard failures regardless of weighted
score.

Repeat stochastic trials and preserve individual outcomes. Use paired
comparisons and bootstrap or other documented confidence estimation. Report
“insufficient evidence” rather than manufacturing a winner.

The ranking engine must be deterministic ordinary code. An approved model may
write a human-readable summary from the already calculated result, but it
cannot alter scores, eligibility, confidence, or recommendation.

### 21.10 Recommendation portfolio

Produce a versioned recommendation containing:

- purpose and inventory snapshot;
- primary profile;
- local/private fallback;
- remote or high-quality escalation where eligible;
- independent QC candidate from a different family/provider where available;
- smaller specialist profiles for triage/documentation where supported;
- Pareto alternatives;
- evidence, confidence, known limitations, and rejected candidates;
- activation scope;
- validity/review time;
- re-evaluation triggers.

Do not invent a different-family QC recommendation when none is visible and
eligible. State the limitation.

Activation requires an authorized operator, effective-configuration preview,
policy simulation, and revisioned rollback. Existing assignments remain active
until approval.

### 21.11 Drift and re-evaluation

Mark or re-evaluate recommendations when:

- a visible model appears/disappears only if the applicable profile policy
  includes automatic challenge scheduling;
- an assigned model digest/version/capability changes;
- endpoint health or trust changes;
- prompt/scaffold, agent contract, capability pack, policy, or runtime changes;
- evaluation suite or scoring policy changes;
- production evidence crosses a configured drift threshold;
- validity expires;
- an operator requests it.

New visible models may be shown as unevaluated candidates but cannot be
recommended until evaluated. Re-evaluation may be scheduled automatically,
subject to budgets, but activation remains governed.

### 21.12 Routing boundary

This increment may activate deterministic purpose/role assignments and explicit
fallback chains from approved recommendations. Learned prompt-level routing and
contextual-bandit exploration are out of scope for protected live workflows.

If a future rule-based per-task selector is implemented here, it must use
transparent task features, approved profiles, fail-closed capability/data
checks, complete evidence, and no live exploration.

## 22. Model Optimizer web interface

Add a Model Optimizer dashboard area with:

1. purpose-profile wizard;
2. visible-model inventory and last synchronization;
3. local/remote/trust/data-classification badges;
4. candidate inclusion/exclusion matrix;
5. deployment-profile configuration with safe bounded choices;
6. evaluation-suite and holdout metadata;
7. resource, time, repeat, and remote-cost budgets;
8. preflight and egress preview;
9. queued/running/completed experiment view;
10. per-task and per-trial evidence;
11. quality confidence intervals and reliability;
12. Pareto comparison;
13. primary/fallback/specialist recommendation proposal;
14. rejected-candidate reasons;
15. approval, activation, rollback, and validity;
16. stale/drift/unevaluated alerts;
17. scheduled reassessment;
18. exportable reproducible report.

The UI must make the closed-world boundary explicit:

> Recommendations are limited to models currently visible and permitted
> through this system's configured model gateway.

Do not display external “better models” or links that imply they were
considered.

## 23. Evidence and data model

Add versioned records for at least:

- normalized trajectory events, metrics, replays, and comparisons;
- runner runtime profiles, prerequisite probes, isolation decisions, and
  compatibility exceptions;
- formal specifications, invariant revisions, check bounds, results,
  counterexamples, and executable-test mappings;
- chaos profiles, injections, affected jobs, cleanup, and invariant outcomes;
- fuzz targets, corpora, seeds, coverage, crashes, minimizations, dispositions,
  and regression tests;
- compatibility baselines, semantic changes, consumers, decisions, and
  waivers;
- dependency diffs, metadata revisions, admission findings/decisions, VEX,
  approvals, and expiry;
- Renovate source/proposal identities and imported task links;
- reproducible-build profiles, build manifests, artifacts, comparisons,
  diffoscope results, and exceptions;
- structural rules, architecture baselines, findings, codemod proposals, and
  results;
- flakiness trials, seeds/orders/environments, classifications, quarantines,
  and expiry;
- Coyote schedules, property seeds, minimized cases, and findings;
- patch-minimization attempts, candidates, checks, and selection;
- MCP servers, identities, schemas, fingerprints, credentials references,
  capabilities, approvals, invocations, drift, and quarantine;
- GenAI semantic-convention versions and normalization mappings;
- visible-model inventory snapshots;
- purpose profiles, candidate model configurations, suites, tasks, holdouts,
  experiments, trials, scores, confidence, Pareto sets, recommendations,
  approvals, activations, drift, and invalidations;
- deployment/task-assurance profile definitions, resolver/input/capability
  snapshots, requested/effective plans, reason codes, and escalation events;
- normalized findings, groups, ownership, dispositions, waivers, expiry, and
  typed protected-action approvals; and
- checkpoints, recovery attempts, resource decisions, setup/doctor reports,
  notification state, and profile-effectiveness measurements.

Link all records into the Increment 2 evidence graph. Preserve project/trust
isolation, retention, redaction, and immutable job snapshots.

Use forward-only transactional migrations with preflight, backup guidance,
idempotent retry where safe, and explicit recovery. Add migration tests from
every supported Increment 2 schema version.

## 24. Security and threat model

Extend the threat model for:

- malicious model/provider inventory and capability claims;
- model identity alias changes;
- evaluation-data leakage and benchmark poisoning;
- malicious model files and parser bugs;
- evaluator self-scoring or collusion;
- gVisor/runtime misconfiguration;
- Landlock capability gaps;
- fuzz escape and resource exhaustion;
- chaos used against live systems;
- dependency confusion, typosquatting, malicious install scripts, and stale
  vulnerability metadata;
- falsified VEX or permanent suppressions;
- malicious Renovate configuration;
- contract baseline manipulation;
- structural-rule and codemod injection;
- flaky-test laundering;
- MCP tool poisoning, schema drift, confused deputy, SSRF, token passthrough,
  local command execution, and hidden instructions;
- telemetry content leakage;
- model recommendation manipulation and unauthorized activation;
- profile-confusion, forged risk evidence, downgrade/escalation races, resolver
  input tampering, stale-plan execution, and hidden per-run overrides;
- approval-scope confusion, finding suppression/deduplication abuse, and
  non-expiring waivers; and
- shared-worker cross-project leakage, resource starvation, checkpoint replay,
  and duplicate protected actions.

Add automated tests for authorization, CSRF, injection, traversal, SSRF, DNS
rebinding, redirects, token audience, cross-project leakage, secret redaction,
archive bombs, unsafe preview, schema drift, stale edit, race conditions,
emergency-stop races, recommendation tampering, inventory escape, profile
downgrade, plan substitution, approval confusion, and checkpoint replay.

Every privileged state change requires append-only/tamper-evident audit and
appropriate RBAC. Credentials, proprietary source, hidden tests, gold patches,
and holdout answers must not enter model-visible prompts, telemetry, support
bundles, or exports except where an explicit data policy safely permits the
minimum necessary content.

## 25. Suggested implementation shape

Extend existing modules rather than duplicating them. A possible shape is:

~~text
services/
  model-optimizer/
  fuzz-worker/
packages/
  trajectory-eval/
  isolation/
  workflow-invariants/
  chaos/
  compatibility-gates/
  dependency-admission/
  reproducibility/
  structural-rules/
  flakiness/
  mcp-gateway/
  genai-telemetry/
  operating-profiles/
  effective-plan/
evals/
  purpose-profiles/
  trajectory-replays/
formal/
  controller-workflow/
~~

Names are illustrative. Reuse the controller, UI, provider gateway, scheduler,
runnerd, evidence graph, OPA, configuration registry, artifact store,
capability packs, evaluation lab, and telemetry pipeline.

Do not introduce Kubernetes, Kafka, Elasticsearch, Neo4j, Temporal, or a new
global vector database. Prefer the current relational/SQLite storage,
content-addressed artifacts, JSON Schema/OpenAPI, OPA, OCI runners, and
OpenTelemetry.

Agents are controller-owned role definitions executed by shared workers. Do
not create an always-on service, queue, database, or container per agent role.
Where the existing implementation has separable processes, consolidate their
public control surface and reuse the current durable queue and evidence model.

## 26. Progressive operating profiles and operational simplification

The system has many specialist capabilities, but the ordinary experience must
remain one coherent maintenance workflow. Implement progressive operation as
a deterministic planning layer over existing components. It must reduce work
for routine tasks without weakening policy or concealing which checks ran.

### 26.1 Keep profile concepts separate

Use distinct typed concepts. Do not overload a single `profile` field:

1. **Deployment profile** describes installed and enabled infrastructure, such
   as lean local, full local, or controlled hybrid operation.
2. **Task-assurance profile** describes the required depth of one maintenance
   workflow: Routine, Quality, High Assurance, or Forensic.
3. **Risk tier** is derived from task/repository evidence and selects mandatory
   containment, approval, and publication controls.
4. **Runtime isolation profile** remains the runnerd/container/gVisor/Windows
   execution boundary selected under risk policy.
5. **Model-purpose profile** remains the Model Selection Optimizer definition
   for roles such as planner, implementer, reviewer, or documentation agent.

Use stable internal identifiers, schema versions, display names, descriptions,
and migration aliases. Risk policy and immutable safety invariants override all
profiles. Selecting a faster or smaller workflow must never imply weaker
containment or broader data egress.

### 26.2 Required task-assurance profiles

Ship the following versioned built-in profiles. They are configuration bundles
resolved into ordinary stages and policies, not separate orchestration code
paths. Administrators may clone them into custom profiles but may not mutate
the signed built-in definitions in place.

#### Routine

Use only when classification and repository policy allow a small, reversible,
low-risk change. It requires:

- one implementation role, with a lightweight independent deterministic or
  model-assisted review rather than unreviewed publication;
- the minimum sufficient context bundle;
- formatting, linting, type/static checks, and directly impacted tests that
  are applicable and available;
- documentation impact detection and required documentation updates;
- repository convention and secret checks;
- bounded patch size and scope checks;
- evidence-backed completion; and
- no automatic commit, push, pull request, merge, release, or remote egress.

Routine must escalate when evidence becomes inconsistent with low risk. It is
not a bypass for QA, policy, required tests, or approval.

#### Quality

This is the default and the target of `automatic` selection when no stronger
profile is required. It requires:

- explicit planning, implementation, and independent QC/QA roles;
- repository convention, structural, architecture, and security checks
  applicable to the detected capability packs;
- test-impact analysis and all impacted suites;
- Documentation Agent execution when the policy/impact detector requires it;
- patch minimization when eligible and beneficial;
- model selection per role through active approved model-purpose assignments;
- one normalized findings/approval flow; and
- a complete evidence and provenance report.

#### High Assurance

Use for sensitive, broad, irreversible, externally visible, or release-bound
changes. It adds, as applicable:

- the strongest policy-required isolation profile;
- independent plan review or a second independent QA pass;
- full relevant test suites in addition to impacted tests;
- API/schema compatibility and migration checks;
- dependency admission and VEX review;
- fuzzing, property, concurrency, and reproducibility checks supported by the
  repository;
- expanded security and architecture analysis;
- full documentation-policy validation;
- artifact/signing/provenance checks already supported by the inherited stack;
- stricter confidence and unresolved-finding thresholds; and
- mandatory authorized human approval before external publication.

Unavailable required capabilities make the plan blocked or explicitly
operator-validated; they must not disappear from the effective plan.

#### Forensic

Use for unknown regressions, nondeterministic failures, suspected security
events, or repositories whose safe mutation boundary is not yet understood.
The initial phase is read-only and requires, as applicable:

- failure reproduction and competing evidence-backed hypotheses;
- repository history, trajectory, and previous-patch comparison;
- flakiness analysis and deterministic seed/schedule replay;
- bounded Git-bisect assistance;
- artifact or environment comparison;
- no source mutation until the cause/evidence gate passes; and
- explicit transition to Quality or High Assurance for the repair.

Forensic is a diagnostic profile, not permission for unconstrained data
collection or command execution.

### 26.3 Deployment profiles

Provide versioned deployment presets while keeping one application contract:

- `local-lean`: mandatory control plane, shared worker, local model gateway,
  sandbox runner, durable database, and bounded built-in telemetry;
- `local-full`: local-lean plus enabled local assurance workers and optional
  observability services that the host can safely support;
- `hybrid-controlled`: local operation plus explicitly configured remote model
  endpoints and/or remote workers subject to classification, egress, cost,
  credential, and approval policy; and
- `custom`: an administrator-authored derivative validated against the same
  schemas and immutable invariants.

Deployment presets control availability and defaults, not authorization. The
absence of an optional service must produce a visible capability state and plan
decision. It must not cause an implicit network fallback.

### 26.4 Deterministic effective-plan resolver

Implement one versioned resolver shared by UI, API, CLI, scheduler, and replay.
Its conceptual inputs are:

~~text
requested task profile
+ immutable safety invariants
+ installation and organization policy
+ repository policy and detected capabilities
+ task classifier and risk evidence
+ visible model/tool/worker availability
+ current resource and budget constraints
+ data classification and egress policy
= immutable effective execution plan snapshot
~~

The effective plan must contain:

- resolver/schema/configuration versions and input snapshot identities;
- requested and effective task-assurance profiles;
- deployment, risk, isolation, and model-purpose decisions;
- ordered stages, required gates, agents, tools, tests, and evidence;
- selected model assignment or approved selection policy per role;
- resource budgets, scheduling constraints, estimated duration class, and host
  reserve;
- required approvals and protected action boundaries;
- capability omissions, blocks, operator-only validations, and reason codes;
- allowed escalation transitions; and
- redacted human-readable explanations.

For the same normalized inputs, resolver version, and capability snapshot, the
plan must be identical. Persist and hash the plan before execution. Every
trajectory event and finding must reference it. If a material input changes
before execution, generate a new version and require review where policy says
so; never mutate the accepted plan silently.

The resolver must operate without an LLM. Models may propose classification or
provide evidence, but deterministic policy validates the evidence and makes
the effective decision.

### 26.5 Escalation and non-downgrade rules

`Automatic — Quality preferred` is the default user selection. The resolver
may start at Routine only when repository policy explicitly permits the
classified task and all low-risk predicates hold. Otherwise it starts at
Quality or higher.

Define typed, configurable escalation signals with immutable minimums. Include:

- protected/sensitive paths, ownership boundaries, or generated files;
- authentication, authorization, cryptography, secrets, or security controls;
- public API, protocol, database schema, migration, or serialization changes;
- dependencies, lockfiles, build pipelines, release, deployment, or signing;
- concurrency, nondeterminism, native/unsafe code, or privilege boundaries;
- diff size, file count, subsystem spread, or unexpected scope expansion;
- absent/weak tests, falling coverage, flaky results, or inconsistent reruns;
- model/tool disagreement, low confidence, malformed output, or unsupported
  claims;
- architecture, compatibility, dependency, documentation, or policy findings;
- repeated repair loops, budget exhaustion, environmental drift, or runner
  failures; and
- a human or repository rule requesting stronger assurance.

Escalation must be atomic, audited, reason-coded, and visible. It may add work
or move the task to a stronger profile; it must never remove completed evidence
or weaken a mandatory gate. Once mutation has begun, automatic de-escalation is
forbidden. A human may always request a stricter profile. A request for a weaker
profile requires authorization and still cannot override risk or safety policy.

Support a shadow/recommendation-only rollout mode that records what the resolver
would have selected without changing execution. Use it to calibrate thresholds
against real outcomes before enabling automatic escalation per repository.

### 26.6 One normal task flow

The default UI must allow a normal authorized user to:

1. select or register a repository;
2. enter a task or select an imported forge item;
3. leave the profile at `Automatic — Quality preferred`;
4. inspect a concise proposed plan and any required approval/egress decision;
5. start and monitor the run;
6. review one patch, evidence summary, and findings set; and
7. approve, reject, revise, export, or perform an authorized forge action.

Repository plus task plus confirmation must be sufficient when preflight
passes. Do not require users to manually select agents, models, test suites,
queues, containers, or context algorithms for ordinary work.

The primary dashboard navigation must converge on five operational areas:

- repositories;
- task queue;
- active/recent runs;
- findings and approvals requiring attention; and
- system/model/tool/worker health.

Specialist pages may remain for expert investigation, but they must use the
same underlying run, finding, evidence, policy, and configuration identities.

### 26.7 Effective-plan preview and explanations

Before execution, show a bounded plan preview containing:

- requested/effective profile and escalation reasons;
- stages and which are conditional;
- agents, model assignments, tools, and test scopes;
- local/remote execution and data-egress indicators;
- estimated duration and resource classes, clearly labeled as estimates;
- required approvals and publication restrictions;
- unavailable capabilities and their consequence; and
- a Basic/Expert expansion rather than raw configuration.

During execution, show a single state-machine timeline derived from controller
events. Do not synthesize false progress percentages. After execution, retain
the exact accepted and final effective plans, including all escalations.

### 26.8 Unified findings and approvals

Normalize agent, compiler, linter, test, policy, security, documentation,
dependency, reproducibility, and optimizer output into the inherited evidence
graph plus a common finding contract. At minimum include:

- stable identity/deduplication key;
- project, run, plan, stage, source, category, and location;
- severity, confidence, blocking state, and policy basis;
- redacted evidence references and reproduction instructions;
- proposed remediation and owner;
- open/accepted/fixed/waived/expired state;
- waiver scope, justification, approver, and expiry where applicable; and
- timestamps, revisions, and audit references.

Group causally related symptoms without deleting raw findings. Present one
filterable findings inbox and one authoritative approval queue. Applying a
patch, enabling remote egress, running a protected tool, changing dependencies,
committing/pushing, creating/updating a pull request, merging, releasing, and
changing active model assignments must use typed protected-action approvals.
Approval for one action never implies approval for another.

### 26.9 Configuration simplification

Resolve configuration in this order and retain provenance for every effective
value:

~~text
immutable safety defaults
-> installation defaults
-> deployment profile
-> organization policy
-> repository policy
-> task-assurance profile
-> permitted per-run overrides
~~

The web interface must provide Basic and Expert views over the same schema.
Basic exposes the small number of decisions normally needed. Expert exposes all
mutable settings with search, dependency/conflict validation, effective-value
origin, dry run, version history, comparison, import/export, and rollback.

Provide recommended presets, repository templates, `restore recommended`, and
a pre-apply summary of behavioral impact. Immutable values remain visible and
read-only. No operational setting may exist only as an environment variable,
undocumented file key, or hidden API field.

### 26.10 Runtime and deployment simplification

Prefer a small number of cohesive processes using the existing architecture:

- control plane: API, scheduler/orchestrator, policy, configuration, and web UI;
- shared worker pool: planner/implementer/QA/documentation and specialist role
  definitions executed on demand;
- model gateway: local and policy-authorized remote inference;
- sandbox runner: isolated repository commands and tools;
- existing durable relational/SQLite and content-addressed artifact storage;
  and
- optional observability collector/backend.

Do not deploy a service, container, database, queue, or public API per agent.
Do not duplicate the controller for each profile. Where technically practical,
ship one versioned application image with explicit entry points for control
plane, worker, runner, and migrations. Retain isolation boundaries where they
provide actual security value.

Use Docker Compose profiles for optional operational groups, for example
`observability`, `high-assurance`, and configured provider/worker adapters.
The documented normal installation remains one command. Compose-profile names,
dependencies, health checks, port exposure, storage, and upgrade effects must
be surfaced in the dashboard and configuration documentation.

### 26.11 Resource-aware execution and recovery

On the target CPU host:

- serialize memory-heavy local model assignments by default;
- allow safe parallel deterministic tooling within CPU/RAM/IO budgets;
- reserve configurable host memory/CPU and reject unsafe combinations;
- reuse context, parse, embedding, build, test, and model artifacts through the
  inherited correctness-safe caches;
- checkpoint at stage boundaries and before protected external actions;
- resume from durable evidence without repeating non-idempotent actions;
- prioritize interactive control/emergency-stop health over background work;
- provide fair multi-project scheduling and bounded overnight queues; and
- explain queueing/resource decisions in the run timeline.

Model unload/load cost, context length, historical reliability, and expected
verification retries must influence scheduling and the optimizer's portfolio
selection, but cannot weaken minimum quality or policy gates.

### 26.12 Setup, health, and lifecycle operations

Provide a guided first-run flow and a non-mutating system doctor covering:

- database/migrations and storage capacity;
- repository credentials and forge connectivity;
- visible local/remote models without discovering or installing public models;
- runner/isolation prerequisites;
- worker/tool/capability-pack availability;
- configuration conflicts and unresolved secrets;
- backup/restore readiness;
- telemetry/redaction state; and
- the first repository preflight.

The doctor must produce concrete remediations and distinguish automatic safe
repairs from operator actions. It must never install models, weaken policy,
change host security configuration, or transmit source without explicit
authorization.

Add restart recovery, stale-workspace cleanup, retention previews, backup and
restore, configuration migration, support-bundle redaction, and actionable
notifications. Routine healthy state must be quiet; alerts require an owner,
severity, evidence, and recommended action.

### 26.13 Backward compatibility and observability

Migrate existing profile-like settings into the new typed namespaces without
changing effective Increment 2 behavior. Existing queued/running tasks retain
their original plan semantics; do not reinterpret them under a new resolver.
New tasks receive a frozen effective-plan snapshot.

Emit bounded OpenTelemetry metrics/events for resolver decisions, escalation,
stage duration, queue delay, resource pressure, recovery, finding age, waiver
age, and manual override. Do not emit prompts, source, credentials, hidden test
content, or unrestricted tool output.

The dashboard must report profile effectiveness by repository: completion,
verification success, repair loops, escalation frequency/reasons, false-low and
false-high classifications identified by later evidence, human overrides, time,
and resource consumption. Use these measurements for explicit threshold review,
never silent self-modification.

## 27. Delivery milestones

Keep the repository runnable after each milestone. High-risk integrations
default off until prerequisites and policy are configured.

### Milestone 1 — contracts, data, configuration, and UI foundations

- Create the Increment 3 requirement-to-code/test/UI matrix.
- Add migrations and domain contracts.
- Register every new configuration namespace.
- Add dashboard routes, permissions, audit, and read-only invariant views.
- Add typed deployment/task/risk/isolation/model-purpose profile namespaces,
  built-in profile manifests, and effective-plan contracts.
- Implement the deterministic resolver in shadow/recommendation-only mode with
  reason codes and immutable plan snapshots.
- Preserve effective Increment 2 behavior by default.

### Milestone 2 — isolation, invariants, and emergency control

- Implement runtime profiles and fake-runner coverage.
- Add gVisor and Landlock prerequisite/probe logic without host mutation.
- Implement the TLA+/PlusCal workflow model and executable state properties.
- Implement emergency stop and race tests.
- Add bounded chaos profiles and rehearsal tests.

### Milestone 3 — trajectory evaluation and fuzzing

- Implement normalized observable trajectories and metrics.
- Add safe replay and run comparison.
- Add API/schema-aware and parser/corpus fuzzing.
- Promote minimized failures into regression tests.
- Complete trajectory/fuzz UI and evidence linkage.

### Milestone 4 — contracts and supply chain

- Implement OpenAPI, GraphQL, Protobuf, JSON Schema, and migration compatibility
  adapters.
- Implement dependency admission and VEX lifecycle.
- Add optional controlled Renovate intake with fake-forge integration.
- Complete compatibility/dependency UI, policies, evidence, and documentation.

### Milestone 5 — verification depth

- Add reproducible artifact verification and diffoscope reporting.
- Add structural rules, PHP Deptrac, and secondary Difftastic view.
- Add flakiness laboratory.
- Add Coyote/FsCheck adapters and cross-pack property/metamorphic primitives.
- Add optional patch minimization.

### Milestone 6 — MCP and standardized GenAI observability

- Implement the governed MCP registry/gateway using fakes first.
- Add schema fingerprints, drift/quarantine, policy interception, and
  authentication/network protections.
- Integrate emergency stop and trajectories.
- Implement version-pinned GenAI semantic-convention normalization and bounded
  dashboards.

### Milestone 7 — closed-world Model Selection Optimizer

- Implement visible-model inventory snapshots through the existing gateway.
- Enforce and test the closed-world invariant.
- Implement purpose profiles and candidate model configurations.
- Add staged evaluation, holdouts, repeated trials, deterministic scoring,
  confidence, Pareto analysis, and recommendation portfolios.
- Implement approval, activation, rollback, drift, and reassessment.
- Complete the Model Optimizer web experience and fake-model E2E suite.

### Milestone 8 — progressive operation and simplified control

- Ship Routine, Quality, High Assurance, Forensic, and Automatic task-assurance
  behavior using the shared effective-plan resolver.
- Calibrate shadow-mode escalation with deterministic fixtures, then enable
  policy-authorized automatic escalation and non-downgrade enforcement.
- Implement the single normal task flow, plan preview/timeline, unified
  findings inbox, and typed approval queue.
- Implement Basic/Expert configuration views, effective-value provenance,
  presets, templates, dry run, comparison, and rollback.
- Consolidate agent roles onto shared workers and add documented Docker Compose
  profiles without weakening security boundaries.
- Complete first-run setup, system doctor, recovery/checkpointing, resource
  scheduling explanations, and operational metrics.
- Prove migration without reinterpreting existing queued/running work.

### Milestone 9 — hardening and release readiness

- Complete E2E, security, accessibility, migration, backup/restore,
  upgrade/recovery, performance, soak, and chaos suites.
- Validate the constrained dual-socket/128-GB resource profile or documented
  simulation.
- Complete operator, administrator, contributor, API, security, evaluator,
  MCP, and troubleshooting documentation.

## 28. Testing requirements

### 28.1 Configuration and UI coverage

Extend the machine-readable configuration coverage inventory. CI must fail when
a configurable Increment 3 feature lacks:

- registry descriptor;
- typed dashboard control/status;
- API and CLI path;
- permission;
- validation/dry run;
- persistence/effective-value test;
- audit/redaction;
- revision/rollback;
- user documentation.

Exercise critical paths with browser E2E tests, including keyboard use, focus,
announcements, high contrast, and non-color-only status.

### 28.2 Unit, property, and model tests

Cover:

- trajectory normalization, grounding, metric calculation, and redaction;
- runtime-profile selection and non-downgrade;
- Landlock ABI handling and gVisor prerequisites;
- controller state-machine transitions and formal invariant mirrors;
- emergency-stop races and restart persistence;
- chaos scope restrictions and cleanup;
- corpus keys, seed replay, minimization, and resource caps;
- compatibility classification and baseline identity;
- dependency diffs, unknown/stale metadata, VEX expiry, and admission policy;
- reproducible-build manifests and comparison classification;
- structural rules, debt baselines, and parser failure;
- flake estimates, quarantine expiry, and no-retry-laundering;
- concurrency schedules/property seeds;
- patch-minimization strict reduction and result preservation;
- MCP fingerprints, scope, OAuth/token audience, SSRF, drift, and normalized
  untrusted results;
- GenAI telemetry schema/version/redaction;
- model inventory normalization and snapshot immutability;
- closed-world candidate enforcement;
- purpose/deployment eligibility;
- staged elimination;
- holdout isolation;
- quality lower-bound/confidence calculation;
- Pareto frontier and deterministic recommendation;
- activation, rollback, drift, and staleness;
- profile namespace separation, inheritance, aliases, and migration;
- effective-plan determinism, hashing, versioning, and stale-input handling;
- automatic selection, escalation reason codes, monotonic non-downgrade, and
  protected-action boundaries;
- Basic/Expert configuration equivalence and effective-value provenance;
- finding normalization, deduplication/grouping, waiver expiry, and approval
  scope isolation;
- worker capability/resource resolution, host reserve, fairness, checkpoint,
  and idempotent resume; and
- existing-task preservation across resolver/configuration upgrades.

Use property/state-machine tests for transitions, inventory changes, concurrent
stop/claim/publication, and optimizer scoring invariants.

### 28.3 Integration tests

Use:

- small local fake models with controllable quality, latency, malformed output,
  tool behavior, and memory simulation;
- fake model gateway endpoints that list changing model inventories;
- protocol-accurate fake OpenAI-style, Anthropic, Gemini, Bedrock, Ollama, and
  partially compatible endpoints;
- fake MCP stdio and HTTP servers, OAuth metadata, tools, resources, drift, and
  malicious descriptions;
- fake GitHub/GitLab plus local bare Git;
- PHP/intranet, .NET/Windows, R/statistics, and SBOM/FMEA fixtures;
- deterministic artifact builders with matching and mismatching outputs;
- fake Renovate proposals;
- malformed API/schema/config/archive/webhook/provider/MCP inputs;
- simulated gVisor/Landlock prerequisite states when unavailable in CI;
- fake Windows worker plus optional real disposable worker validation;
- deterministic task classifiers and risk signals for every escalation class;
- fake capability/resource snapshots with missing, changing, and conflicting
  availability;
- legacy Increment 2 profile/configuration fixtures and queued/running tasks;
  and
- fake related findings from agents, tests, policy, security, documentation,
  dependencies, and reproducibility tools.

No real model credentials, large weights, external repositories, signing
services, VPNs, or physical instruments are required in CI.

### 28.4 End-to-end acceptance flows

At minimum prove:

1. inspect and replay an observable fake-agent trajectory without hidden
   reasoning or external side effects;
2. detect a fabricated claim after an empty tool result;
3. route a high-risk Linux job to required gVisor and fail closed when the
   prerequisite is absent;
4. activate emergency stop during a job and prove no new provider, runner,
   MCP, or publication action starts;
5. restart during commit/publication and prove idempotent recovery without a
   duplicate forge artifact;
6. inject representative provider, runner, database, disk, and MCP faults and
   preserve invariants/evidence;
7. fuzz the controller API and a parser, minimize a failure, and create a
   regression fixture;
8. detect and gate a breaking OpenAPI/GraphQL/Protobuf fixture change;
9. add a vulnerable or policy-forbidden dependency and block it;
10. approve a scoped VEX disposition, then prove expiry reopens the finding;
11. ingest a fake Renovate proposal as a normal governed task;
12. build deterministic artifacts twice and pass reproducibility;
13. detect nondeterministic artifacts and produce a bounded recursive diff;
14. detect a new structural/Deptrac violation while preserving a legacy
    baseline;
15. expose Difftastic only as a secondary view and retain the raw Git diff;
16. identify a deliberately flaky test without converting retries into a pass;
17. replay a Coyote/property failure from its schedule/seed;
18. minimize a passing patch and prove the selected result remains fully
    verified, or safely retain the original on budget/failure;
19. register and approve a fake MCP server, invoke an allowed tool, and record
    policy/trajectory/evidence;
20. change the MCP schema/description and block it pending reapproval;
21. reject MCP token passthrough, SSRF metadata, unauthorized local command, and
    over-scoped tool use;
22. emit useful versioned GenAI telemetry without prompt/source/tool content;
23. inventory only models exposed by the configured fake gateway;
24. prove an Internet/catalog model not in that inventory cannot enter a
    candidate set or recommendation;
25. run optimizer mode “all visible eligible” and a manually selected subset;
26. evaluate fake candidates with different quality, reliability, time, cost,
    and memory, then reproduce the deterministic recommendation;
27. block a visible remote candidate from confidential fixtures when egress
    policy forbids it;
28. report insufficient evidence instead of forcing a winner;
29. approve and activate a recommendation, then roll back;
30. change a model digest/capability/inventory and mark the recommendation
    stale without silently replacing the active profile;
31. back up, migrate/restart, restore, and preserve all new provenance;
32. submit a low-risk fixture with repository, task, and confirmation only,
    then complete it through the single normal workflow;
33. resolve identical normalized inputs and capability snapshots to byte-stable
    effective plans with the same reason codes;
34. distinguish deployment, task-assurance, risk, isolation, and model-purpose
    profiles throughout API, UI, audit, and evidence;
35. allow Routine only for an eligible fixture while retaining required review,
    tests, documentation impact, evidence, and publication protection;
36. use `Automatic — Quality preferred` for a normal maintenance fixture and
    run independent QC/QA plus impacted verification;
37. touch a sensitive path/dependency/public contract and atomically escalate
    to High Assurance without losing evidence or weakening containment;
38. encounter an unknown/flaky failure, begin Forensic read-only, establish a
    reproducible cause, and transition to a governed repair profile;
39. request a weaker profile after mutation starts and prove automatic
    de-escalation and safety-policy override are rejected;
40. block a task when a required high-assurance capability is unavailable and
    display the omission, reason, remediation, and operator-validation boundary;
41. show Basic and Expert views resolving to the same effective configuration,
    including value provenance and a tested rollback;
42. deduplicate causally related tool/agent findings without losing raw
    evidence, then prove a waiver/approval applies only to its typed scope;
43. restart a checkpointed progressive run and resume without repeating a
    protected/non-idempotent action;
44. migrate legacy profile settings and preserve the semantics of existing
    queued/running tasks while new tasks use frozen effective plans;
45. demonstrate local-lean operation with no implicit remote fallback, then
    enable a fake hybrid endpoint and still enforce classification/egress
    policy; and
46. exercise first-run setup and system doctor using fake prerequisites, proving
    that diagnosis does not install models, alter host security, or weaken
    policy.

### 28.5 Performance and soak tests

Verify:

- controller/dashboard responsiveness during long fuzz, reproducibility, and
  model-evaluation jobs;
- resource scheduler serialization of large local candidates;
- configurable host reserve and no unsafe OOM combinations;
- bounded trajectory, corpus, telemetry, and comparison storage;
- early elimination reduces evaluation work without changing finalist ranking
  on deterministic fixtures;
- repeated trials and confidence computation remain resumable;
- remote cost/rate/concurrency budgets fail closed;
- MCP and provider timeouts do not leak jobs or credentials;
- emergency stop remains responsive under load;
- multi-project isolation and fairness survive restart and extended execution;
- the normal dashboard/control API remains responsive while shared workers run
  long High Assurance and Forensic jobs;
- common Routine and Quality fixtures avoid unneeded stages while producing the
  same or stronger required verification evidence than their baseline;
- plan resolution is bounded and deterministic under large configuration and
  capability inventories;
- automatic escalation does not duplicate stages, findings, or protected
  actions;
- local model load/unload and cache decisions stay inside memory/host-reserve
  limits; and
- checkpoint/recovery and quiet-health notification behavior survive an
  extended multi-project soak.

## 29. Documentation deliverables

Update or add:

- Increment 3 architecture and trust-boundary diagrams;
- observable trajectory contract, metrics, replay, and privacy guide;
- gVisor/Landlock runtime-profile setup, compatibility, and recovery guide;
- formal workflow model, invariants, bounds, and implementation mapping;
- chaos and emergency-stop operator runbook;
- fuzz target authoring, corpus, minimization, and triage guide;
- API/schema compatibility policy and waiver guide;
- dependency admission, OSV, Scorecard signals, VEX, and Renovate intake guide;
- reproducible-build and nondeterminism-triage guide;
- ast-grep, Deptrac, architecture baseline, and diff-view guide;
- flakiness, Coyote, FsCheck, and property/metamorphic testing guide;
- patch-minimization behavior and limitations;
- MCP registry, local/remote setup, OAuth, capabilities, drift, incident
  response, and security guide;
- GenAI semantic-convention and telemetry privacy reference;
- Model Selection Optimizer purpose-profile authoring guide;
- closed-world model inventory and candidate-selection explanation;
- evaluation-suite, holdout, scoring, confidence, Pareto, recommendation,
  approval, rollback, and drift guide;
- progressive operation architecture and terminology guide distinguishing
  deployment, task-assurance, risk, isolation, and model-purpose profiles;
- Routine, Quality, High Assurance, Forensic, Automatic, escalation,
  non-downgrade, and effective-plan reference;
- single task flow, plan preview, unified findings, approvals, and Basic/Expert
  dashboard guide;
- configuration layering, effective-value provenance, presets, repository
  templates, migration, comparison, and rollback guide;
- shared-worker/Compose-profile deployment, first-run setup, system doctor,
  checkpoint/recovery, retention, backup, and quiet-alerting runbook;
- profile calibration and effectiveness review guide, including shadow mode,
  false-low/false-high analysis, and explicit threshold change control;
- target-host resource tuning and overnight evaluation guide;
- complete dashboard, API/OpenAPI, configuration-schema, backup/restore,
  upgrade, and troubleshooting updates;
- limitations distinguishing CI fakes, bounded formal models, and
  operator-validated external integrations.

Documentation examples must match shipped schemas, tests, and UI. Screenshots
must use deterministic fake data and freshness checks.

## 30. Definition of done

Increment 3 is complete only when:

1. Increment 1 and Increment 2 supported tests/workflows still pass.
2. Existing durable data migrates with tested backup and recovery.
3. Every routine new feature is operable and configurable through the web
   interface with API/CLI parity.
4. Immutable safety invariants are visible, versioned, tested, and not
   disableable through ordinary configuration.
5. Trajectories contain observable actions/evidence but no hidden reasoning.
6. Replay cannot repeat an external side effect.
7. Trajectory evaluation detects wrong tools, invalid arguments, skipped gates,
   unsupported claims, and unsafe failure behavior.
8. Required gVisor/Landlock/Windows runtime policy fails closed rather than
   downgrading silently.
9. Agents receive no host/runtime socket, broad host UDS, privileged mode, or
   unrestricted network.
10. Formal workflow invariants pass bounded model checks and executable
    implementation tests; counterexamples are usable artifacts.
11. Emergency stop is persistent, audited, race-tested, and blocks every
    protected action type.
12. Chaos scenarios demonstrate safe recovery and publication idempotency.
13. Fuzzing produces versioned corpora, minimized reproductions, bounded
    resource use, and regression tests.
14. Breaking contracts cannot pass protected publication without evidence and
    approval/waiver.
15. New dependencies receive baseline-differential admission; unknown/stale
    metadata is not treated as clean.
16. VEX dispositions are evidenced, scoped, owned, reviewable, and expiring.
17. Renovate cannot merge/publish or execute unsafe repository commands by
    default.
18. Reproducible-build profiles compare independent builds and explain
    mismatches.
19. Structural and architecture rules detect new violations without hiding
    legacy debt or textual diffs.
20. Flaky tests remain visible; quarantine is governed and expiring.
21. .NET concurrency/property failures are reproducible from stored
    schedules/seeds using fakes in CI.
22. Patch minimization preserves the original and cannot weaken required
    verification.
23. MCP is optional, controller-intercepted, registered, pinned, scoped,
    fingerprinted, audited, and fail-closed on drift.
24. MCP authentication/network tests prevent token passthrough, confused
    deputy, SSRF, redirect, local-command, and capability-scope bypass.
25. GenAI telemetry is versioned, useful, bounded, and content-private by
    default.
26. The Model Selection Optimizer considers only the pinned visible-model
    inventory from configured gateway endpoints/manual registrations.
27. No external catalogue search or automatic model download/installation is
    reachable from optimizer code or UI.
28. A model absent from the experiment inventory cannot be evaluated,
    recommended, activated, or introduced by benchmark metadata.
29. Purpose profiles, candidate configurations, suites, holdouts, trials,
    scores, confidence, and recommendations are versioned and reproducible.
30. Deterministic verification dominates scoring; the ranking engine is
    deterministic and models cannot alter its result.
31. Recommendations report confidence, limitations, alternatives, rejected
    candidates, validity, and re-evaluation triggers.
32. Activation is authorized, evidenced, reversible, and does not occur
    automatically for protected workflows.
33. Remote visibility never overrides data-classification/egress policy.
34. Model identity/capability drift makes recommendations stale without
    silently changing active assignments.
35. Security, authorization, isolation, redaction, cross-project, injection,
    SSRF, archive, stale-edit, race, and recommendation-tampering tests pass.
36. Critical dashboard workflows pass accessibility and keyboard tests.
37. Backup/restore and upgrade/recovery preserve or rebuild all new state and
    provenance.
38. The stack remains startable through the documented Docker Compose workflow
    and useful on the CPU-only target without Kubernetes or cloud services.
39. Unavailable external integrations have protocol-accurate fakes and precise
    finite operator validation; none are falsely reported as tested.
40. Deployment, task-assurance, risk, runtime-isolation, and model-purpose
    profiles are distinct typed/versioned concepts in schema, code, UI, audit,
    and evidence.
41. `Automatic — Quality preferred` is the default, and a normal task can be
    started with repository, request, and confirmation after preflight.
42. Effective plans are deterministic for frozen inputs, hashed, immutable,
    explained, persisted before execution, and referenced by run evidence.
43. Routine never bypasses required review, testing, documentation impact,
    containment, evidence, approval, or publication protection.
44. Escalation is atomic, reason-coded, audited, monotonic after mutation, and
    cannot weaken a completed or mandatory gate.
45. Sensitive/dependency/API/schema/security/release fixtures select or
    escalate to High Assurance and fail visibly when a required capability is
    unavailable.
46. Forensic begins read-only, records hypotheses/evidence, and transitions to
    a governed repair profile only after its cause/evidence gate.
47. The normal UI provides one task flow, effective-plan preview/timeline,
    patch/evidence review, findings inbox, and approval queue.
48. Findings are normalized and deduplicated/grouped without deleting raw
    evidence; waivers and approvals are typed, scoped, authorized, audited, and
    expiring where applicable.
49. Basic and Expert configuration views use the same schemas/services and
    expose effective-value provenance, validation, dry run, revision, and
    rollback for every mutable operational setting.
50. No mutable profile or operational behavior exists only in an environment
    variable, hidden API, raw file, or undocumented option.
51. Agent roles execute on shared workers; the system does not require a
    container, service, public API, queue, or database per agent or profile.
52. The normal installation remains one documented Compose command; optional
    operational groups use validated Compose profiles with visible health and
    dependency state.
53. Local-lean operation is complete without cloud services or implicit remote
    fallback; hybrid operation remains explicit and policy/egress controlled.
54. CPU/RAM/IO budgets, host reserve, large-model serialization, fairness, and
    actionable queue explanations are validated on the target profile or a
    documented constrained simulation.
55. Checkpoint/restart recovery does not repeat protected or non-idempotent
    actions and preserves the accepted/final effective plans.
56. First-run setup and system doctor diagnose prerequisites without silently
    installing models, changing host security, enabling egress, or weakening
    policy.
57. Migration preserves legacy settings and the semantics of already
    queued/running tasks; only new tasks use the new frozen resolver result.
58. Profile-effectiveness metrics support explicit human calibration and never
    cause silent self-modification of thresholds or policy.

## 31. Codex operating instructions

When implementing:

1. Read both inherited goal files, repository guidance, architecture,
   migrations, tests, and working-tree state first.
2. Create and maintain a requirement-to-implementation-test-UI-documentation
   matrix.
3. Reconcile this logical design with the actual repository; do not duplicate a
   working service merely to match an illustrative name.
4. Preserve unrelated operator changes and never use destructive Git commands.
5. Implement milestone by milestone with small reviewable changes and a
   runnable repository after every milestone.
6. Treat migrations, UI, API, permissions, audit, rollback, tests, and
   documentation as part of each feature.
7. Prefer pinned maintained dependencies and document licenses, security
   implications, update method, and offline behavior.
8. Use deterministic fake models, model inventories, providers, MCP servers,
   forges, Renovate proposals, Windows workers, instruments, and build artifacts
   in CI.
9. Never download large weights or contact real model providers, MCP servers,
   external repositories, signing services, VPNs, or physical instruments
   without a separate explicit operator instruction.
10. Never expand the Model Selection Optimizer candidate universe beyond models
    returned by the configured model gateway snapshot.
11. Run narrow tests during development and the full inherited/new required
    suites before handoff.
12. Measure and report resource behavior on the target profile or documented
    constrained simulation.
13. Report commands, tests, migrations, security decisions, measured results,
    known limitations, and operator-only validation.
14. If a requirement cannot be implemented safely in the actual architecture,
    stop at the safe boundary, document the blocker and evidence, and do not
    substitute an insecure approximation or silently remove the requirement.
15. Implement progressive profiles as declarative resolver inputs over shared
    stages; do not copy the orchestration pipeline for each profile.
16. Keep deployment, task-assurance, risk, runtime-isolation, and model-purpose
    terminology/types separate in implementation and documentation.
17. Preserve safety monotonicity: profiles may add assurance, but neither user
    convenience nor resource pressure may remove a policy-required gate.
18. Demonstrate the default three-input workflow and the complete Expert
    controls with automated browser tests before declaring operational
    simplification complete.
