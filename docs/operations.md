# Operations and updates

Use `maintainctl doctor` for authenticated component/host diagnostics, `/healthz` for liveness, `/readyz` for storage readiness, and `/metrics` for Prometheus-format uptime, jobs, phase completions, findings, and model gauges. Logs are structured JSON; correlate job phases using durable job IDs and artifact metadata.

Start the optional metrics collector with:

```sh
docker compose -f compose.yaml -f compose.observability.yaml --profile observability up -d prometheus
```

Prometheus has no published port and scrapes the controller on the internal control network. Attach an operator-owned Grafana/Loki stack to that network only after applying the same digest, non-root, read-only, capability, and no-host-port requirements.

## Update workflow

Run `scripts/update.sh preflight` from a clean committed tree. It validates Compose, runs Go tests/vet, frontend accessibility/build/API drift, the full local acceptance, vulnerability checks, and SBOM generation, then records the exact source SHA. Immediately before apply, run `maintainctl reauthenticate --password-file <private-file>`. `scripts/update.sh apply` refuses a different SHA, creates and dry-runs a fresh backup, tags every running application image as known-good, builds and promotes the candidate, and checks health. `scripts/update.sh rollback` restores those known-good image tags and health-checks the stack; if the release crossed an incompatible schema boundary, stage the validated pre-update backup and restart before resuming work. Retain old images and the pre-restore database until the observation window closes.

Never update via mutable tags. Re-resolve digests, review upstream release/security notes, regenerate SBOM/provenance, and repeat the complete gate.
