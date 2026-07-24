# Historical evaluation lab

The Evaluation page and `maintainctl evaluation` commands provide the first
Increment 2 historical patch evaluation surface. This checkpoint is an offline,
deterministic simulator. It proves the durable data model, reproducible report
identity, memory/cache namespace isolation, and review-only recommendation
boundary without contacting external providers or replaying real repository
history yet.

## Data model

Migration 39 adds append-only tables:

- `evaluation_datasets` records immutable project-bound datasets with source
  kind, repository, base/target revisions, exclusions, scoring profile,
  retention, hidden known-patch identity, reproducibility key, metadata, actor,
  and creation time.
- `evaluation_runs` records immutable run reports with dataset/project identity,
  profile matrix, `eval://memory/...` namespace, `eval://cache/...` namespace,
  budget, concurrency, profile metrics, report hash, review-only promotion
  recommendation, actor, and creation time.

SQLite triggers reject updates and deletes for both tables. The storage service
also validates that evaluation runs never use project memory namespaces such as
`viking://resources/projects/...`.

## Operator surfaces

REST:

- `GET /api/v1/evaluations/datasets`
- `POST /api/v1/evaluations/datasets`
- `GET /api/v1/evaluations/runs`
- `POST /api/v1/evaluations/runs`

CLI:

- `maintainctl evaluation datasets [--project <project-id>]`
- `maintainctl evaluation create-dataset --input <json-file|->`
- `maintainctl evaluation runs [--dataset <dataset-id>]`
- `maintainctl evaluation launch --input <json-file|->`

Web:

- Evaluation page: create historical/curated datasets, launch isolated offline
  comparison runs, inspect report hashes, namespace identities, and profile
  metrics.

## Current limits

The simulator does not yet execute real historical repairs, collect real wall
clock/resource telemetry, or compare actual candidate patches to hidden known
patches. All promotion output is intentionally `review_only`; no model, policy,
cache, route, or runtime profile can be activated by an evaluation result.
