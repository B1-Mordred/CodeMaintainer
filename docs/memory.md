# Project memory

Every record is scoped first to `viking://resources/projects/<owner>/<repository>/`. Namespace filtering occurs before semantic ranking. Retrieval is token bounded, records candidate and selected IDs plus trajectory, and injects only the selected context packet.

New records and workflow extractions enter quarantine after secret scanning. Promotion requires deterministic verification, a merged PR, or explicit human approval. Failed cases are sanitized. Corrections are optimistic/versioned; invalidation and deletion append lifecycle events, and deletion clears content. Merge webhooks/polling promote exact repository/branch/PR/result bindings; rejection or closure makes candidates stale.

Use the dashboard to browse/search and `POST .../reindex` for vector rebuilds. The smoke-index action must complete health/write/scoped-search/delete within 15 seconds. Export bundles are manifest hashed, schema versioned, same-project only, and restore always re-quarantines imported data.

Memory is part of the checked redaction boundary in `config/redaction-coverage.json`. The durable store rejects credential-like content, traversal paths, invalid commits, and token-bearing source URIs before records can become retrievable evidence.
