# Local Code Maintainer

Local Code Maintainer is a self-hosted, browser-operated appliance for producing minimal, independently reviewed, deterministically verified maintenance patches across multiple repositories. It is designed for a CPU-only Linux host, works without cloud LLMs, and includes a complete mock profile so development and acceptance do not require GitHub credentials or large model weights.

> Implementation is in progress. The current foundation provides the durable workflow state machine, SQLite audit storage, controller API and OpenAPI contract, event stream, CLI, narrow fake adapters, and a statically built React/TypeScript dashboard shell. See `execplan/complete-system.md` for exact progress and unverified milestones.

## Quick start

The supported toolchain runs in Docker; Go and npm are not required on the host.

```sh
docker compose build
docker compose run --rm go-tool go test ./...
docker compose up --build
```

Open <http://127.0.0.1:8080>. The default development configuration binds only to localhost and uses fake integrations. Do not expose this profile directly to a network.

To use the CLI without installing Go:

```sh
docker compose run --rm maintainctl doctor
docker compose run --rm maintainctl status
```

Production rootless deployment, real models, GitHub App setup, memory, backup/restore, and upgrades will be documented as their implementation milestones land. `project.md` is the authoritative product and security contract.
