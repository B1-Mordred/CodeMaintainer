# Hermes Agent integration

The optional `hermes` Compose profile uses the official Hermes Agent
`v2026.7.7.2` Linux/amd64 image. The exact upstream commit, image manifest, and
MIT license are recorded in `VERSION.json`.

Run `./scripts/bootstrap.sh` before enabling the profile. Bootstrap creates a
private controller service token and copies `config.yaml.example` to
`.data/hermes/config.yaml` without overwriting operator changes. Start the
profile with:

```sh
docker compose --profile hermes up -d hermes-tool-bridge hermes
```

The Hermes container has no host port, Docker socket, repository worktree,
controller token, GitHub credential, or controller database. It runs as the
deployment UID with a read-only root filesystem and reaches only the internal
MCP bridge and inference network. The bridge alone receives the controller
token as a read-only file and translates ten fixed MCP tools to the narrow
`/api/v1/hermes/tools/*` contract. The bridge does not expose a host port.

The supplied configuration enables only `mcp-maintainer`, repeats the exact
tool allowlist at the Hermes MCP client, disables MCP resources and prompts,
and globally disables built-in file, terminal, execution, browser, web,
memory, skill-management, delegation, cron, and other toolsets. Skill content
can only enter the controller's versioned proposal queue; a database constraint
keeps it inert even after administrator review.

The example uses the deterministic fake model endpoint only for integration
checks. Configure an actual model provider and a desired messaging channel in
`.data/hermes` before using conversational operation. Provider credentials stay
in Hermes' private data directory and are never mounted into the controller or
bridge. No GitHub credential belongs in this directory.

OpenViking is not connected directly to Hermes in this appliance. The upstream
provider would bypass the controller's registered-project namespace filter and
retrieval ledger. Hermes therefore queries memory through the controller MCP
tool, while the controller uses OpenViking as a replaceable index. This is the
compatible security-preserving integration for this trust model.

Smoke-test MCP discovery without starting a messaging gateway:

```sh
docker compose --profile hermes run --rm --no-deps hermes mcp test maintainer
```

Source and license:

- https://github.com/NousResearch/hermes-agent
- https://github.com/NousResearch/hermes-agent/blob/v2026.7.7.2/LICENSE
