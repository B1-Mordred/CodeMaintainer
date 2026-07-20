# GitHub App

GitHub integration is disabled by default. Create an App scoped only to selected repositories with repository metadata read, contents read/write, pull requests read/write, issues read, and checks/status read if CI polling is required. Do not grant organization administration or workflow mutation.

Store the App ID, installation ID, private key, and webhook secret only in protected deployment secret files/environment consumed by the Git bridge. The controller and workers never receive them. The bridge signs short-lived RS256 assertions, caches expiring installation tokens, uses temporary askpass transport without rewriting credential-free remotes, validates webhook HMAC over exact bytes, rejects delivery replays, and polls every five minutes as a fallback.

Register the project with `maintainctl repo add owner/repository --provider github --default-branch main`, run `repo sync`, and inspect credential-safe diagnostics. Publication always requires recent reauthentication plus reviewer/administrator approval bound to the exact result SHA; it creates a draft PR idempotently. Deploy keys used for Git transport do not authorize GitHub APIs.
