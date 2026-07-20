import axe from "axe-core";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

const status = { status: "healthy", profile: "mock", components: { controller: "healthy" } };
const jobs = { items: [{ id: "job_fixture", repository: "owner/repo", state: "queued", updated_at: "2026-07-20T09:00:00Z" }] };
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
});
