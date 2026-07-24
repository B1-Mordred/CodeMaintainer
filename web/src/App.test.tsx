import axe from "axe-core";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

const status = { status: "healthy", profile: "mock", components: { controller: "healthy" } };
const jobs = { items: [{ id: "job_fixture", repository: "owner/repo", state: "queued", updated_at: "2026-07-20T09:00:00Z" }] };
const providerStatus = {
  families: ["local_llamacpp", "openai_responses"],
  providers: [
    { id: "local-llamacpp", schema_version: 1, interface_family: "local_llamacpp", display_name: "Local llama.cpp supervisor", trust_tier: "local", remote: false, enabled: true, approved_data_classes: ["task_metadata", "candidate_diff"], credential_configured: false, operator_assertions: ["local-only supervisor profile"], version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" },
    { id: "fake-openai-responses", schema_version: 1, interface_family: "openai_responses", display_name: "CI fake OpenAI Responses", trust_tier: "approved_private", remote: true, enabled: false, approved_data_classes: ["task_metadata"], credential_configured: false, operator_assertions: ["protocol fake"], version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" },
  ],
  endpoints: [],
  models: [],
  routes: [{ id: "local-quality-default", role: "implementation", preference: "local_first", ordered_model_ids: ["local-implementation"], allowed_data_classes: ["task_metadata", "candidate_diff"], max_tokens_per_request: 32768, max_cost_usd: 0, retry_budget: 2, fallback_policy: "same_trust_or_stricter", batch_policy: "disabled", enabled: true, version: 1, created_at: "2026-07-24T04:00:00Z", updated_at: "2026-07-24T04:00:00Z" }],
  recent_egress_manifests: [],
  remote_enabled_by_default: false,
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
  if (path === "/api/v1/projects") return jsonResponse({ items: [] });
  if (path === "/api/v1/models") return jsonResponse({ items: [], status: { state: "unloaded", profile_id: "", memory_bytes: 0, prompt_tokens_second: 0, decode_tokens_second: 0 } });
  if (path === "/api/v1/model-providers/status") return jsonResponse(providerStatus);
  if (path === "/api/v1/model-providers/routes/simulations") return jsonResponse(routeDecision);
  if (path === "/api/v1/jobs/job_fixture") return jsonResponse({ ...jobs.items[0], transitions: [], phases: [], findings: [] });
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
    expect(await screen.findByText("CI fake OpenAI Responses")).toBeInTheDocument();
    expect(await screen.findByText("Remote provider")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Simulate provider route" }));
    expect(await screen.findByText("local-implementation")).toBeInTheDocument();
    expect(screen.getByText("local-quality-default")).toBeInTheDocument();
    const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    expect(result.violations).toEqual([]);
  });
});
