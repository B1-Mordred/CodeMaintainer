# Observability and support bundles

CodeMaintainer keeps diagnostics local by default. The controller records bounded, redacted observability facts in SQLite and exposes them through the API, `maintainctl`, and the operator console.

## Local collector

The local collector is `controller-sqlite`. Records are append-only and versioned with `schema_version: 1`.

Recorded event fields cover operational facts without capturing sensitive content:

- trace, span, and correlation IDs;
- component, event kind, event name, and severity;
- duration, queue time, retry count, and resource bytes;
- bounded redacted attributes;
- redaction count, actor, and creation time.

The current default retention window is 30 days and the sampling ratio is `1.0`. External OTLP export is disabled by default, has no configured endpoint allow-list, and storage rejects events marked as externally exported.

## Redaction boundary

Observability attributes are JSON only and are bounded to 32 KiB before and after redaction. The controller redacts keys or paths containing sensitive terms such as:

- `secret`, `token`, `password`, `api_key`, `apikey`;
- `auth`, `cookie`, `header`;
- `prompt`, `request_body`, `body`;
- `source_content`;
- `hidden_reasoning`, `reasoning`, `chain_of_thought`.

Large string values are truncated before storage. Raw prompts, request bodies, unrestricted source content, remote provider payloads, hidden reasoning, and secrets must not be placed in support bundles.

## API

All routes are authenticated under `/api/v1`.

- `GET /observability/status` returns collector settings, redaction policy, recent events, and recent support bundle manifests.
- `GET /observability/events?component=<name>&limit=<n>` lists retained redacted events.
- `POST /observability/events` records one event. The controller redacts attributes before storage.
- `GET /observability/support-bundles?limit=<n>` lists retained support bundle manifests.
- `POST /observability/support-bundles` creates a bounded support bundle manifest from recent telemetry.

Support bundle creation requires a reason and may specify section names. The default sections are `system_status`, `recent_telemetry`, `configuration_summary`, and `support_manifest`.

## CLI

Examples:

```sh
maintainctl observability status
maintainctl observability events --component controller
maintainctl observability record --input event.json
maintainctl observability support-bundles
maintainctl observability create-support-bundle --reason "debug slow verification"
```

`record --input -` reads the event JSON from standard input.

## Operator console

The Observability page shows:

- local collector, retention, sampling, external OTLP status, event count, support bundle count, and redaction count;
- redaction and export policy;
- recent telemetry timeline with duration, queue time, retries, resource bytes, and redaction counts;
- support bundle manifest and bundle hashes;
- expert-mode views of already-redacted event attributes and bundle manifests.

Support bundle records are manifest-only local export records. An operator must separately review and transfer any exported diagnostic package.
