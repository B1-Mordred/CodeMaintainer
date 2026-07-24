# Resource scheduler

The resource scheduler evaluates queued work against a bounded host topology and safe resource profiles, records the decision and reason append-only, exposes simulation through the API/CLI/operator console, and now gates durable live job lease acquisition. The controller remains the sole workflow-state authority.

## Inputs

Scheduler simulations accept:

- `mode`: `quality_latency` or `throughput_batching`.
- `topology`: host CPU, memory reservation, I/O pressure, and thermal state. The API fills a deterministic local profile when omitted.
- `profiles`: bounded CPU, memory, model, runner, and network envelopes. The API fills the safe default profiles when omitted.
- `active`: currently running workloads.
- `queued`: candidate queued workloads.
- `maintenance_window`: false defers all queued work.
- `fairness_window`: when greater than zero, same-project queued work is deferred while that project already has active work.

The default profiles cover controller reservation, controller workflow phases, dependency preparation, offline verification, implementation inference, QC inference, documentation inference, and test-designer inference. They intentionally describe fixed envelopes rather than arbitrary commands, images, mounts, paths, networks, or model arguments.

Live lease acquisition derives profile IDs from durable workflow state:

- controller-only phases use `controller_workflow`;
- dependency preparation uses `dependency_preparation`;
- reproduction, verification, final verification, and golden rehearsal use `verification_offline`;
- implementation and repair use `implementation_inference`;
- Test Designer, Documentation Agent, and QC phases use their matching inference profiles.

## Decisions

Each simulation or live lease attempt with candidates produces and retains a `scheduler_decisions` row with:

- selected job/project/profile when a workload is safe to schedule;
- rejected and deferred job IDs;
- co-residence safety status;
- whether fairness affected the result;
- model batch group when throughput batching applies;
- bounded human-readable resource summary and reason.

Retained decisions are append-only. Simulations may use hypothetical job IDs, so decision rows do not foreign-key selected jobs.

For live leases, the decision and `job_leases` write occur in the same SQLite transaction. A worker receives a lease only if the scheduler selected that exact job and the lease write won the expiry/ownership check. Same-project concurrent candidates are deferred by the fairness window, and candidates that would exceed available CPU or memory after active leases plus controller reservation are not leased.

## Operator surfaces

API:

- `GET /api/v1/scheduler/status`
- `POST /api/v1/scheduler/simulations`
- `GET /api/v1/scheduler/decisions`

CLI:

- `maintainctl scheduler status`
- `maintainctl scheduler decisions`
- `maintainctl scheduler simulate --input <json-file|->`

Browser:

- Scheduling / Hermes → Resource scheduler

## Current boundary

This checkpoint proves safe co-residence rejection, fairness-aware live lease selection, throughput model grouping in simulations, durable decision history, and API/CLI/UI parity. Remaining scheduler work includes richer NUMA/disk/I/O/thermal/provider-health inputs, configurable limits and batching windows, benchmark-derived recommendations, and provider-aware routing.
