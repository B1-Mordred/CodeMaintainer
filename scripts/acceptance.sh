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

acceptance_port="${MAINTAINER_ACCEPTANCE_PORT:-8080}"
acceptance_base_url="http://127.0.0.1:${acceptance_port}"
compose_files=(-f compose.yaml -f compose.dev.yaml)
if [[ "$acceptance_port" != "8080" ]]; then
  compose_files+=(-f compose.acceptance.yaml)
fi

acceptance_compose() {
  compose "${compose_files[@]}" "$@"
}

wait_for_controller() {
  for attempt in $(seq 1 60); do
    if curl --fail --silent --show-error "${acceptance_base_url}/healthz"; then
      return 0
    fi
    sleep 1
  done
  printf 'Controller did not become healthy at %s within 60 seconds.\n' "$acceptance_base_url" >&2
  return 1
}

./scripts/bootstrap.sh

acceptance_compose --profile tools run --rm go-tool sh -c \
  'test -z "$(gofmt -l cmd internal)" && go test ./... && go vet ./...'
acceptance_compose --profile tools run --rm web-tool sh -c \
  'npm ci --ignore-scripts && npm run check:api && npm test && npx tsc --noEmit -p tsconfig.app.json && npx redocly lint ../internal/api/openapi.yaml'
acceptance_compose config --quiet
acceptance_compose build controller maintainctl runnerd code-intelligence
acceptance_compose up -d runnerd controller
wait_for_controller
auth_status="$(curl --fail --silent --show-error "${acceptance_base_url}/api/v1/auth/status" | tr -d '[:space:]')"
acceptance_password_file=".data/secrets/acceptance-admin.password"
if printf '%s' "$auth_status" | grep -q '"bootstrapped":false'; then
  temporary_password_file="$(mktemp .data/secrets/.acceptance-admin.password.XXXXXX)"
  trap 'rm -f "$temporary_password_file"' EXIT
  chmod 600 "$temporary_password_file"
  printf 'Acceptance-%s\n' "$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')" >"$temporary_password_file"
  mv "$temporary_password_file" "$acceptance_password_file"
  trap - EXIT
  chmod 600 "$acceptance_password_file"
  acceptance_compose --profile tools run --rm maintainctl bootstrap \
    --username acceptance-admin \
    --display-name "Acceptance Administrator" \
    --password-file /workspace/.data/secrets/acceptance-admin.password
elif ! acceptance_compose --profile tools run --rm maintainctl doctor >/dev/null 2>&1; then
  if [[ -r "$acceptance_password_file" ]]; then
    acceptance_compose --profile tools run --rm maintainctl login \
      --username acceptance-admin \
      --password-file /workspace/.data/secrets/acceptance-admin.password
  else
    printf '%s\n' "Acceptance requires a valid .data/cli session.json or .data/secrets/acceptance-admin.password for the existing bootstrapped database." >&2
    exit 1
  fi
fi
acceptance_compose --profile tools run --rm maintainctl doctor
acceptance_compose --profile tools run --rm --build browser-tool ./test/e2e/ui-smoke.sh "$acceptance_base_url"

printf '\nLocal application and real-browser acceptance passed.\n'
