#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

docker_command=(docker)
if ! docker info >/dev/null 2>&1; then
  if command -v sudo >/dev/null 2>&1 && sudo -n docker info >/dev/null 2>&1; then
    docker_command=(sudo -n docker)
  else
    printf 'Docker is unavailable. Configure rootless Docker or explicit operator access first.\n' >&2
    exit 1
  fi
fi

compose() {
  "${docker_command[@]}" compose "$@"
}

if ! compose version >/dev/null 2>&1; then
  printf 'Docker Compose v2 is required.\n' >&2
  exit 1
fi

mkdir -p .data/acceptance
compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm go-tool \
  go run ./cmd/soak-runner --output .data/acceptance/soak-report.json
