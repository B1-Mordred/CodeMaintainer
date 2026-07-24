# Evidence traceability graph

Increment 2 stores a controller-owned typed evidence graph so operators can trace
retained facts without relying on scattered raw JSON blobs. The first graph
checkpoint records immutable job and artifact nodes plus `produced` edges when
the artifact index commits.

The graph is intentionally append-only:

- `evidence_nodes` records the job, project, typed subject, subject hash,
  producer, bounded label, metadata JSON, metadata hash, and creation time.
- `evidence_edges` records typed relationships between nodes for the same job
  and project. Cross-job edges are rejected by the storage service.
- SQLite triggers reject updates and deletes for both graph tables.
- Artifact indexing writes the artifact row, job node, artifact node, produced
  edge, and audit event in one transaction.

Operator access:

- Web: open a job in **Jobs** and inspect **Evidence traceability graph**.
- CLI: `maintainctl evidence <job-id>`.
- API: `GET /api/v1/jobs/{jobID}/evidence-graph`.

This checkpoint proves durable typed artifact provenance. The final
request-through-publication/SBOM graph remains broader than this slice: later
work must attach task contracts, risk decisions, policy decisions, verification,
documentation, QC, publication, SBOM, and support-bundle evidence to the same
graph model.
