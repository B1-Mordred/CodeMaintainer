#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

docker_command=(docker)
if ! docker info >/dev/null 2>&1; then
	docker_command=(sudo -n docker)
fi
compose() { "${docker_command[@]}" compose "$@"; }

action="${1:-preflight}"
case "$action" in
  preflight)
	if [[ -n "$(git status --porcelain)" ]]; then
		printf '%s\n' 'Update preflight requires a committed, clean source tree.' >&2
		exit 1
	fi
	compose config --quiet
	compose run --rm go-tool go test ./...
	compose run --rm go-tool go vet ./...
	compose run --rm web-tool sh -c 'npm ci && npm test && npm run build && npm run check:api'
    ./scripts/acceptance.sh
	./scripts/vulnerability-scan.sh
	./scripts/sbom.sh
    mkdir -p .data/updates
    git rev-parse HEAD >.data/updates/preflight-passed
    printf 'Update preflight passed. Create and dry-run a fresh backup before apply.\n'
    ;;
  apply)
    if [[ ! -f .data/updates/preflight-passed ]] || [[ "$(cat .data/updates/preflight-passed)" != "$(git rev-parse HEAD)" ]]; then
      printf 'Run scripts/update.sh preflight and record the approved candidate before apply.\n' >&2
      exit 1
    fi
	backup_json="$(compose run --rm maintainctl backup)"
	backup_id="$(printf '%s\n' "$backup_json" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
	if [[ -z "$backup_id" ]]; then
		printf '%s\n' 'A recently reauthenticated administrator session is required to create the pre-update backup.' >&2
		exit 1
	fi
	compose run --rm maintainctl restore --dry-run "$backup_id"
	mkdir -p .data/updates
	if [[ -f .data/updates/deployed-source ]]; then
		cp .data/updates/deployed-source .data/updates/known-good-source
	else
		git rev-parse HEAD >.data/updates/known-good-source
	fi
	for image in local/code-maintainer-controller:0.1.0-dev local/code-maintainer-cli:0.1.0-dev local/code-maintainer-runnerd:0.1.0-dev local/code-maintainer-fake-model-server:0.1.0-dev local/code-maintainer-git-bridge:0.1.0-dev local/code-maintainer-code-intelligence:0.1.0-dev; do
		if "${docker_command[@]}" image inspect "$image" >/dev/null 2>&1; then
			"${docker_command[@]}" image tag "$image" "${image%:*}:known-good"
		fi
	done
    compose build controller maintainctl runnerd model-supervisor git-bridge code-intelligence
    compose -f compose.yaml -f compose.dev.yaml up -d
    compose run --rm maintainctl health
	git rev-parse HEAD >.data/updates/deployed-source
    printf 'Candidate promoted. Known-good source metadata is in .data/updates.\n'
    ;;
  rollback)
	for image in local/code-maintainer-controller:0.1.0-dev local/code-maintainer-cli:0.1.0-dev local/code-maintainer-runnerd:0.1.0-dev local/code-maintainer-fake-model-server:0.1.0-dev local/code-maintainer-git-bridge:0.1.0-dev local/code-maintainer-code-intelligence:0.1.0-dev; do
		known_good="${image%:*}:known-good"
		"${docker_command[@]}" image inspect "$known_good" >/dev/null
		"${docker_command[@]}" image tag "$known_good" "$image"
	done
	compose -f compose.yaml -f compose.dev.yaml up -d --no-build
	compose run --rm maintainctl health
	if [[ -f .data/updates/known-good-source ]]; then
		cp .data/updates/known-good-source .data/updates/deployed-source
	fi
	printf 'Known-good images restored. If schema compatibility requires it, stage the validated pre-update backup and restart before resuming work.\n'
    ;;
  *)
    printf 'usage: scripts/update.sh [preflight|apply|rollback]\n' >&2
    exit 2
    ;;
esac
