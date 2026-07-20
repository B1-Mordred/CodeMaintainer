#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v docker >/dev/null 2>&1; then
  printf 'Docker is required but was not found. This script never installs or changes the host Docker daemon.\n' >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  printf 'Docker Compose v2 is required. Configure rootless Docker or grant this operator access explicitly.\n' >&2
  exit 1
fi

mkdir -p \
  .data/models .data/mirrors .data/worktrees .data/artifacts \
  .data/database .data/memory .data/caches .data/config \
  .data/secrets .data/backups .cache/go/build .cache/go/mod .cache/npm
chmod 700 .data .data/database .data/secrets .data/backups

docker compose -f compose.yaml -f compose.dev.yaml config --quiet
printf 'Bootstrap preflight passed. Start the mock profile with: ./maintainctl up\n'
