# Quality Workflow

Increment 2 Milestone 5 starts by making implementation wait for a controller-owned task contract and deterministic risk decision.

## Task contracts

Every job now passes through `locking_acceptance_criteria` and then pauses at `awaiting_task_approval`. During that phase the controller creates a versioned draft contract from the submitted task. The contract records requested behavior, non-goals, affected users, acceptance criteria, constraints, likely components and risks, required evidence, required documentation, assumptions, questions, and a machine-readable completion checklist.

Operators can inspect and approve the contract through the Jobs page or the CLI:

```sh
maintainctl contract get <job-id>
maintainctl contract update <job-id> --version <n> --reason <text> --file <contract.json>
maintainctl contract approve <job-id> --version <n> --reason <text>
```

Approval is optimistic and exact-versioned. The approval transaction stores the accepted criteria hash on the job and advances the workflow to `loading_implementation_model`. Implementation does not run while the job is in `awaiting_task_approval`.

## Risk routing

The first risk assessment is computed from deterministic controller signals bound to the task contract. Signals cover protected paths, public API/schema/database changes, authentication/secrets/cryptography/network exposure, dependency and supply-chain changes, installer/service/signing/hardware behavior, migration/data-loss potential, numerical/statistical behavior, weak or missing tests, generated or binary artifacts, and task ambiguity.

Risk levels route required stages and manual gates:

- `low`: clarifier, implementation, targeted/full/final verification, and code QC.
- `medium` and `high`: also require independent Test Designer, golden/rehearsal review, Documentation Agent, and deterministic policy review before QC. Later slices still add full documentation QC actions.
- `high`: additionally records publication reauthentication as a manual gate.

Risk history is append-only. Lowering risk requires a reviewer or administrator to reauthenticate, provide a reason, and set a future expiry:

```sh
maintainctl risk get <job-id>
maintainctl reauthenticate --password-file <file|->
maintainctl risk waive <job-id> --assessment <id> --to <low|medium> --reason <text> --expires-at <RFC3339>
```

The API derives reauthentication from the session. It does not trust caller-supplied `reauthenticated` fields.

## Agent contract registry

Agent-controller packets and structured model outputs are registered as versioned JSON Schema contracts. The built-in catalogue currently covers task packets, implementation results, QC reports, Test Designer proposals, documentation manifests, risk assessments, and completion summaries. The controller advertises strict JSON Schema response formats to compatible model runtimes and still validates decoded output with bounded Go contracts before accepting it.

Model-backed implementation, QC, Test Designer, and Documentation Agent workers now use the registered retry policy for malformed structured output. A rejected completion is not applied or stored as trusted evidence; the trusted runner sends bounded controller validation feedback back to the model and retries up to the descriptor limit, currently three attempts. Exhaustion fails the phase according to workflow policy instead of accepting unsupported claims, extra fields, wrong job bindings, unsafe edits, or malformed JSON.

Operators can inspect the active catalogue and job-level validation evidence:

```sh
maintainctl agent-contracts
maintainctl agent-contracts <job-id>
```

Each validation record stores the phase, contract kind, schema version, schema hash, payload hash, attempt, validity, bounded error text, and artifact reference when present. The ledger is append-only and is also visible in the Jobs page under “Structured output contracts.” This provides the evidence base for Test Designer, Documentation Agent, and OPA gates.

## Independent Test Designer

After full candidate verification, medium- and high-risk jobs route through `loading_test_designer_model` and `test_design_review` before normal QC. Low-risk jobs continue directly to QC; if a Test Designer phase is explicitly seeded for low risk, the controller stores a bounded `skipped` report instead of invoking a model.

The Test Designer receives a fresh controller-built packet containing the approved contract, authoritative context, baseline/differential/test-impact evidence, and candidate diff. It does not receive the implementation agent's narrative. The report must match the registered `test_proposal` schema and is validated against the exact job, approved contract hash, risk level, and result commit before storage.

Reports are append-only and include proposed missing tests, boundary cases, regression risks, suitable golden/rehearsal checks, evidence IDs, and dispositions. Required unresolved proposals pause the job in `awaiting_test_design_disposition` before golden, documentation, policy, or QC stages. Reviewer dispositions are a separate append-only ledger; the original report is never mutated, and the effective report view overlays the latest accepted/rejected/not-applicable decision plus the recorded reason.

Operators can inspect reports through the Jobs page or CLI:

```sh
maintainctl test-designer <job-id>
maintainctl test-designer dispose <report-id> <proposal-id> --disposition accepted|rejected|not_applicable --reason <text>
```

## Golden and rehearsal gates

After Test Designer review, or after full verification for low-risk jobs that have registered rehearsals from assigned capability packs, the workflow records a `golden_rehearsal_review` phase before QC. The phase selects only controller-registered rehearsal definitions from trusted capability manifests and cross-links matching Test Designer suggestions; it does not execute pack-supplied commands or accept browser-supplied runner inputs.

The report stores candidate-vs-approved artifact identities, comparison class, tolerance and mask policy metadata, provenance, diff summary, and status. A matched report advances to QC. A job with no registered rehearsals records `no_rehearsals` evidence. A changed or missing approved golden stores `approval_required` and routes the job to `awaiting_golden_approval`; it never updates the approved artifact automatically.

Golden update approvals are append-only records with actor, role, reason, recent reauthentication, and the exact approved/candidate artifact hashes. The effective decision for each comparison is the latest append-only approval record. Rejections keep the comparison unresolved until a later explicit approval supersedes them; when all required comparisons are approved, the API advances a waiting job to `loading_documentation_model`.

Operators can inspect reports and append an approval or rejection through the Jobs page/API or CLI:

```sh
maintainctl golden reports <job-id>
maintainctl reauthenticate --password-file <file|->
maintainctl golden approve <report-id> <comparison-id> --reason <text>
maintainctl golden reject <report-id> <comparison-id> --reason <text>
```

## Documentation Agent

After full verification and golden/rehearsal evidence, the workflow routes through `loading_documentation_model` and `documentation_review` before final QC. The Documentation Agent receives a fresh bounded packet with the approved task contract, deterministic risk level, candidate diff, verification evidence, context-manifest selections, Test Designer reports, and golden/rehearsal reports. Its output is validated against the registered `documentation_manifest` contract and exact job, project, contract hash, risk level, and result commit before storage.

Documentation manifests are append-only. They record policy-selected required documents, source-of-truth mappings, source-controlled changes, render/check status, unsupported claims, and bounded edit declarations. If the agent creates or updates documentation, the controller commits those source-controlled changes, updates the job `result_sha`, records fresh test-impact evidence, and runs a fresh full verification differential for the documentation commit before QC can inspect it. If the manifest contains unsupported claims or declares changes without an exact documentation commit, the phase fails closed.

The current policy foundation is controller-owned and deterministic. It establishes the durable Documentation Agent stage, schema validation, runner profile, exact-commit re-verification, API/CLI/browser visibility, and mock-profile behavior. Later slices still need the full declarative policy builder, renderer/tool profile management, documentation-specific finding lifecycle, screenshot/golden management, rich render previews, and independent documentation QC actions.

Operators can inspect manifests through the Jobs page or CLI:

```sh
maintainctl documentation <job-id>
```

## OPA policy lifecycle foundation

The controller now retains a deterministic policy bundle ledger and inserts `policy_review` after `documentation_review` and before `loading_qc_model`. The built-in structured bundle is the safe default for Increment 2 quality gates. It records required stages by risk level, protected action classes, allowed runner/network/model trust profiles, publication requirements, memory-promotion requirements, and waiver requirements.

Policy review evaluates the active bundle against job, risk, result commit, golden/rehearsal, and documentation evidence. The decision is append-only and includes bundle ID/version, decision point, redacted input, input hash, allow/deny outcome, required stages, explanation, and whether the decision failed closed. A denied decision stops QC rather than routing around the gate.

Advanced policy bundles use the embedded `github.com/open-policy-agent/opa` library pinned in `go.mod`. The controller formats Rego source before storing it, records both source and formatted-source hashes, runs retained policy tests before activation, and requires the latest retained test run for the exact formatted source to pass before an advanced bundle can become active. OPA evaluation failures produce fail-closed decisions rather than bypassing the policy gate.

Operators can inspect and exercise the lifecycle through the CLI:

```sh
maintainctl policy bundles
maintainctl policy activations
maintainctl policy test-runs [--bundle <id>]
maintainctl policy decisions <job-id>
maintainctl policy simulate --decision qc_requirement --input <json-file|-> [--bundle <id>]
maintainctl policy test <bundle-id> --tests <json-file|->
maintainctl policy create-advanced --version <version> --reason <text> --rego <rego-file> --tests <json-file|->
maintainctl reauthenticate --password-file <file|->
maintainctl policy activate <bundle-id> --reason <text> --staged-rollout-percent 100
```

The API exposes the same bundle, activation, simulation, test-run, and job-decision records. Activation and advanced Rego authoring require recent reauthentication when authentication is enabled. Simulation input is bounded JSON and stored only after secret-like keys are redacted. The Jobs page shows the latest policy decision and full decision history for inspected jobs. The Policy page exposes bundle source/structured interpretation, retained tests and coverage, simulations, activation history, and an expert-only advanced Rego editor.

This foundation exposes advanced Rego authoring only behind the expert UI and controller-side administrator/reauthentication checks. Structured template editing, richer staged-rollout and rollback workbenches, and broad protected-action enforcement across memory, provider, forge, signing, Windows, and hardware actions remain open work under I2-20.

## Remaining Milestone 5 work

This slice does not complete Milestone 5. Still pending are editable tolerance/mask management and rendered visual diffs for golden reports, the complete declarative documentation-policy/toolchain/QC workbench, structured policy editing, richer rollback/staged-rollout operations, and cross-domain protected-action enforcement.
