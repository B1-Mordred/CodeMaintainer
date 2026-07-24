# Database migrations

The controller SQLite database is upgraded only through forward-only migrations under `internal/storage/sqlite/migrations/`. `internal/storage/sqlite/store.go` embeds every migration and records each applied version in `schema_migrations` inside the same transaction as the schema/data change. `CurrentSchemaVersion` is the runtime source of truth for the expected schema.

`config/migration-coverage.json` is the release inventory for schema evolution. Every migration entry must name the migration file, the durable records it introduces or preserves, restart/resume evidence, backup/restore evidence, documentation, and rollback guidance. `TestMigrationCoverageInventoryMatchesEmbeddedForwardMigrations` fails when a migration file is added without updating that inventory.

## Required change process

1. Add a new `NNN_description.sql` file with a monotonically increasing version.
2. Embed it in `internal/storage/sqlite/store.go`, add it to `allMigrations()`, and update `CurrentSchemaVersion`.
3. Add or extend storage tests that open an older supported schema, run `Open`, and prove the migrated records, provenance, and workflow state survive restart.
4. Add backup/restore evidence if the migration changes durable state that must be preserved across appliance recovery.
5. Update `config/migration-coverage.json`, this document when the procedure changes, `config/increment-2-coverage.json`, and the relevant execplan revision notes.

## Backup, restore, and rollback model

Rollback is operational rather than down-migration based. Migrations never rewrite the ledger backward. Before production upgrade, create an encrypted backup and run a dry-run restore compatibility check. If promotion fails, restore the last known-good backup or redeploy the previous known-good image against that backup. During restore, the controller stages a checksum-validated bundle, requires a restart, preserves the previous database as `.pre-restore-<timestamp>`, removes stale WAL/SHM files, and atomically writes the restored database.

The deterministic CI path uses plaintext mock bundles so tests can inspect manifests and checksums. Production requires a private `0600` base64 32-byte key and AES-256-GCM encrypted bundles; the key is not backed up by the application and must be escrowed separately by the operator.

## Operator-only production validation

For every release that changes the schema, run the documented operator-only restore on a separate host: create an encrypted backup, dry-run restore it, stage and apply it, restart the appliance, then verify users, projects, audit events, memory, artifacts, mirrors, configuration history, and one smoke job before deleting the preserved pre-restore database. Record the image digest, schema version, backup ID, operator, timestamp, command output, and retained evidence.
