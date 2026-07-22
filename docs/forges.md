# Git forge profiles and normalized synchronization

The Git forges page is the normal operator surface for GitHub, GitLab, and local bare Git profiles. `maintainctl forge` and the `/api/v1/forges` and per-project forge routes use the same controller service and durable records.

## Credential and endpoint boundary

The controller stores an opaque credential reference and redacted status, never a token, private key, webhook secret, or authenticated remote URL. GitHub App signing and installation tokens and GitLab tokens remain inside `git-bridge`. Workers, agents, the controller, and the browser do not receive them.

A profile is bound to an already registered project. Its provider and repository must exactly match that project; storage rejects attempts to retarget either identity. Hosted endpoints must use HTTPS except for loopback test servers and must exactly match a versioned allow-list entry. Local profiles use only `local://bare-git`. Changing a credential reference requires recent administrator reauthentication. The browser leaves the existing reference field blank and sends it only when an operator explicitly replaces the binding.

Profiles version and audit the endpoint allow-list, sync direction, polling interval, branch and change-request conventions, label mapping, CI artifact policy, release policy, submodule handling, and enabled state. These settings cannot introduce a command, image, mount, host path, network, environment variable, or credential into a worker.

## Normalized inventory

Every provider returns the same bounded object envelope for repositories, issues, change requests, discussions, pipelines, jobs, artifacts, branches, tags, releases, and submodules. The envelope retains the provider, external identity, title/state/ref/SHA/URL relationships, observation time, and bounded raw provider metadata. Repository text and provider metadata are untrusted evidence.

GitHub collection separates pull requests returned by the issues API from ordinary issues. GitLab artifact records derive from job `artifacts_file` metadata; synchronization never downloads an archive. Git refs and `.gitmodules` come from the exact credential-isolated mirror. Features unavailable because of provider semantics, edition, or permission are reported explicitly as `unsupported` or `operator_only` rather than as successful empty data.

Hosted inventory uses a versioned opaque cursor bound to the synchronized base SHA, endpoint family, and provider page. A changed base restarts collection safely. Only GET requests retry transient 502, 503, or 504 responses, with at most three attempts. HTTP 429 returns partial durable state plus the bounded retry delay; the controller does not sleep while holding the request. Provider publication calls are never automatically retried by this collector.

Each controller sync requires a safe idempotency key and atomically writes its run, normalized object upserts, and audit event. Repeating the same key and input returns the original run as a replay. Reusing the key for another project, provider, or cursor fails. Cursor and object metadata sizes, namespaces, kinds, counts, and JSON validity are checked again at the controller boundary.

## GitHub and GitLab permissions

The GitHub App requests only read Actions/checks/issues/metadata plus write contents and pull requests, matching existing exact-commit draft publication. Installation-token responses containing an unknown or broader permission are rejected. Optional repository Discussions access can report unavailable for an installation without failing other inventory families. Per-workflow-run job expansion is intentionally reported `operator_only` in the bounded repository collector.

GitLab uses a file-backed API token in `git-bridge`; it is sent only through request headers or temporary askpass transport. Diagnostics require Reporter-level read access and Developer-level draft-publication access. Discussion expansion is reported `operator_only` because it requires bounded per-issue and per-merge-request traversal.

GitLab merge-request webhooks use a separate random secret stored only in `.data/secrets/gitlab-webhook.secret` and mounted into `git-bridge`. Configure the GitLab webhook URL as `/api/v1/gitlab/webhooks`, enable merge-request events, and copy that secret into GitLab's secret-token field. The public controller route forwards the presented token and exact bounded payload through its authenticated internal bridge connection; only `git-bridge` holds the expected value and performs constant-time authentication. Closed rejected and merged events normalize to the same exact repository/branch/base/head/change-request contract as GitHub, retain an exact payload hash, reject replays with different bytes, and atomically update eligible memory only after matching the registered provider, exact publication number, branch, and result SHA. GitHub keeps its HMAC flow at `/api/v1/github/webhooks` unchanged.

## Operator workflow and recovery

Register the project first, then create a matching profile through the browser or CLI:

```text
maintainctl forge save PROJECT_ID forge-profile.json
maintainctl forge get PROJECT_ID
maintainctl forge probe PROJECT_ID
maintainctl forge sync PROJECT_ID --key operator-sync-001
maintainctl forge runs PROJECT_ID
maintainctl forge objects PROJECT_ID --kind change_request
```

For a partial run, pass its `output_cursor` to the next sync with a new idempotency key. After a controller restart, inspect `forge runs` before retrying. The same key safely replays a committed result; a new key continues from the stored cursor. If credentials rotate, reauthenticate, update the opaque reference, run the credential-safe probe, and then resume. Logs, API responses, UI diagnostics, sync rows, and normalized objects must not contain secret values.

The deterministic test path uses loopback fake GitHub/GitLab APIs, exact recorded webhook payloads, and local bare Git. It proves pagination, retries, rate-limit continuation, normalized inventory, GitHub HMAC and GitLab secret-token verification, publication matching, and replay safety. Real hosted credentials, provider editions, delivery routing, and provider-enforced rate limits remain operator-only validation and are not claimed by the local suite.
