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
acceptance_password_file="/src/.data/secrets/acceptance-admin.password"
acceptance_password=""
if [[ -r "$acceptance_password_file" ]]; then
  acceptance_password="$(tr -d '\n' <"$acceptance_password_file")"
fi
password_bridge="$output_root/set-acceptance-password.js"
node -e '
const fs = require("fs");
const password = process.argv[1] || "";
const target = process.argv[2];
const payload = `async (page) => {
  await page.evaluate((value) => { window.__codemaintainerAcceptancePassword = value; }, ${JSON.stringify(password)});
  return { acceptance_password: ${JSON.stringify(password ? "configured" : "missing")} };
}
`;
fs.writeFileSync(target, payload, { mode: 0o600 });
' "$acceptance_password" "$password_bridge"

"${pw[@]}" open "$base_url"
"${session_pw[@]}" cookie-set maintainer_session "$session_token" --domain 127.0.0.1 --path / --httpOnly --sameSite Lax
"${session_pw[@]}" reload
"${session_pw[@]}" snapshot >"$output_root/desktop.snapshot.txt"
"${session_pw[@]}" screenshot >"$output_root/desktop.screenshot.txt"
"${session_pw[@]}" --raw run-code --filename "$password_bridge" >"$output_root/set-acceptance-password.result.json"
"${session_pw[@]}" --raw run-code --filename test/e2e/onboarding-config.js >"$output_root/onboarding-config.result.json"
grep -q 'browser accepted Repo Doctor proposal and reviewed effective configuration' "$output_root/onboarding-config.result.json"
grep -q 'browser configuration rollback restored workflow.max_review_cycles' "$output_root/onboarding-config.result.json"
"${session_pw[@]}" --raw run-code --filename test/e2e/provider-workbench.js >"$output_root/provider-workbench.result.json"
"${session_pw[@]}" snapshot >"$output_root/provider-workbench.snapshot.txt"
"${session_pw[@]}" screenshot >"$output_root/provider-workbench.screenshot.txt"
"${session_pw[@]}" resize 390 844
"${session_pw[@]}" snapshot >"$output_root/mobile.snapshot.txt"
"${session_pw[@]}" screenshot >"$output_root/mobile.screenshot.txt"
"${session_pw[@]}" close

grep -q 'Setup and health' "$output_root/desktop.snapshot.txt"
grep -q 'Administrator account' "$output_root/desktop.snapshot.txt"
grep -q 'Route profile remote-documentation-ci-preview saved at version' "$output_root/provider-workbench.result.json"
grep -q 'remote-documentation-ci-preview' "$output_root/provider-workbench.result.json"
grep -q '"job_state":"completed"' "$output_root/provider-workbench.result.json"
grep -q 'completed browser job retained fake remote provider execution manifest evidence' "$output_root/provider-workbench.result.json"
grep -q 'provider_execution_status' "$output_root/provider-workbench.snapshot.txt"
grep -q 'documentation worker task packet' "$output_root/provider-workbench.snapshot.txt"
grep -q 'Primary navigation' "$output_root/mobile.snapshot.txt"
