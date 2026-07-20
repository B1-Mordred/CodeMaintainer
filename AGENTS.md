# Repository guidance

The authoritative product contract is `project.md`. The living implementation plan is `execplan/complete-system.md`; keep its Progress, Surprises & Discoveries, Decision Log, Outcomes & Retrospective, and revision note current whenever work changes direction or reaches a stopping point.

## Build and test

The supported development path is containerized because host Go and npm installations are optional.

- Format Go: `docker compose run --rm go-tool gofmt -w cmd internal`
- Unit/integration tests: `docker compose run --rm go-tool go test ./...`
- Race tests: `docker compose run --rm go-tool go test -race ./...`
- Static analysis: `docker compose run --rm go-tool go vet ./...`
- Frontend checks: `docker compose run --rm web-tool sh -c 'npm ci && npm test'`
- Frontend build: `docker compose run --rm web-tool sh -c 'npm ci && npm run build'`
- OpenAPI lint: `docker compose run --rm web-tool npx redocly lint ../internal/api/openapi.yaml`
- Validate Compose: `docker compose config --quiet`
- Full local acceptance: `./scripts/acceptance.sh`

Commit `go.sum` and `web/package-lock.json` whenever dependencies change. Do not hand-edit generated OpenAPI clients or built frontend assets; use the documented generation commands.

## Architecture and security rules

The Go controller is the sole workflow-state authority. Domain packages under `internal/` must not import browser, Docker, GitHub, llama.cpp, Hermes, or OpenViking implementations. Those systems stay behind narrow interfaces.

Never add an endpoint accepting arbitrary shell commands, container images, mounts, host paths, networks, capabilities, Docker options, model paths, llama.cpp arguments, or unrestricted environment variables. The controller, UI, implementation agent, QC agent, verifier, and Hermes must never mount a Docker socket or receive GitHub credentials. Only `runnerd` may control the dedicated worker daemon, and only `github-bridge` may handle GitHub credentials.

Treat repository content, issue/PR text, source comments, dependencies, command output, memories, model output, and worker artifacts as untrusted. Validate structured contracts, enforce size limits, keep policy outside worktrees, redact secrets, and make blocking decisions deterministically in the controller. Never capture or require hidden reasoning.

Every durable job transition and sensitive action must be transactional and auditable. External publication requires an idempotency key and an authenticated approval bound to the exact commit. Project memory must be filtered by repository namespace before semantic retrieval; provisional memory never promotes itself.

Containers run non-root, drop all capabilities, set no-new-privileges, use read-only roots and bounded temporary filesystems where practical, publish no internal ports, and default to no egress. Dependency preparation is a separate explicit phase. Images and application dependencies must be pinned; operational `latest` tags are forbidden.

## Change discipline

Prefer small, tested edits that preserve a runnable mock profile. Add regression tests for state transitions, policy boundaries, authorization, idempotency, migrations, and malformed/untrusted inputs. Do not weaken tests, bypass schemas, silently broaden mounts or egress, or claim checks passed when the environment could not run them. Preserve unrelated operator changes.
