# Troubleshooting

- `permission denied ... docker.sock`: use the configured rootless daemon or the explicitly authorized development `sudo docker` path. Never mount the general socket into the controller.
- controller unhealthy before bootstrap: health checks must use public `/healthz`; `doctor` is intentionally authenticated.
- project not registered: run `repo add`, confirm exact provider/default branch, then `repo sync` and diagnostics.
- publication blocked: inspect open blocker/must-fix findings, exact result SHA, recent reviewer reauthentication, upstream movement, and draft idempotency record.
- model unavailable: validate manifest schema, filename confinement, declared bytes/SHA-256, role/family compatibility, RAM, and supervisor token.
- memory search empty: confirm exact project namespace, canonical status, index health, then run scoped smoke-index/reindex.
- restore rejected: use the matching encryption key, check file mode, run dry-run, and confirm schema version. Never edit a bundle.
- tail logs are bounded: inspect immutable job artifacts for complete verifier/QC reports rather than raising limits arbitrarily.

Run `docker compose config --quiet`, `maintainctl health`, `maintainctl doctor`, and `scripts/acceptance.sh` in that order to separate deployment, liveness, authenticated dependency, and workflow failures.
