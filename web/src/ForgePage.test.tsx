import axe from "axe-core";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ForgePage } from "./ForgePage";
import { setCSRFToken } from "./api/client";

const jsonResponse = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const project = { id: "gitlab-project", provider: "gitlab", repository: "owner/repo", default_branch: "main", enabled: true, created_at: "2026-07-22T01:00:00Z", updated_at: "2026-07-22T01:00:00Z" };
const profile = {
  project_id: project.id, provider: "gitlab", endpoint: "https://gitlab.example.test", endpoint_allowlist: ["https://gitlab.example.test"], repository: project.repository,
  credential_reference: "gitbridge-secret:gitlab-main", credential_status: "configured", webhook_status: "polling", sync_direction: "pull", polling_minutes: 5,
  branch_convention: "maintainer/{job_id}", change_request_convention: "draft", label_mapping: { defect: "bug" }, ci_artifact_policy: "metadata_only",
  release_policy: "observe", submodules_enabled: true, enabled: true, revision: 2, updated_at: "2026-07-22T01:00:00Z",
} as const;
const syncRun = { id: "forgesync_fixture", project_id: project.id, provider: "gitlab", input_cursor: "", output_cursor: "abc", idempotency_key: "console-fixture", state: "complete", objects: 1, partial: false, unsupported: [], replay: false, created_at: "2026-07-22T01:02:00Z" };
const object = { project_id: project.id, provider: "gitlab", kind: "issue", external_id: "17", title: "Untrusted issue title", state: "opened", provider_metadata: { source: "fixture" }, updated_at: "2026-07-22T01:01:00Z" };

describe("ForgePage", () => {
  const requests: Request[] = [];
  beforeEach(() => {
    setCSRFToken("csrf-token-with-at-least-thirty-two-characters");
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const request = input instanceof Request ? input : new Request(input);
      requests.push(request.clone());
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/projects") return jsonResponse({ items: [project] });
      if (path === "/api/v1/forges/profiles") return jsonResponse({ schema_version: 1, items: [profile] });
      if (path.endsWith("/forge-sync-runs")) return jsonResponse({ items: [syncRun] });
      if (path.endsWith("/forge-objects")) return jsonResponse({ items: [object] });
      if (path.endsWith("/forge-profile") && request.method === "PUT") return jsonResponse({ ...profile, revision: 3 });
      if (path.endsWith("/actions/probe")) return jsonResponse({ project_id: project.id, provider: "gitlab", ready: true, credential_status: "configured", webhook_status: "polling", capabilities: [{ feature: "issue", status: "supported" }, { feature: "discussion", status: "operator_only", reason: "bounded expansion disabled" }], problems: [], checked_at: "2026-07-22T01:03:00Z" });
      if (path.endsWith("/actions/sync")) return jsonResponse(syncRun);
      return jsonResponse({ error: { code: "unmocked", message: `${request.method} ${path}` } }, 404);
    }));
  });

  afterEach(() => { cleanup(); requests.length = 0; vi.unstubAllGlobals(); });

  it("keeps credential bindings write-only and exposes normalized provider diagnostics accessibly", async () => {
    const { container } = render(<ForgePage />);
    await waitFor(() => expect(screen.getByLabelText(/^EndpointMust exactly match/)).toHaveValue("https://gitlab.example.test"));
    const credential = screen.getByLabelText(/Credential reference \(write only\)/);
    expect(credential).toHaveValue("");
    expect(screen.queryByDisplayValue(profile.credential_reference)).not.toBeInTheDocument();
    expect(await screen.findByText("Untrusted issue title")).toBeInTheDocument();

    fireEvent.change(credential, { target: { value: "gitbridge-secret:rotated" } });
    fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
    expect(await screen.findByText(`Saved revision 3 for ${project.repository}.`)).toBeInTheDocument();
    const saveRequest = requests.find((request) => request.method === "PUT");
    expect(saveRequest?.headers.get("X-CSRF-Token")).toBe("csrf-token-with-at-least-thirty-two-characters");
    expect(await saveRequest?.clone().json()).toMatchObject({ credential_reference: "gitbridge-secret:rotated", expected_revision: 2, provider: "gitlab" });

    const probeButton = screen.getByRole("button", { name: "Test connection" });
    expect(probeButton).toBeEnabled();
    fireEvent.click(probeButton);
    await waitFor(() => expect(requests.some((request) => new URL(request.url).pathname.endsWith("/actions/probe"))).toBe(true));
    expect(await screen.findByText(/Connection probe completed/)).toBeInTheDocument();
    expect(await screen.findByText("bounded expansion disabled")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Sync inventory" }));
    expect(await screen.findByText(/Sync complete/)).toBeInTheDocument();

    await waitFor(async () => {
      const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
      expect(result.violations).toEqual([]);
    });
  });
});
