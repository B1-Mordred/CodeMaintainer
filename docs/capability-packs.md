# Repository onboarding and capability packs

The Onboarding & packs page is the normal operator interface for Repo Doctor and the trusted capability-pack catalog. `maintainctl repo doctor`, `maintainctl pack`, and the `/api/v1/capability-packs` and project Repo Doctor routes expose the same controller-owned application service.

## Trust and authority boundary

A pack is a typed declarative manifest compiled into the controller. Its SHA-256 checksum covers the canonical manifest content. A manifest can name only controller-registered runner profiles, operation classes, parsers, policy fragments, context selectors, risk and documentation rules, UI fields, and rehearsal definitions. It has no image, executable, mount, network, host path, Linux capability, unrestricted environment, model argument, or free-form runner-command field. Pack installation never changes runner authority.

Lifecycle changes use optimistic installation revisions and append both a pack event and an audit event in the same SQLite transaction. Install, enable, disable, upgrade, rollback, pin, and unpin require recent administrator reauthentication. A pin blocks upgrade and rollback. Upgrade targets must be newer, rollback targets must be older and must appear in the retained lifecycle history. Project assignments retain an exact pack version and controller-normalized typed configuration; upgrading an installation never silently changes an assignment. The browser renders only fields declared by the trusted manifest. The controller rejects duplicate JSON keys, unknown or nested undeclared fields, arrays, out-of-range numbers, invalid enumerations, unsafe repository references, malformed timestamps, and documents larger than 64 KiB. Defaults are expanded before storage so API, CLI, and browser inspection see the same effective document.

The built-in catalog currently includes:

- `php83-intranet@1.0.0`: Composer validation/audit and locks, PSR-4, PHPUnit and coverage parsing, PHPStan/Psalm, Rector dry run, selected Infection, Twig, disposable Apache/MySQL 8/Redis profiles, Vite/Tailwind, Playwright, theme/plugin contracts, and portability checks. The project selects optional tools, services, thresholds, and budgets.
- `r-statistical-validation@1.0.0`: `renv`, `R CMD check`, `testthat`, lint, `roxygen2`, `pkgdown`, reproducibility controls, and versioned numerical/table/model/chart comparison primitives.
- `sbom-fmea-security@1.0.0`: explicitly selected Syft, Grype, Trivy, or CodeQL operation classes, SBOM comparison, expiring suppression policy, and evidence-linked FMEA correlation. CAPEC and ATT&CK mappings remain operator-maintained references, not exploitability claims.

Unavailable prerequisites are visible in catalog and preview responses. The controller never substitutes an unselected scanner, coverage engine, service, or analyzer.

## Repo Doctor

Repo Doctor accepts no repository paths, bytes, commands, detector rules, pack manifests, or runner fields from the browser. The controller synchronizes the registered project through the credential-isolated Git bridge and scans its exact bounded snapshot. Files are limited to 5,000, individual files to 2 MiB, and the total to 64 MiB. Unsafe paths and oversized input fail the scan.

Detection covers languages, frameworks, package managers and locks, build/test/static-analysis configuration, CI, guidance and ownership files, migrations and risk paths, submodules/LFS, UTF-8, and CRLF observations. Each finding and proposal cites a snapshot path and SHA-256 content identity with a confidence score. Repository text is untrusted data; it is never interpreted as a controller instruction.

A scan writes findings and disabled proposals only. It does not install a pack, create an assignment, edit configuration, modify the repository, or change an allow-list. Re-scans compare findings with the previous scan and expose added/removed drift. The operator can inspect current and proposed state, edit the trusted typed proposal fields, run a no-write preview, and explicitly accept or reject using the proposal revision. A stale review conflicts. Accepting a pack proposal succeeds only when the exact proposed checksummed version is already installed and enabled; proposal review and normalized assignment then commit transactionally.

An existing assignment can be reconfigured from the same typed editor. Preview invokes controller validation without mutation and returns the normalized effective configuration and fixed workflow impact. Apply requires the exact assignment revision plus an audited reason, increments the revision transactionally, and stores only a configuration digest in the audit payload. A stale revision conflicts and no partial update is retained. R and security pack details are documented in [R statistical validation](r-statistical-pack.md) and [SBOM, FMEA, and security](sbom-fmea-security-pack.md).

## Golden and rehearsal primitives

Pack manifests can declare a fixed rehearsal ID, kind, registered operation class, artifact kinds, comparison class, and approval-policy reference. The PHP pack includes portability, database, browser screenshot/accessibility/journey primitives; the R pack includes numerical reference-result comparison; the security pack includes SBOM release diff. These are reusable definitions only. Versioned artifact provenance, baseline-update approvals, masks/tolerances, rendered diffs, and blocking workflow gates are delivered with the Milestone 5 golden/rehearsal policy layer. No failing rehearsal can update its own golden.

## CLI examples

Reauthenticate before lifecycle changes, then preview and install an exact version:

```text
maintainctl reauthenticate --password-file -
maintainctl pack preview php83-intranet --action install --version 1.0.0 --revision 0 --reason "review prerequisites"
maintainctl pack install php83-intranet --version 1.0.0 --revision 0 --reason "approved trusted PHP profile"
maintainctl repo doctor owner/repository
maintainctl repo doctor-scans owner/repository
```

Use the scan and proposal IDs from the scan result for a no-write preview and explicit review:

```text
maintainctl pack proposal dry-run owner-repository scan_ID proposal_ID --version 1 --reason "review evidence" --config project-pack.json
maintainctl pack proposal accept owner-repository scan_ID proposal_ID --version 1 --reason "approved exact pack assignment" --config project-pack.json
```

Lifecycle history and exact project assignments remain inspectable with `maintainctl pack events PACK_ID` and `maintainctl pack assignments PROJECT_ID`.

Preview and apply the same typed assignment document used by the browser:

```text
maintainctl pack configure-preview --revision 3 --config r-profile.json --reason "review reproducibility controls" PROJECT_ID r-statistical-validation
maintainctl pack configure --revision 3 --config r-profile.json --reason "approve reproducibility controls" PROJECT_ID r-statistical-validation
```

The configuration file must be a JSON object using only catalog-declared fields. Preview first when automating an update; apply still performs the complete validation again inside the optimistic write path.
