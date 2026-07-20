#!/usr/bin/env bash
set -euo pipefail

base_url="${1:-http://127.0.0.1:8080}"
session_file="${MAINTAINER_SESSION_FILE:-/src/.data/cli/session.json}"
output_root="${PLAYWRIGHT_OUTPUT_DIR:-/src/output/playwright}"
if command -v playwright-cli >/dev/null 2>&1; then
  cli=(playwright-cli)
else
  cli=(npx --yes --package @playwright/cli@0.1.17 playwright-cli)
fi
pw=("${cli[@]}" --config test/e2e/playwright-cli.json -s=maintainer-ui)
session_pw=("${cli[@]}" -s=maintainer-ui)

mkdir -p "$output_root"
session_token="$(node -e 'const fs=require("fs"); const value=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); process.stdout.write(value.token)' "$session_file")"

"${pw[@]}" open "$base_url"
"${session_pw[@]}" cookie-set maintainer_session "$session_token" --domain 127.0.0.1 --path / --httpOnly --sameSite Lax
"${session_pw[@]}" reload
"${session_pw[@]}" snapshot >"$output_root/desktop.snapshot.txt"
"${session_pw[@]}" screenshot >"$output_root/desktop.screenshot.txt"
"${session_pw[@]}" resize 390 844
"${session_pw[@]}" snapshot >"$output_root/mobile.snapshot.txt"
"${session_pw[@]}" screenshot >"$output_root/mobile.screenshot.txt"
"${session_pw[@]}" close

grep -q 'Overview' "$output_root/desktop.snapshot.txt"
grep -q 'Maintenance jobs\|Queue and recent jobs' "$output_root/desktop.snapshot.txt"
grep -q 'Primary navigation' "$output_root/mobile.snapshot.txt"
