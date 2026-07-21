#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

docker_command=(docker)
if ! docker info >/dev/null 2>&1; then
  docker_command=(sudo -n docker)
fi

acceptance_root="$(mktemp -d "$repo_root/.data/agent-acceptance-XXXXXX")"
case "$acceptance_root" in
  "$repo_root"/.data/agent-acceptance-*) ;;
  *) printf 'unsafe acceptance root: %s\n' "$acceptance_root" >&2; exit 1 ;;
esac
suffix="${acceptance_root##*-}"
network="maintainer-agent-$suffix"
server="maintainer-fake-model-$suffix"

cleanup() {
  "${docker_command[@]}" rm -f "$server" >/dev/null 2>&1 || true
  "${docker_command[@]}" network rm "$network" >/dev/null 2>&1 || true
  rm -rf "$acceptance_root"
}
trap cleanup EXIT

mkdir -p "$acceptance_root/worktree" "$acceptance_root/implementation-artifacts" \
  "$acceptance_root/qc0-artifacts" "$acceptance_root/qc1-artifacts"
cp test/fixtures/agents/answer.go "$acceptance_root/worktree/answer.go"

"${docker_command[@]}" network create --internal "$network" >/dev/null
"${docker_command[@]}" run -d --name "$server" --network "$network" --network-alias model-supervisor --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=32m --cap-drop ALL --security-opt no-new-privileges \
  local/codemaintainer-fake-model-server:0.1.0-dev >/dev/null

for attempt in $(seq 1 50); do
  if [[ "$("${docker_command[@]}" inspect --format '{{.State.Running}}' "$server")" == true ]]; then
    break
  fi
  if [[ "$attempt" == 50 ]]; then
    printf 'fake model did not become available\n' >&2
    exit 1
  fi
  sleep 0.1
done
sleep 0.2

worker_user="$(id -u):$(id -g)"
common=(--rm --network "$network" --read-only --tmpfs /tmp:rw,exec,nosuid,nodev,size=64m
  --cap-drop ALL --security-opt no-new-privileges --user "$worker_user"
  -e MAINTAINER_MODEL_ENDPOINT=http://model-supervisor:8082/v1
  -e MAINTAINER_MAX_ARTIFACT_BYTES=2097152)

"${docker_command[@]}" run "${common[@]}" -e MAINTAINER_RUN_ID=run_implementation \
  -v "$acceptance_root/worktree:/workspace:rw" \
  -v "$acceptance_root/implementation-artifacts:/artifacts:rw" \
  -v "$repo_root/test/fixtures/agents/implementation-task.json:/inputs/00:ro" \
  local/codemaintainer-implementation-agent:0.1.0-dev

grep -q 'return a + b' "$acceptance_root/worktree/answer.go"
grep -q 'implementation_result' "$acceptance_root/implementation-artifacts/run_implementation.manifest.json"

"${docker_command[@]}" run "${common[@]}" -e MAINTAINER_RUN_ID=run_qc_0 \
  -v "$acceptance_root/worktree:/workspace:ro" -v "$acceptance_root/qc0-artifacts:/artifacts:rw" \
  -v "$repo_root/test/fixtures/agents/qc-cycle-0.json:/inputs/00:ro" \
  local/codemaintainer-qc-agent:0.1.0-dev
grep -q 'blocking_findings' "$acceptance_root/qc0-artifacts/run_qc_0/qc_report"

"${docker_command[@]}" run "${common[@]}" -e MAINTAINER_RUN_ID=run_qc_1 \
  -v "$acceptance_root/worktree:/workspace:ro" -v "$acceptance_root/qc1-artifacts:/artifacts:rw" \
  -v "$repo_root/test/fixtures/agents/qc-cycle-1.json:/inputs/00:ro" \
  local/codemaintainer-qc-agent:0.1.0-dev
grep -q '"verdict":"pass"' "$acceptance_root/qc1-artifacts/run_qc_1/qc_report"

[[ "$("${docker_command[@]}" network inspect --format '{{.Internal}}' "$network")" == true ]]
printf 'Agent image acceptance passed.\n'
