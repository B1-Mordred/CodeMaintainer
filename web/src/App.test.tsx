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

describe("App", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ bootstrapped: true, authentication_enabled: true }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(session), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(status), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(jobs), { status: 200 })));
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
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ bootstrapped: false, authentication_enabled: true }), { status: 200 })));
    const { container } = render(<App />);
    expect(await screen.findByRole("heading", { name: "Create the local administrator" })).toBeInTheDocument();
    const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    expect(result.violations).toEqual([]);
  });
});
