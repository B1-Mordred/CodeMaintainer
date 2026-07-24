# Resource scheduler

The resource scheduler is currently an advisory controller surface. It evaluates queued work against a bounded host topology and safe resource profiles, records the decision and reason append-only, and exposes the result through the API, CLI, and operator console. It does not yet replace the live durable lease dispatcher.

## Inputs

Scheduler simulations accept:

- `mode`: `quality_latency` or `throughput_batching`.
- `topology`: host CPU, memory reservation, I/O pressure, and thermal state. The API fills a deterministic local profile when omitted.
- `profiles`: bounded CPU, memory, model, runner, and network envelopes. The API fills the safe default profiles when omitted.
- `active`: currently running workloads.
- `queued`: candidate queued workloads.
- `maintenance_window`: false defers all queued work.
- `fairness_window`: when greater than zero, same-project queued work is deferred while that project already has active work.

The default profiles cover controller reservation, offline verification, implementation inference, QC inference, documentation inference, and test-designer inference. They intentionally describe fixed envelopes rather than arbitrary commands, images, mounts, paths, networks, or model arguments.

## Decisions

Each simulation produces and retains a `scheduler_decisions` row with:

- selected job/project/profile when a workload is safe to schedule;
- rejected and deferred job IDs;
- co-residence safety status;
- whether fairness affected the result;
- model batch group when throughput batching applies;
- bounded human-readable resource summary and reason.

Retained decisions are append-only. Simulations may use hypothetical job IDs, so decision rows do not foreign-key selected jobs.

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

This checkpoint proves safe co-residence rejection, fairness-aware selection, throughput model grouping, durable decision history, and API/CLI/UI parity. The next scheduler step is to bind the live queue lease path to these decisions so unsafe combinations cannot be leased outside simulation.
