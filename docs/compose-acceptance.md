# Compose and acceptance runbook

CodeMaintainer supports a containerized development and acceptance path because host Go and npm installations are optional. The checked Compose and acceptance evidence is split between CI-safe gates, local live mock-stack acceptance, and operator-only production checks.

## CI-safe gate

GitHub Actions runs the CI-safe gate on `dev` and pull requests:

- Go version comes from `go.mod` through `actions/setup-go` with `go-version-file: go.mod`.
- Go formatting uses `gofmt -l cmd internal`.
- Go tests run with `go test -race ./...`.
- Static analysis runs with `go vet ./...`.
- Frontend validation runs `npm ci && npm run check:api && npm test && npm run build && npx redocly lint ../internal/api/openapi.yaml`.
- Compose validation runs `docker compose -f compose.yaml -f compose.dev.yaml config --quiet`.
- The image build gate builds `controller`, `maintainctl`, `runnerd`, `model-supervisor`, and `code-intelligence`.

This gate does not use real forge credentials, Windows hosts, external providers, or large model weights.

## Local live acceptance

Run:

```sh
./scripts/acceptance.sh
```

The script bootstraps required local directories/secrets, validates Go and frontend checks inside tool containers, validates the dev Compose graph, builds control-plane images, starts `runnerd` and `controller`, checks `http://127.0.0.1:8080/healthz`, ensures a valid local acceptance administrator session for fresh mock data, records recent reauthentication for the browser publication gate, runs `maintainctl doctor`, and executes the browser acceptance container. Set `MAINTAINER_ACCEPTANCE_PORT=18080` to include `compose.acceptance.yaml`, replace the host port mapping, and run the same browser gate beside an existing retained appliance. When Docker access is mediated by `sudo`, the wrapper passes this port through `sudo env ... docker compose` so the probed base URL and published Compose port remain aligned.

The browser gate onboards local bare-Git fixture repositories, runs Repo Doctor against heterogeneous PHP/R fixtures, accepts a detected capability-pack proposal, reviews effective pack configuration, exercises the typed configuration draft/check/review/apply/rollback path, and then drives the Models and agents provider workbench. The provider workflow probes the fake remote model, enables the fake remote provider, approves a remote documentation route after retained egress preview review, queues and advances a provider-backed job, approves each retained per-job remote egress manifest before remote execution, and verifies the completed job retained remote documentation provider execution evidence without contacting the network.

If port `127.0.0.1:8080` is already owned by another retained appliance, use an isolated checkout/data root with `MAINTAINER_ACCEPTANCE_PORT=18080` before claiming a full live acceptance result. Do not mark this gate passing from CI alone.

## Compose views to validate

The deterministic documented views are:

```sh
docker compose -f compose.yaml -f compose.dev.yaml config --quiet
docker compose --profile tools config --quiet
docker compose -f compose.yaml -f compose.observability.yaml --profile observability config --quiet
MAINTAINER_REMOTE_HOST=maintainer.example.invalid \
OIDC_GATEWAY_URL=https://auth.example.invalid/verify \
MAINTAINER_TLS_CERT=/absolute/path/to/cert.pem \
MAINTAINER_TLS_KEY=/absolute/path/to/key.pem \
  docker compose -f compose.yaml -f compose.remote.yaml --profile remote config --quiet
MAINTAINER_DATA_ROOT=/absolute/path/to/data \
MODEL_MANIFEST_ROOT=/absolute/path/to/model-manifests \
RUNNERD_POLICY_FILE=/absolute/path/to/runner-policy.json \
RUNNERD_WORKER_SOCKET=/absolute/path/to/dedicated-worker.sock \
  docker compose -f compose.yaml -f compose.production.yaml config --quiet
```

Production startup also requires operator-owned host prerequisites such as the dedicated rootless worker daemon, production backup key, mounted model manifests, and TLS/forward-auth gateway described in `docs/deployment.md` and `docs/acceptance.md`.

## Maintenance scripts

- `./scripts/agent-image-acceptance.sh` proves the separate implementation and QC images with deterministic fake agent artifacts.
- `./scripts/vulnerability-scan.sh` runs `govulncheck` and npm audit and records when an OCI scanner is unavailable.
- `./scripts/sbom.sh` records Go modules, npm packages, and resolved container images.
- `./scripts/update.sh preflight|apply|rollback` documents the operator promotion flow and keeps rollback image tags and backups explicit.
