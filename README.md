# CodeMaintainer

CodeMaintainer is a self-hosted, browser-operated appliance for producing minimal, independently reviewed, deterministically verified maintenance patches across multiple repositories. It targets CPU-only Linux, works without cloud LLMs, and ships a complete deterministic mock profile that needs neither GitHub credentials nor model weights.

Canonical repository: <https://github.com/B1-Mordred/CodeMaintainer>

## Quick start

The supported toolchain runs in Docker; Go and npm are not required on the host.

```sh
./maintainctl bootstrap
./maintainctl up
```

Open <http://127.0.0.1:8080>, create the one-time administrator, register the seeded `fixture/arithmetic` project, and submit a smoke task. The controller, UI, worker policy, model supervisor, Git bridge, memory boundary, and publication gate all remain separately testable.

Use the same application API from the CLI:

```sh
./maintainctl login --username admin --password-file /path/to/private-password
./maintainctl reauthenticate --password-file /path/to/private-password
./maintainctl doctor
./maintainctl repo add fixture/arithmetic
./maintainctl run fixture/arithmetic --task "Add a regression test for negative operands"
./maintainctl status
```

Run the full local gate with `./scripts/acceptance.sh`; run `./scripts/agent-image-acceptance.sh` for the immutable implementation/QC image boundary and `docker compose --profile tools run --rm --build browser-tool` for the pinned real-browser gate. See [deployment](docs/deployment.md), [operations](docs/operations.md), and [security](docs/security.md) before enabling production integrations.

## Documentation

- [Architecture](docs/architecture.md)
- [Deployment and remote access](docs/deployment.md)
- [GitHub App](docs/github-app.md)
- [Models](docs/models.md)
- [Memory](docs/memory.md)
- [Repository onboarding and capability packs](docs/capability-packs.md)
- [Operations and updates](docs/operations.md)
- [Backup and restore](docs/backup-restore.md)
- [Security](docs/security.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Licensing and SBOM](docs/licensing.md)
- [Acceptance evidence](docs/acceptance.md)

`project.md` remains the authoritative product and security contract. `execplan/complete-system.md` records implementation evidence and remaining operator-only validations.
