# Configuration registry and workbench

The controller owns the trusted configuration registry. Repository files, model output, imports, and browser input cannot add descriptors, validators, scopes, hooks, commands, images, mounts, networks, paths, capabilities, or environment variables. Each stable dotted key declares its type, JSON schema, allowed scopes, safe default, role, apply mode, dependency and prerequisite metadata, export/import policy, redaction rule, and UI hint in the controller binary.

The machine-readable setting inventory is [`config/configuration-coverage.json`](../config/configuration-coverage.json). CI compares it to the compiled descriptor registry and rejects a missing setting or missing service, API, CLI, UI, permission, validation, persistence, audit, rollback, documentation, or test mapping.

## Typed workbench

Open **Configuration** in the authenticated console. Choose one exact scope, then search or browse the grouped typed controls. Safe mode hides advanced bootstrap fields; Expert mode shows them as read-only. Each control displays its current effective value, winning source, current-scope state, and apply mode. **Inherited** removes this scope's override; **Safe default** writes the descriptor's trusted built-in value at the selected scope. The before/after preview names the apply impact before a draft is saved.

The deterministic precedence order is:

1. built-in default;
2. system;
3. capability pack;
4. project;
5. named environment or runner;
6. job template;
7. one-job override.

Save edits with an audited reason. The controller requires the displayed scope ETag and creates a draft bound to that exact version. Dependencies and incompatibilities are evaluated against the prospective effective values, including active conditional relations; the browser previews their messages and the controller enforces them again for create, import, validate, dry run, review, and apply. Validation and dry-run results are append-only and bound to the draft version. Review binds the exact normalized draft bytes. Apply accepts only that reviewed version and creates a new scoped revision and audit event transactionally. Stale concurrent edits fail with a conflict and must be refreshed; the browser does not overwrite them.

The prerequisite catalog runs only compiled trusted checkers. The local-inbox descriptor currently probes durable controller storage and its version-bound dry run through the registered `durable-controller-storage` and `local-inbox-readiness` handlers. Missing handler names fail closed as unavailable; browser or imported content cannot register executable checks.

Apply badges distinguish live settings, settings used only by newly accepted jobs, service reloads, and operator restarts. New-job settings must be read from the accepted job's immutable configuration snapshot, never from later live state.

The matching recovery and automation commands are under `maintainctl config`: `descriptors`, `values`, `effective`, `draft`, `history`, `registry-rollback`, `registry-export`, `import-preview`, and `import`.

## Export and import

Exports are deterministic, schema-versioned, registry-hash-bound, scope/version-provenanced, and protected by a document SHA-256 hash. Secret descriptors export configured/redacted state only; raw secret material is never returned. Bootstrap-only or non-exportable values are not portable.

Strict import rejects every unknown key. Forward-compatible import may preserve an unknown, structurally valid entry as immutable import-draft evidence, but the entry is never inserted into scoped values and never becomes effective. Known entries still pass the current descriptor's type, scope, dependency, and import policy. An import creates a normal draft and therefore still requires validation, review, and explicit apply.

## Rollback and history

Scoped history is append-only. Rollback requires an administrator, recent reauthentication, an audited reason, and the current scope ETag. It creates a new revision from the selected historical scope state; it never deletes or rewrites later history. Whole-document Increment 1 revisions remain available under Administration as a compatibility and recovery path.

## Bootstrap-controlled deployment settings

`deployment.listen_address`, `deployment.data_root`, and `deployment.profile` define trust and filesystem/process boundaries. They are visible as advanced read-only descriptors but cannot be changed through the API, CLI, browser, imports, packs, projects, templates, or jobs. Change them only through the documented trusted startup configuration and restart procedure. Their immutability prevents a routine configuration edit from broadening network exposure, selecting arbitrary host paths, or switching implementation adapters.
