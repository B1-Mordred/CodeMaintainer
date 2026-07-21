# Security

The browser-facing controller has no Docker socket or GitHub credential. Workers are non-root, capability-free, `no-new-privileges`, read-only-root, bounded by CPU/RAM/PIDs/tmpfs/log/artifact/wall/disk policies, and offline except for a separate dependency phase or inference-only network. They mount one worktree and never the host home, controller database, secrets, other worktrees, or Docker socket.

All repository text, issues, memories, dependencies, command output, model output, and artifacts are untrusted. Structured schemas, byte limits, protected-path checks, secret scans, exact commit binding, and deterministic policy remain outside worktrees. Hidden reasoning is neither requested nor retained.

Local passwords use Argon2id. Server sessions and CSRF values are random and stored only as hashes; cookies are HttpOnly and SameSite, secure in TLS mode, and expire after 12 hours. Sensitive user, backup, restore, and publication actions require five-minute reauthentication. Roles are exact and non-hierarchical. Authority changes revoke sessions and the final enabled administrator cannot be removed.

Review `config/examples/runner-policy.production.example.json`, use a dedicated rootless daemon, protect all secret files with mode `0600`, keep the UI on loopback or authenticated TLS, and treat audit/history retention as security evidence.

## Threat model

Protected assets are source and unpublished patches, GitHub App material, local identities and sessions, model files, project-scoped memory, the controller database/audit ledger, and host execution authority. The browser and controller form the trusted operator boundary; the Git bridge, runnerd, model supervisor, optional index, and optional Hermes bridge are separately authenticated service boundaries. Worktrees, repository and issue text, dependencies, worker output, model output, the browser network, and every external provider are untrusted.

Expected attackers include a malicious repository contributor, prompt-injected issue or memory content, a compromised worker or dependency, a remote browser attacker, and an authenticated user exceeding their role. The design addresses the principal abuse paths as follows:

- Repository text cannot select commands, images, mounts, networks, model paths, or provider credentials; all are resolved from trusted schemas and server policy.
- A compromised worker has one disposable worktree, fixed resources, no general network, no control-plane secrets, no host home, and no Docker socket. Publication occurs only through the separate Git bridge after exact-SHA gates.
- Prompt-injected model output is schema-validated and non-authoritative. Deterministic verification and controller-owned finding and approval state decide progress.
- Browser request forgery is constrained by HttpOnly SameSite sessions, random CSRF tokens, origin/Fetch-Metadata checks, CSP, no CORS, exact RBAC, rate limits, and recent reauthentication.
- GitHub credential theft is constrained by bridge-only file secrets, short-lived installation tokens, credential-free remotes, temporary askpass, narrow repository permission checks, redaction, and no token-returning API.
- Cross-project memory access is prevented by deriving exact namespaces from registered projects before ranking. Quarantine, provenance, retrieval traces, secret scans, and authenticated lifecycle operations prevent semantic memory from becoming authority.
- Duplicate or stale external effects are constrained by idempotency keys, webhook replay records, durable leases, exact commit binding, upstream rechecks, and append-only decisions.
- Backup theft is constrained by mode-0600 AES-256-GCM production bundles and an independently protected 256-bit key. Restore validates schema, manifest, every file hash, safe paths, and stages before restart.

## Residual risks and operator checks

Container isolation is not a VM boundary; kernel or daemon compromise can escape it. Production therefore still requires a dedicated rootless daemon, current host patches, an operator-reviewed seccomp/AppArmor or SELinux policy, and validation that the selected UID owns only the deployment data. Dependency acquisition intentionally has bounded egress and remains a supply-chain risk; review lock changes, SBOM/license output, vulnerability results, and image signatures before promotion. Local administrators can authorize destructive state changes and read retained evidence; protect their endpoint, require TLS remotely, use distinct operator/reviewer accounts, and export audit records off-host when tamper resistance is required. Availability is single-host and SQLite-backed; test encrypted restores on a separate host and retain the key separately. Real GitHub App publication, real model weights/licenses, real OpenViking embedding behavior, external OIDC/passkey configuration, and the dedicated rootless-daemon topology remain operator-environment validations and are not simulated as production proof.

Tree-sitter's C runtime and pinned grammars are isolated in the non-root `code-intelligence` service so the controller and recovery CLI retain their static `CGO_ENABLED=0` distroless build. The parser service receives only bounded hash-bound file bytes over the internal control network, has no repository/data mounts or published port, and cannot launch SCIP/LSP commands. Controller-side validation remains authoritative for every returned fact.
