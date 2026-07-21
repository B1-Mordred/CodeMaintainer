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

./scripts/bootstrap.sh

compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm go-tool sh -c \
  'test -z "$(gofmt -l cmd internal)" && go test ./... && go vet ./...'
compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm web-tool sh -c \
  'npm ci --ignore-scripts && npm run check:api && npm test && npx tsc --noEmit -p tsconfig.app.json && npx redocly lint ../internal/api/openapi.yaml'
compose -f compose.yaml -f compose.dev.yaml config --quiet
compose -f compose.yaml -f compose.dev.yaml build controller maintainctl runnerd code-intelligence
compose -f compose.yaml -f compose.dev.yaml up -d runnerd controller
curl --fail --silent --show-error http://127.0.0.1:8080/healthz
compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm maintainctl doctor
compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm --build browser-tool

printf '\nLocal application and real-browser acceptance passed.\n'
