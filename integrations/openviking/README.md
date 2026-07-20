# OpenViking integration boundary

The optional `memory` Compose profile runs the official, unmodified OpenViking
v0.3.21 image as a separately versioned service. The controller communicates
only through fixed HTTP API routes. OpenViking is a semantic index and context
service; SQLite remains the authoritative provenance, lifecycle, audit, and
workflow database.

The exact upstream tag, commit, Linux/amd64 image manifest, and license are in
`VERSION.json`. OpenViking v0.3.21 is licensed under AGPL-3.0-only. The complete
corresponding source is available from the recorded upstream repository and
revision. This project does not copy, link, or modify OpenViking code. Operators
who modify or provide the service over a network must review and satisfy the
AGPL source-offer obligations for their deployment.

## Operator setup

OpenViking requires an embedding configuration and may optionally use a VLM.
Model weights and provider secrets are deliberately not shipped. Initialize the
bind-mounted data directory with the official wizard, select a local CPU-capable
embedding provider (for example Ollama or a verified local GGUF), and configure
API-key mode:

```sh
install -d -m 0700 .data/memory/openviking .data/secrets
docker compose --profile memory run --rm openviking openviking-server init
```

Place the controller's OpenViking user or root key in
`.data/secrets/openviking.token` with mode `0600`. If a root key is used, the
configured account and user headers default to `maintainer` and
`maintainer-controller`; set `MAINTAINER_OPENVIKING_ACCOUNT` and
`MAINTAINER_OPENVIKING_USER` to the identities created during initialization.
The key must also match the server configuration. Never commit `ov.conf`, model
credentials, or the token; `.data/` is ignored.

Start the optional service and enable the controller adapter:

```sh
MAINTAINER_OPENVIKING_URL=http://openviking:1933 \
  docker compose --profile memory up -d openviking controller
```

No OpenViking port is published to the host. Readiness is available only on the
internal `control` network. Canonical-memory upserts and forgets use a durable
leased outbox and recover after either process restarts. Rebuild requests queue
every canonical project record before asking OpenViking to reindex its derived
semantic/vector artifacts.

To verify the pin without pulling model weights:

```sh
git ls-remote https://github.com/volcengine/OpenViking.git refs/tags/v0.3.21
docker manifest inspect --verbose ghcr.io/volcengine/openviking:v0.3.21
docker compose config --quiet
```
