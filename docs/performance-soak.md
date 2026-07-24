# Performance and bounded soak evidence

Increment 2 targets a CPU-only, single-host appliance. Performance evidence is therefore split into deterministic CI-safe smoke evidence and operator or final-acceptance soak evidence that must run after all feature rows are otherwise closed.

## Deterministic local evidence

The current deterministic gate proves these bounded performance controls without external credentials, proprietary hardware, Windows licensing, or model weights:

- Scheduler simulations and live lease acquisition retain decisions for `quality_latency` and `throughput_batching`, including CPU, memory, maintenance-window, fairness, deadline, and model-batch reasoning.
- Live leases reject same-project unfair co-residence and candidates that would exceed memory after active leases plus controller reservation.
- Runtime benchmarks retain prompt/decode timing, duration, memory, exact runtime identity, quality fixture result, determinism result, cache eligibility, experimental gates, and a non-activating recommendation.
- Historical evaluation records budget, concurrency, task/test quality, regression rate, diff churn, context tokens/bytes, wall time, CPU time, memory, cache effect, documentation compliance, interventions, uncertainty, reproducibility hash, and review-only promotion status.
- Observability records bounded local duration, queue time, retry count, resource bytes, redaction counts, and support-bundle manifests.
- The deterministic acceptance script runs Go, frontend, OpenAPI, Compose, live mock health, and browser checks without fetching model weights or contacting real forges/providers.

## Remaining soak closure

No current CI job claims a long-duration soak pass. Final closure still needs a deterministic bounded soak runner and retained JSON report with explicit pass/fail thresholds for:

- queue latency and live scheduler decision stability under repeated local-fake jobs;
- memory growth across repeated indexing, workflow, evidence, and support-bundle operations;
- scheduler fairness and co-residence behavior across mixed project workloads;
- provider gateway retry, circuit-breaker, capability-drift, and cost-budget behavior once outbound fake execution is integrated;
- support-bundle size and telemetry redaction counts over repeated runs;
- local-fake workflow throughput and phase-duration distribution;
- restart/resume behavior during or after the soak window.

The soak runner must remain bounded: fixed fixture repositories, fixed fake providers, no external network dependency, no real credentials, no large model weights, finite duration, finite artifact sizes, and deterministic failure reports. Operator-only production benchmarking, such as physical NUMA comparisons with licensed GGUF weights, remains documented separately in `docs/acceptance.md`.
