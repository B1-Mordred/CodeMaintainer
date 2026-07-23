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
- `medium` and `high`: also require independent Test Designer, Documentation Agent, documentation QC, and OPA policy stages once the remaining Milestone 5 slices are implemented.
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

Operators can inspect the active catalogue and job-level validation evidence:

```sh
maintainctl agent-contracts
maintainctl agent-contracts <job-id>
```

Each validation record stores the phase, contract kind, schema version, schema hash, payload hash, attempt, validity, bounded error text, and artifact reference when present. The ledger is append-only and is also visible in the Jobs page under “Structured output contracts.” This provides the evidence base for retry policy and later Test Designer, Documentation Agent, and OPA gates.

## Remaining Milestone 5 work

This slice does not complete Milestone 5. Still pending are full validation-error retry orchestration, independent Test Designer execution and dispositions, policy-controlled golden/rehearsal artifacts, Documentation Agent and documentation QC, and OPA-backed policy simulation/activation/rollback.
