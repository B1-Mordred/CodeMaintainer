#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
remote="$repo_root/.data/remotes/fixture.git"

if [[ -d "$remote" ]]; then
  git --git-dir="$remote" rev-parse --verify refs/heads/main >/dev/null
  exit 0
fi
if [[ -e "$remote" ]]; then
  printf 'Mock remote path exists but is not a directory: %s\n' "$remote" >&2
  exit 1
fi
if ! command -v git >/dev/null 2>&1; then
  printf 'Git is required to seed the credential-free mock repository.\n' >&2
  exit 1
fi

fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/maintainer-fixture.XXXXXX")"
cleanup() {
  rm -rf -- "$fixture_root"
}
trap cleanup EXIT

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
