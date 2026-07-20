#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_root="$repo_root/artifacts/sbom"
mkdir -p "$output_root"

docker_command=(docker)
if ! docker info >/dev/null 2>&1; then
  docker_command=(sudo -n docker)
fi

"${docker_command[@]}" compose run --rm go-tool go list -m -json all >"$output_root/go-modules.json"
"${docker_command[@]}" compose run --rm web-tool sh -c 'npm ci >/dev/null && npm ls --all --json' >"$output_root/npm-packages.json"
"${docker_command[@]}" compose --profile tools config --images | sort -u >"$output_root/container-images.txt"
git -C "$repo_root" rev-parse HEAD >"$output_root/source-revision.txt"

if command -v syft >/dev/null 2>&1; then
  syft dir:"$repo_root" -o spdx-json="$output_root/source.spdx.json"
fi

printf 'SBOM inventory written to %s\n' "$output_root"
