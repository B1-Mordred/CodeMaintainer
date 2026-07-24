#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
remote_root="$repo_root/.data/remotes"

if ! command -v git >/dev/null 2>&1; then
  printf 'Git is required to seed the credential-free mock repositories.\n' >&2
  exit 1
fi

ensure_remote_main() {
  local remote="$1"
  if [[ -d "$remote" ]]; then
    git --git-dir="$remote" rev-parse --verify refs/heads/main >/dev/null
    return 0
  fi
  if [[ -e "$remote" ]]; then
    printf 'Mock remote path exists but is not a directory: %s\n' "$remote" >&2
    exit 1
  fi
  return 1
}

seed_bare_from_worktree() {
  local source="$1"
  local remote="$2"
  local message="$3"
  if ensure_remote_main "$remote"; then
    return 0
  fi
  local fixture_root
  fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/maintainer-fixture.XXXXXX")"
  cleanup() {
    rm -rf -- "$fixture_root"
  }
  trap cleanup RETURN
  mkdir -p "$fixture_root/source"
  cp -R "$source"/. "$fixture_root/source/"
  git init -q -b main "$fixture_root/source"
  git -C "$fixture_root/source" config user.name 'CodeMaintainer Fixture'
  git -C "$fixture_root/source" config user.email 'fixture@localhost'
  git -C "$fixture_root/source" add .
  git -C "$fixture_root/source" commit -q -m "$message"
  git clone -q --bare "$fixture_root/source" "$remote"
  chmod -R u+rwX,go-rwx "$remote"
}

seed_arithmetic_remote() {
  local remote="$remote_root/fixture.git"
  if ensure_remote_main "$remote"; then
    return 0
  fi
  local fixture_root
  fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/maintainer-fixture.XXXXXX")"
  cleanup() {
    rm -rf -- "$fixture_root"
  }
  trap cleanup RETURN
  git init -q -b main "$fixture_root/source"
  git -C "$fixture_root/source" config user.name 'CodeMaintainer Fixture'
  git -C "$fixture_root/source" config user.email 'fixture@localhost'
  cat >"$fixture_root/source/go.mod" <<'EOF'
module fixture.local/arithmetic

go 1.25
EOF
  cat >"$fixture_root/source/answer.go" <<'EOF'
package answer

func Add(a, b int) int { return a - b }
EOF
  cat >"$fixture_root/source/answer_test.go" <<'EOF'
package answer

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal(Add(2, 3))
	}
}
EOF
  git -C "$fixture_root/source" add go.mod answer.go answer_test.go
  git -C "$fixture_root/source" commit -q -m 'seed arithmetic defect'
  git clone -q --bare "$fixture_root/source" "$remote"
  chmod -R u+rwX,go-rwx "$remote"
}

mkdir -p "$remote_root"
seed_arithmetic_remote
seed_bare_from_worktree "$repo_root/test/fixtures/repositories/php83" "$remote_root/php83.git" 'seed PHP 8.3 intranet fixture'
seed_bare_from_worktree "$repo_root/test/fixtures/repositories/r-statistical" "$remote_root/r-statistical.git" 'seed R statistical validation fixture'
