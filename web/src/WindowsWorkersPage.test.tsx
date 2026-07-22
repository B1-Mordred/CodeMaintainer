import axe from "axe-core";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WindowsWorkersPage } from "./WindowsWorkersPage";
import { setCSRFToken } from "./api/client";

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const profile = {
  id: "windows-simulator", name: "Windows worker simulator", mode: "simulator", endpoint: "simulator://windows-worker",
  endpoint_allowlist: ["simulator://windows-worker"], credential_status: "not_required", health: "simulated", capacity: 2,
  vm_template_id: "windows-2022-sim-v1", toolchains: { dotnet: "8.0.302", powershell: "7.4.4" },
  allowed_job_types: ["dotnet_restore_build_test", "powershell_pester"], timeout_seconds: 1800,
  simulator_profile_ids: ["hamilton-sim-v1", "instrument-sim-v1"], artifact_retention_days: 30,
  manual_gates: { physical_hardware: false, code_signing: false, production_vpn: false }, enabled: true,
  revision: 1, updated_at: "2026-07-22T08:30:00Z",
} as const;
const result = { run_id: "winrun-fixture", profile_id: profile.id, project_id: "owner-repo", job_id: "console-fixture", job_type: "dotnet_restore_build_test", input_sha256: "c".repeat(64), state: "completed", checks: [{ id: "restore-locked", state: "passed", summary: "fixture", duration_ms: 10 }], artifacts: [], idempotency_key: "console-fixture", replay: false, started_at: "2026-07-22T08:31:00Z", completed_at: "2026-07-22T08:31:01Z" } as const;

describe("WindowsWorkersPage", () => {
  const requests: Request[] = [];
  beforeEach(() => {
    setCSRFToken("csrf-token-with-at-least-thirty-two-characters");
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const request = input instanceof Request ? input : new Request(input); requests.push(request.clone());
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/windows-workers/profiles") return json({ schema_version: 1, approved_job_types: profile.allowed_job_types, items: [profile] });
      if (path.endsWith("/actions/probe")) return json({ profile_id: profile.id, ready: true, mode: "simulator", health: "healthy", capacity: 2, vm_template_id: profile.vm_template_id, toolchains: profile.toolchains, problems: [], checked_at: "2026-07-22T08:32:00Z" });
      if (path.endsWith("/runs") && request.method === "GET") return json({ items: [] });
      if (path.endsWith("/runs") && request.method === "POST") return json(result);
      if (path.endsWith("/windows-simulator") && request.method === "PUT") return json({ ...profile, revision: 2 });
      return json({ error: { code: "unmocked", message: `${request.method} ${path}` } }, 404);
    }));
  });
  afterEach(() => { cleanup(); requests.length = 0; vi.unstubAllGlobals(); });

  it("keeps Windows operations closed, simulator-first, and accessible", async () => {
    const { container } = render(<WindowsWorkersPage/>);
    expect(await screen.findByDisplayValue("windows-2022-sim-v1")).toBeInTheDocument();
    expect(screen.queryByLabelText(/Credential reference/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));
    expect(await screen.findByText("Structured worker probe completed.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Run fixture" }));
    expect(await screen.findByText(/completed with 1 structured checks/)).toBeInTheDocument();
    const run = requests.find((request) => request.method === "POST" && new URL(request.url).pathname.endsWith("/runs"));
    const body = await run?.clone().json();
    expect(body).toMatchObject({ job_type: "dotnet_restore_build_test", input: { repository_sha: expect.stringMatching(/^[a-f0-9]{40}$/) } });
    expect(body).not.toHaveProperty("command"); expect(body).not.toHaveProperty("script"); expect(body).not.toHaveProperty("environment");
    await waitFor(async () => expect((await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } })).violations).toEqual([]));
  });
});
