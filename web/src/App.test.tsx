import axe from "axe-core";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

const status = { status: "healthy", profile: "mock", components: { controller: "healthy" } };
const jobs = { items: [{ id: "job_fixture", project_id: "owner-repo", repository: "owner/repo", task: "verify retained evidence", state: "queued", acceptance_criteria: [], base_sha: "", result_sha: "", max_tokens: 1000, reserved_tokens: 0, max_wall_seconds: 3600, version: 1, created_at: "2026-07-20T09:00:00Z", updated_at: "2026-07-20T09:00:00Z" }] };
const providerStatus = {
  families: ["local_llamacpp", "openai_responses"],
  providers: [
    { id: "local-llamacpp", schema_version: 1, interface_family: "local_llamacpp", display_name: "Local llama.cpp supervisor", trust_tier: "local", remote: false, enabled: true, approved_data_classes: ["task_metadata", "candidate_diff"], credential_configured: false, operator_assertions: ["local-only supervisor profile"], version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" },
    { id: "fake-openai-responses", schema_version: 1, interface_family: "openai_responses", display_name: "CI fake OpenAI Responses", trust_tier: "approved_private", remote: true, enabled: false, approved_data_classes: ["task_metadata"], credential_configured: false, operator_assertions: ["protocol fake"], version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" },
  ],
  endpoints: [
    { id: "local-llamacpp-endpoint", provider_id: "local-llamacpp", base_url: "http://127.0.0.1:11434", network_zone: "local", allow_private_address: true, tls_mode: "local_http", redirect_policy: "reject", dns_policy: "loopback_only", timeout_millis: 30000, health_check_path: "/health", version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" },
    { id: "fake-openai-responses-endpoint", provider_id: "fake-openai-responses", base_url: "https://example.invalid/v1", network_zone: "public_internet", allow_private_address: false, tls_mode: "verify", redirect_policy: "reject", dns_policy: "public_only", timeout_millis: 30000, health_check_path: "/models", version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" },
  ],
  models: [
    { id: "local-implementation", provider_id: "local-llamacpp", endpoint_id: "local-llamacpp-endpoint", model_id: "local-implementation", display_name: "Local implementation model", role_eligibility: ["implementation", "quality"], capabilities: { responses_api: false, chat_completions: true, streaming: true, cancellation: true, structured_outputs: true, tool_calls: false, parallel_tool_calls: false, stable_tool_call_ids: false, system_messages: true, developer_messages: false, usage_accounting: true, reasoning_controls: false, prompt_caching: false, batch: false, asynchronous: false, model_listing: false, immutable_model_ids: true }, context_limit: 32768, output_limit: 8192, input_price_per_mtok: 0, output_price_per_mtok: 0, quality_status: "accepted_local_default", capability_override: false, version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" },
    { id: "fake-remote-json", provider_id: "fake-openai-responses", endpoint_id: "fake-openai-responses-endpoint", model_id: "gpt-5.6-fake", display_name: "CI fake OpenAI Responses JSON", role_eligibility: ["implementation"], capabilities: { responses_api: true, chat_completions: false, streaming: true, cancellation: true, structured_outputs: true, tool_calls: true, parallel_tool_calls: true, stable_tool_call_ids: true, system_messages: true, developer_messages: true, usage_accounting: true, reasoning_controls: true, prompt_caching: true, batch: true, asynchronous: true, model_listing: true, immutable_model_ids: true }, context_limit: 128000, output_limit: 16384, input_price_per_mtok: 0, output_price_per_mtok: 0, quality_status: "ci_fake_only", capability_override: false, version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" },
  ],
  routes: [{ id: "local-quality-default", role: "implementation", preference: "local_first", ordered_model_ids: ["local-implementation"], allowed_data_classes: ["task_metadata", "candidate_diff"], max_tokens_per_request: 32768, max_cost_usd: 0, retry_budget: 2, fallback_policy: "same_trust_or_stricter", batch_policy: "disabled", enabled: true, version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" }],
  recent_egress_manifests: [],
  recent_capability_probes: [],
  remote_enabled_by_default: false,
};
const capabilityProbe = {
  probe: {
    id: "provider-probe-fixture", provider_id: "fake-openai-responses", endpoint_id: "fake-openai-responses-endpoint",
    model_profile_id: "fake-remote-json", interface_family: "openai_responses", status: "passed",
    observed_model_id: "gpt-5.6-fake", native_api_shape: "openai.responses.create",
    capabilities: providerStatus.models[1].capabilities, request_schema_sha256: "b".repeat(64), response_schema_sha256: "c".repeat(64),
    latency_millis: 2, errors: [], actor_id: "operator-console", created_at: "2026-07-24T04:00:00Z",
  },
};
const routeDecision = {
  decision: {
    status: "allowed",
    reason: "selected local-implementation via local_llamacpp",
    egress_manifest: {
      id: "egress_fixture", project_id: "owner-repo", route_id: "local-quality-default", provider_id: "local-llamacpp",
      endpoint_id: "local-llamacpp-endpoint", model_profile_id: "local-implementation", purpose: "api local route",
      data_classes: ["task_metadata", "candidate_diff"], artifact_ids: [], redactions: ["secret_scan"],
      estimated_bytes: 2048, estimated_tokens: 1024, retention: "local-only; no remote retention",
      policy_decision: "allowed", decision_reason: "selected local-implementation via local_llamacpp",
      manifest_sha256: "a".repeat(64), created_at: "2026-07-24T04:00:00Z",
    },
  },
};
const session = {
  principal: {
    user: { id: "user_0123456789abcdef0123456789abcdef", username: "admin", display_name: "Administrator", role: "administrator", disabled: false, created_at: "2026-07-20T09:00:00Z", updated_at: "2026-07-20T09:00:00Z" },
    expires_at: "2026-07-20T21:00:00Z",
  },
  csrf_token: "csrf-token-with-at-least-thirty-two-characters",
};
const runtimeBenchmarks = {
  items: [{
    id: "runtime-benchmark-fixture", profile_id: "implementation", role: "implementation", model_family: "fixture",
    model_sha256: "", quantization: "fake", context_limit: 4096, threads: 0, batch: 0, ubatch: 0, numa: "",
    runtime_identity_sha256: "d".repeat(64), prompt_tokens_second: 1, decode_tokens_second: 1, duration_millis: 1,
    memory_bytes: 0, healthy: true,
    quality_fixtures: [{ id: "smoke-ok-exact", status: "passed", score: 1, threshold: 1, evidence_sha256: "e".repeat(64) }],
    quality_status: "passed", determinism_status: "passed", cache_mode: "disabled", cache_identity_sha256: "",
    experimental_features: [], recommendation: "candidate", reason: "benchmark passed smoke, quality, determinism, and identity gates; operator review is still required before activation",
    actor_id: "operator-console", created_at: "2026-07-24T04:30:00Z",
  }],
};
const evidenceGraph = {
  job_id: "job_fixture",
  project_id: "owner-repo",
  nodes: [
    { id: "evidence_node_job_job_fixture", job_id: "job_fixture", project_id: "owner-repo", kind: "job", subject_id: "job_fixture", subject_sha256: "1".repeat(64), label: "Job job_fixture", producer: "controller", metadata: { source: "job" }, metadata_sha256: "2".repeat(64), created_at: "2026-07-24T05:00:00Z" },
    { id: "evidence_node_artifact_artifact_fixture", job_id: "job_fixture", project_id: "owner-repo", kind: "artifact", subject_id: "artifact_fixture", subject_sha256: "3".repeat(64), label: "command_result artifact", producer: "verifier", metadata: { kind: "command_result", media_type: "application/json", bytes: 16 }, metadata_sha256: "4".repeat(64), created_at: "2026-07-24T05:00:00Z" },
  ],
  edges: [
    { id: "evidence_edge_fixture", job_id: "job_fixture", project_id: "owner-repo", from_node_id: "evidence_node_job_job_fixture", to_node_id: "evidence_node_artifact_artifact_fixture", relationship: "produced", reason: "artifact retained for job evidence", actor_id: "verifier", metadata: { source: "artifact_index" }, created_at: "2026-07-24T05:00:00Z" },
  ],
};
const projects = {
  items: [{ id: "owner-repo", provider: "local", repository: "owner/repo", default_branch: "main", local_remote_name: "fixture.git", enabled: true, created_at: "2026-07-20T09:00:00Z", updated_at: "2026-07-20T09:00:00Z" }],
};
const documentationPolicyProfile = {
  profile: {
    id: "documentation-policy-default",
    version: "documentation-policy-v1",
    summary: "Public API and CLI changes require source-controlled docs, render checks, and reviewer gates.",
    rules: [{
      id: "public-api-cli-docs",
      name: "Public API and CLI docs",
      description: "Require API, CLI, changelog, and troubleshooting documentation for public surfaces.",
      match: { paths: ["internal/api/**", "cmd/**"], change_classes: ["public_api", "cli"], risk_levels: ["medium", "high"], languages: ["go"], capability_packs: [], labels: [] },
      documents: ["docs/api.md", "docs/operations.md"],
      render_targets: ["html"],
      checks: ["links", "openapi"],
      reviewer_roles: ["reviewer"],
      publication_gate: true,
      source_of_truth: "git",
      publication_target: "repository",
    }],
    tool_profile: { id: "documentation-tools-default", render_targets: ["html"], checks: ["links", "anchors", "openapi"], preview_modes: ["side_by_side"] },
  },
};
const documentationSimulation = {
  simulation: {
    profile_id: "documentation-policy-default",
    profile_version: "documentation-policy-v1",
    matched_rules: documentationPolicyProfile.profile.rules,
    requirements: [{ id: "DOC-API", document: "docs/api.md", reason: "public API changed", required: true, source: "policy" }],
    checks: [{ id: "DOC-LINKS", kind: "links", target: "docs/api.md", status: "passed", summary: "links validated" }],
    render_targets: ["html"],
    reviewer_roles: ["reviewer"],
    publication_gates: ["documentation_qc"],
    source_mappings: [{ path: "internal/api/openapi.yaml", summary: "OpenAPI changed", source_hash: "1".repeat(64), documents: ["docs/api.md"] }],
    policy_summary: "Documentation required for public API and CLI surfaces.",
    publication_ready: false,
    no_documentation_required: false,
  },
};
const documentationManifest = {
  id: "docmanifest_fixture", job_id: "job_fixture", schema_version: 1, project_id: "owner-repo",
  contract_sha256: "1".repeat(64), risk_level: "medium", result_sha: "2".repeat(40),
  source_context: "documentation agent fixture", policy_version: "documentation-policy-v1",
  requirements: documentationSimulation.simulation.requirements,
  changes: documentationSimulation.simulation.source_mappings,
  checks: documentationSimulation.simulation.checks,
  unsupported_claims: [],
  edits: [{ path: "docs/observability.md", content: "updated docs", expected_sha256: "3".repeat(64) }],
  status: "changes_applied", policy_summary: "Documentation required for public API and CLI surfaces.",
  created_at: "2026-07-24T06:30:00Z",
};
const documentationFinding = {
  job_id: "job_fixture", id: "finding_doc_fixture", severity: "must_fix", category: "documentation.links",
  claim: "Documentation link validation must be retained.", location: { path: "docs/observability.md" },
  required_resolution: "Retain passing link validation evidence.", verification_method: "documentation_qc",
  status: "open", first_seen_cycle: 1, last_seen_cycle: 1, version: 1,
  created_at: "2026-07-24T06:31:00Z", updated_at: "2026-07-24T06:31:00Z",
};
const securityFinding = {
  job_id: "job_fixture", id: "finding_security_fixture", severity: "blocker", category: "security.sbom",
  claim: "SBOM release diff must be reviewed before publication.", location: { artifact: "sbom-diff" },
  required_resolution: "Review the SBOM diff or record an expiring suppression.", verification_method: "sbom-release-diff",
  status: "open", first_seen_cycle: 1, last_seen_cycle: 1, version: 1,
  created_at: "2026-07-24T06:32:00Z", updated_at: "2026-07-24T06:32:00Z",
};
const securityManifest = {
  schema_version: 1,
  id: "sbom-fmea-security",
  name: "SBOM, FMEA, and security",
  version: "1.0.0",
  checksum_sha256: "d".repeat(64),
  description: "Selectable security evidence.",
  languages: ["mixed"],
  compatibility: { controller_constraint: ">=2.0.0", platforms: ["linux/amd64"] },
  prerequisites: [],
  detection_rules: [],
  runner_profile_ids: ["security-verify"],
  operation_classes: ["syft-sbom", "sbom-diff"],
  parser_ids: ["sbom-v1"],
  policy_fragments: [],
  context_selectors: [],
  risk_rules: [],
  documentation_rules: [],
  workflow_changes: [{ stage: "verify", operation_id: "syft-sbom", required: true, description: "Generate pinned SBOM evidence." }],
  ui_schema: [{ key: "scanner.syft", label: "Syft SBOM", kind: "boolean", default: true, help: "Generate a pinned SBOM." }],
  rehearsals: [{ id: "sbom-release-diff", kind: "security", operation_id: "sbom-diff", artifact_kinds: ["sbom-diff"], comparison_class: "structured", approval_policy: "release-review" }],
};
const jobDetail = { ...jobs.items[0], transitions: [], phases: [], findings: [documentationFinding, securityFinding], documentation_manifests: [documentationManifest] };
const evaluationDatasets = {
  items: [{
    id: "evaldataset_fixture", schema_version: 1, project_id: "owner-repo", name: "Historical fixture sample",
    source_kind: "historical_range", repository: "owner/repo",
    base_revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", target_revision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    known_patch_sha256: "c".repeat(64), hidden_patch_sha256: "d".repeat(64), exclusions: ["vendor/**"],
    scoring_profile: "quality_default_v1", retention_days: 30, reproducibility_key: "e".repeat(64), metadata: {},
    actor_id: "operator-console", created_at: "2026-07-24T05:30:00Z",
  }],
};
const evaluationRuns = {
  items: [{
    id: "evalrun_fixture", schema_version: 1, dataset_id: "evaldataset_fixture", project_id: "owner-repo", status: "completed",
    profile_matrix: ["local-default", "remote-fake"],
    isolated_memory_namespace: "eval://memory/owner-repo/evaldataset_fixture/evalrun_fixture",
    isolated_cache_namespace: "eval://cache/owner-repo/evaldataset_fixture/evalrun_fixture",
    budget_seconds: 3600, concurrency: 1, scoring_profile: "quality_default_v1",
    results: [
      { profile_id: "local-default", task_completion: 0.9, test_success: 0.85, regression_rate: 0, diff_churn: 0.4, finding_precision: 0.8, finding_recall: 0.75, context_tokens: 12000, context_bytes: 48000, irrelevant_context_ratio: 0.12, wall_time_millis: 60000, cpu_time_millis: 54000, memory_bytes: 536870912, cache_effect: "isolated-cold-cache", documentation_compliance: 0.9, operator_interventions: 1, unresolved_uncertainty: 1, known_patch_hidden: true, promotion_allowed: false, promotion_blocked_reason: "historical evaluation is review-only and cannot automatically promote a model, policy, or route" },
    ],
    report_sha256: "f".repeat(64), promotion_recommendation: "review_only",
    reason: "offline deterministic evaluation simulator hid the known patch and used isolated memory/cache namespaces",
    actor_id: "operator-console", created_at: "2026-07-24T05:35:00Z",
  }],
};
const observabilityEvent = {
  id: "obsevent_fixture", schema_version: 1, trace_id: "trace_fixture_123456", span_id: "span_fixture",
  correlation_id: "trace_fixture_123456", component: "controller", kind: "span", name: "workflow.stage.verify",
  severity: "info", attributes: { stage: "verify", prompt: "[REDACTED]", authorization_header: "[REDACTED]" },
  redaction_count: 2, duration_millis: 1250, queue_millis: 45, retry_count: 1, resource_bytes: 4096,
  external_exported: false, external_endpoint: "", actor_id: "operator-console", created_at: "2026-07-24T05:50:00Z",
};
const supportBundle = {
  id: "supportbundle_fixture", schema_version: 1, status: "ready", reason: "debug slow run",
  sections: ["configuration_summary", "recent_telemetry", "support_manifest", "system_status"],
  redaction_policy: "redact secret/token/password/key/auth/cookie/header/prompt/request-body/source-content/hidden-reasoning fields before storage or export",
  manifest: { external_otlp_enabled: false, local_collector: "controller-sqlite", redactions: 2, excluded: ["secrets", "sensitive prompts", "hidden reasoning"] },
  manifest_sha256: "9".repeat(64), bundle_sha256: "8".repeat(64), bytes: 512,
  actor_id: "operator-console", created_at: "2026-07-24T05:51:00Z",
};
const observabilityStatus = {
  schema_version: 1, local_collector: "controller-sqlite", retention_days: 30, sampling_ratio: 1,
  external_otlp_enabled: false, external_otlp_allowlist: [],
  redaction_policy: supportBundle.redaction_policy,
  recent_events: [observabilityEvent], recent_support_bundles: [supportBundle],
};

const jsonResponse = (body: unknown) => new Response(JSON.stringify(body), {
  status: 200,
  headers: { "Content-Type": "application/json" },
});

const responseByPath = (input: RequestInfo | URL) => {
  const request = input instanceof Request ? input : new Request(input);
  const path = new URL(request.url).pathname;
  if (path === "/api/v1/auth/status") return jsonResponse({ bootstrapped: true, authentication_enabled: true });
  if (path === "/api/v1/auth/session") return jsonResponse(session);
  if (path === "/api/v1/system/status") return jsonResponse(status);
  if (path === "/api/v1/jobs") return jsonResponse(jobs);
  if (path === "/api/v1/projects") return jsonResponse(projects);
  if (path === "/api/v1/capability-packs") return jsonResponse({ items: [securityManifest] });
  if (path === "/api/v1/documentation/policy/profile") return jsonResponse(documentationPolicyProfile);
  if (path === "/api/v1/documentation/policy/simulations") return jsonResponse(documentationSimulation);
  if (path === "/api/v1/models") return jsonResponse({ items: [], status: { state: "unloaded", profile_id: "", memory_bytes: 0, prompt_tokens_second: 0, decode_tokens_second: 0 } });
  if (path === "/api/v1/models/runtime-benchmarks") return jsonResponse(runtimeBenchmarks);
  if (path === "/api/v1/model-providers/status") return jsonResponse(providerStatus);
  if (path === "/api/v1/model-providers/routes/simulations") return jsonResponse(routeDecision);
  if (path === "/api/v1/model-providers/models/fake-remote-json/actions/probe") return new Response(JSON.stringify(capabilityProbe), { status: 201, headers: { "Content-Type": "application/json" } });
  if (path === "/api/v1/model-providers/capability-probes") return jsonResponse({ probes: [capabilityProbe.probe] });
  if (path === "/api/v1/evaluations/datasets") return request.method === "POST" ? new Response(JSON.stringify({ dataset: evaluationDatasets.items[0] }), { status: 201, headers: { "Content-Type": "application/json" } }) : jsonResponse(evaluationDatasets);
  if (path === "/api/v1/evaluations/runs") return request.method === "POST" ? new Response(JSON.stringify({ run: evaluationRuns.items[0] }), { status: 201, headers: { "Content-Type": "application/json" } }) : jsonResponse(evaluationRuns);
  if (path === "/api/v1/observability/status") return jsonResponse(observabilityStatus);
  if (path === "/api/v1/observability/events") return request.method === "POST" ? new Response(JSON.stringify({ event: observabilityEvent }), { status: 201, headers: { "Content-Type": "application/json" } }) : jsonResponse({ items: [observabilityEvent] });
  if (path === "/api/v1/observability/support-bundles") return request.method === "POST" ? new Response(JSON.stringify({ bundle: supportBundle }), { status: 201, headers: { "Content-Type": "application/json" } }) : jsonResponse({ items: [supportBundle] });
  if (path === "/api/v1/jobs/job_fixture") return jsonResponse(jobDetail);
  if (path === "/api/v1/jobs/job_fixture/artifacts") return jsonResponse({ items: [{ id: "artifact_fixture", job_id: "job_fixture", project_id: "owner-repo", sha256: "3".repeat(64), bytes: 16, kind: "command_result", media_type: "application/json", producer: "verifier", metadata: {}, created_at: "2026-07-24T05:00:00Z" }] });
  if (path === "/api/v1/jobs/job_fixture/evidence-graph") return jsonResponse(evidenceGraph);
  return new Response(JSON.stringify({ error: { code: "unmocked", message: path } }), { status: 404, headers: { "Content-Type": "application/json" } });
};

describe("App", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn(responseByPath));
  });

  afterEach(() => vi.unstubAllGlobals());

  it("renders system state and jobs without critical accessibility violations", async () => {
    const { container } = render(<App />);
    expect(await screen.findByText("job_fixture")).toBeInTheDocument();
    expect(screen.getAllByText("Healthy", { exact: false }).length).toBeGreaterThan(0);
    await waitFor(async () => {
      const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
      expect(result.violations).toEqual([]);
    });
  });

  it("renders the one-time administrator bootstrap without critical accessibility violations", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(jsonResponse({ bootstrapped: false, authentication_enabled: true })));
    const { container } = render(<App />);
    expect(await screen.findByRole("heading", { name: "Create the local administrator" })).toBeInTheDocument();
    const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    expect(result.violations).toEqual([]);
  });

  it("exposes provider gateway profiles and egress route preview accessibly", async () => {
    const { container } = render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: /Models/i }));
    expect(await screen.findByRole("heading", { name: "Provider gateway" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Runtime benchmark lab" })).toBeInTheDocument();
    expect(await screen.findByText(/operator review is still required/)).toBeInTheDocument();
    expect((await screen.findAllByText("CI fake OpenAI Responses")).length).toBeGreaterThan(0);
    expect(await screen.findByText("Remote provider")).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Provider model probes" })).toBeInTheDocument();
    expect(await screen.findByText("CI fake OpenAI Responses JSON")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Probe capabilities for fake-remote-json" }));
    expect(await screen.findByText(/openai\.responses\.create/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Simulate provider route" }));
    expect(await screen.findByText("local-implementation")).toBeInTheDocument();
    expect(screen.getByText("local-quality-default")).toBeInTheDocument();
    const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    expect(result.violations).toEqual([]);
  });

  it("opens a job evidence traceability graph from the Jobs page", async () => {
    render(<App />);
    fireEvent.click((await screen.findAllByRole("button", { name: "Jobs" }))[0]);
    fireEvent.click((await screen.findAllByRole("button", { name: "job_fixture" }))[0]);
    expect(await screen.findByRole("heading", { name: "Evidence traceability graph" })).toBeInTheDocument();
    expect(await screen.findByText(/artifact retained for job evidence/)).toBeInTheDocument();
  });

  it("shows isolated historical evaluation reports", async () => {
    const { container } = render(<App />);
    fireEvent.click((await screen.findAllByRole("button", { name: "Evaluation" }))[0]);
    expect(await screen.findByRole("heading", { name: "Historical patch evaluation lab" })).toBeInTheDocument();
    expect((await screen.findAllByText(/eval:\/\/memory\/owner-repo/)).length).toBeGreaterThan(0);
    expect((await screen.findAllByText(/review-only/i)).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Launch evaluation run" }));
    expect(await screen.findByText(/isolated namespaces/)).toBeInTheDocument();
    const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    expect(result.violations).toEqual([]);
  });

  it("shows bounded redacted observability diagnostics and support bundles", async () => {
    const { container } = render(<App />);
    fireEvent.click((await screen.findAllByRole("button", { name: "Observability" }))[0]);
    expect(await screen.findByRole("heading", { name: "Redaction and export policy" })).toBeInTheDocument();
    expect((await screen.findAllByText("External OTLP")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("Disabled")).length).toBeGreaterThan(0);
    expect(await screen.findByText(/hidden-reasoning/)).toBeInTheDocument();
    expect(await screen.findByText("workflow.stage.verify")).toBeInTheDocument();
    expect((await screen.findAllByText(/9999999999999999/)).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Create redacted support bundle" }));
    expect(await screen.findByText(/operator export review/)).toBeInTheDocument();
    const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    expect(result.violations).toEqual([]);
  });

  it("exposes the required seventeen operational dashboard areas", async () => {
    render(<App />);
    const labels = [
      "Setup and health", "Repositories", "Capability packs", "Jobs", "Quality", "Documentation",
      "Code intelligence", "Forges", "Runners and Windows", "Models and agents", "Scheduling and resources",
      "Policy and risk", "Security and SBOM", "Evaluation", "Memory and evidence", "Observability", "Configuration",
    ];
    for (const label of labels) {
      expect((await screen.findAllByRole("button", { name: label })).length).toBeGreaterThan(0);
    }
  });

  it("shows documentation operations as a dedicated page", async () => {
    const { container } = render(<App />);
    fireEvent.click((await screen.findAllByRole("button", { name: "Documentation" }))[0]);
    expect(await screen.findByRole("heading", { name: "Documentation policy workbench" })).toBeInTheDocument();
    expect(await screen.findByText("docmanifest_fixture")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Simulate documentation impact" }));
    expect((await screen.findAllByText(/Documentation required for public API and CLI surfaces/)).length).toBeGreaterThan(0);
    const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    expect(result.violations).toEqual([]);
  });

  it("shows security and SBOM operations as a dedicated page", async () => {
    const { container } = render(<App />);
    fireEvent.click((await screen.findAllByRole("button", { name: "Security and SBOM" }))[0]);
    expect(await screen.findByRole("heading", { name: "Security and SBOM scan profiles" })).toBeInTheDocument();
    expect(await screen.findByText("SBOM, FMEA, and security")).toBeInTheDocument();
    expect((await screen.findAllByText("sbom-release-diff")).length).toBeGreaterThan(0);
    expect(await screen.findByText(/SBOM release diff must be reviewed/)).toBeInTheDocument();
    const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    expect(result.violations).toEqual([]);
  });
});
