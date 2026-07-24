# Backup and restore

`maintainctl backup` creates a consistent SQLite `VACUUM INTO` snapshot containing configuration and audit data, plus checksum-bound retained artifacts, bounded repository mirror files, and OpenViking state. Worktrees are disposable and model weights are reproducible from manifests, so both are excluded. Individual files and total bundles are size bounded; exclusions are reported.

Mock bundles are plaintext gzip JSON for inspectable CI. Production refuses to start the backup service without a private, mode-`0600`, unpadded base64 32-byte key and encrypts the complete compressed bundle with AES-256-GCM. Keep the key outside backups, escrow it separately, rotate it by retaining old keys until their bundles expire, and test recovery after rotation.

Always run `maintainctl restore --dry-run <backup-id>` first. It validates encryption, manifest checksum, each file checksum, byte counts, paths, and schema compatibility. `maintainctl restore --apply <backup-id>` stages the already validated bundle. Then stop and restart the full appliance; before SQLite opens, the controller preserves the old database as `.pre-restore-<timestamp>`, replaces selected files atomically, removes stale WAL/SHM files, and clears the pending marker. Verify `/readyz`, `doctor`, users, projects, audit, memory, and one smoke job before deleting the preserved database.

Schema changes are tracked in `docs/migrations.md` and `config/migration-coverage.json`. The coverage inventory is checked by the SQLite test suite so every migration file must declare restart/resume evidence, backup/restore evidence, documentation, durable records, and rollback guidance before it can ship.
