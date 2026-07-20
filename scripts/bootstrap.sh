#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v docker >/dev/null 2>&1; then
  printf 'Docker is required but was not found. This script never installs or changes the host Docker daemon.\n' >&2
  exit 1
fi

docker_command=(docker)
if ! docker info >/dev/null 2>&1; then
  if command -v sudo >/dev/null 2>&1 && sudo -n docker info >/dev/null 2>&1; then
    docker_command=(sudo -n docker)
  else
    printf 'Docker is unavailable. Configure rootless Docker or grant this operator access explicitly.\n' >&2
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

mkdir -p \
  .data/models .data/mirrors .data/remotes .data/worktrees .data/artifacts \
  .data/database .data/memory .data/caches .data/config \
  .data/secrets .data/backups .data/run .data/cli .cache/go/build .cache/go/mod .cache/npm
chmod 700 .data .data/database .data/secrets .data/backups .data/cli

runnerd_token=.data/secrets/runnerd.token
if [[ ! -e "$runnerd_token" ]]; then
  umask 077
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n' >"$runnerd_token"
fi
chmod 600 "$runnerd_token"

model_token=.data/secrets/model-control.token
if [[ ! -e "$model_token" ]]; then
  umask 077
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n' >"$model_token"
fi
chmod 600 "$model_token"

git_bridge_token=.data/secrets/git-bridge.token
if [[ ! -e "$git_bridge_token" ]]; then
  umask 077
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n' >"$git_bridge_token"
fi
chmod 600 "$git_bridge_token"

./scripts/seed-mock-remote.sh

compose -f compose.yaml -f compose.dev.yaml config --quiet
printf 'Bootstrap preflight passed. Start the mock profile with: ./maintainctl up\n'
