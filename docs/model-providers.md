# Model provider gateway

Increment 2 routes all local and optional remote inference through the
controller-owned provider gateway. Agents and untrusted runners receive only a
provider-neutral task contract. They do not receive endpoint URLs, credentials,
cloud account material, provider-specific request fields, or network authority.

## Current checkpoint

This checkpoint introduces the durable provider-gateway foundation:

- provider, endpoint, model, route, egress-manifest, and capability-probe
  records in SQLite migrations 35 and 36;
- seeded local-only llama.cpp defaults plus disabled protocol-fake remote
  provider templates for every required family: OpenAI Responses, OpenAI Chat
  Completions, OpenAI-compatible APIs, Azure OpenAI, Anthropic Messages, Gemini,
  Vertex Gemini, Bedrock Converse, and Bedrock Responses-compatible APIs;
- a provider service that selects only enabled routes and models whose data
  classes, structured-output capability, endpoint policy, trust tier, token
  budget, and cost ceiling match the request;
- fail-closed egress decisions for forbidden data classes, disabled remote
  profiles, unsafe endpoints, capability gaps, budget excess, or unsupported
  fallback;
- append-only egress manifests with destination profile, model profile, data
  classes, artifact references, redactions, estimate, retention text, decision
  reason, and manifest hash;
- deterministic fake capability probes that retain observed native API shape,
  observed model ID, capability set, request/response schema hashes, latency,
  status, errors, actor, and timestamp without contacting external providers;
- a bounded streamed event parser that normalizes provider-family chunks enough
  to reject malformed events, interleaved tool-call deltas, truncation, and
  unbounded content while retaining partial usage events for later
  reconciliation;
- REST, OpenAPI, generated TypeScript client, `maintainctl model-provider`, and
  Models-page visibility.

Remote inference remains disabled by default. The seeded fake remote profiles
exist to exercise conformance and operator workflows in CI. They are not proof
that a real account, region, retention policy, or external endpoint is approved.

## Default profile behavior

The controller seeds these records on first gateway status or simulation use:

- `local-llamacpp`: enabled, local-only, trusted for all approved data classes,
  and routed by `local-quality-default` for implementation work.
- fake remote provider templates: disabled, remote, private-trust CI fixtures
  with narrow approved data classes.
- `remote-documentation-ci-preview`: disabled route for documentation preview
  over the fake OpenAI Responses profile.

An existing project therefore remains fully local unless an authorized operator
creates or enables a remote route through the future configuration workflow and
the effective policy allows the exact data boundary.

## Endpoint safety

Endpoint profiles are validated by the controller. Remote profiles require
HTTPS, reject redirects, and cannot point at private, loopback, link-local, or
metadata addresses unless the profile is explicitly classified for that network
zone. Public profiles cannot target private addresses. Local supervisor profiles
may use loopback HTTP and are labelled as local-only.

The gateway records DNS policy, TLS mode, redirect behavior, timeout, region,
and network zone as profile data. It does not accept arbitrary provider URLs or
model arguments from a job submission or agent packet.

## Egress manifests

Before a provider route is treated as usable, the service creates a retained
egress manifest. The manifest is the operator-visible preview and evidence hash
for what would cross the model boundary:

- project and optional job identity;
- route, provider, endpoint, and model profile IDs;
- purpose;
- data classes and artifact IDs;
- deterministic redaction categories;
- byte and token estimate;
- retention statement;
- allow or deny decision and reason;
- `manifest_sha256`.

Denied simulations are retained too. This makes policy and routing failures
auditable without contacting an external provider.

Secret-bearing data classes fail closed before a route is selected. The checked
redaction inventory in `config/redaction-coverage.json` links the provider
egress manifest policy to the configuration, observability, support bundle,
memory, authentication, forge, Windows-worker, and policy-decision redaction
tests.

## Operator surfaces

REST endpoints:

- `GET /api/v1/model-providers/status`
- `POST /api/v1/model-providers/routes/simulations`
- `GET /api/v1/model-providers/egress-manifests`
- `POST /api/v1/model-providers/models/{modelID}/actions/probe`
- `GET /api/v1/model-providers/capability-probes`

CLI:

- `maintainctl model-provider status`
- `maintainctl model-provider simulate --input <json-file|->`
- `maintainctl model-provider egress [--project <project-id>]`
- `maintainctl model-provider probe <model-profile-id>`
- `maintainctl model-provider probes [--model <model-profile-id>]`

Web:

- Models page: local manifests, provider profiles, trust tiers, credential
  configured/not-configured status, provider model profiles, deterministic
  capability probes, route simulation, egress manifest preview, and recent
  manifest/probe history.

## Remaining provider milestone work

The full Increment 2 provider milestone is still open. Remaining work includes:

- real protocol adapters behind the fake conformance adapter boundary;
- write-only credential rotation workflows;
- override/revalidation history and drift pausing;
- integration of provider-native streaming, cancellation, batch/asynchronous
  resumability, and usage reconciliation into model-backed worker calls;
- OPA-bound project data-class policy and per-job approval when required;
- editable provider/endpoint/model/route workbench controls with rollback and
  export/import through the configuration registry;
- integration of gateway route snapshots into all model-backed worker calls;
- benchmark-backed local runtime optimization profiles.
