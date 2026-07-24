import axe from "axe-core";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConfigurationPage } from "./ConfigurationPage";
import { setCSRFToken } from "./api/client";

const descriptor = {
  key: "workflow.max_review_cycles",
  namespace: "workflow",
  schema_version: 1,
  value_kind: "integer",
  json_schema: { type: "integer", minimum: 0, maximum: 8 },
  ui: { label: "Maximum review cycles", help: "Bounds deterministic implementation and QC repair loops.", group: "Workflow", order: 10, widget: "number", documentation_link: "docs/operator.md", advanced: false },
  permitted_scopes: ["system", "capability_pack", "project", "environment", "job_template", "job_override"],
  default: 2,
  recommended: 2,
  secret: false,
  required_permission: "config.write",
  apply: "new_jobs",
  exportable: true,
  importable: true,
  migration: "increment-1-system-document",
  audit_redaction: "plain",
  bootstrap_controlled: false,
};

const waiverDescriptor = {
  ...descriptor,
  key: "qc.human_waiver_enabled",
  namespace: "qc",
  value_kind: "boolean",
  json_schema: { type: "boolean" },
  ui: { ...descriptor.ui, label: "Allow human waivers", help: "Allows audited policy waivers only while rationale policy remains enabled.", group: "QC", widget: "checkbox" },
  default: true,
  recommended: true,
  dependencies: [{ key: "qc.waiver_rationale_required", operator: "equals", value: true, when_value: true, message: "Human waivers require audited rationale policy to remain enabled." }],
};
const rationaleDescriptor = {
  ...waiverDescriptor,
  key: "qc.waiver_rationale_required",
  ui: { ...waiverDescriptor.ui, label: "Require waiver rationale", help: "Requires a bounded audited rationale for every waiver." },
  dependencies: [],
};

const state = { scope: { kind: "system" }, version: 1, revision_id: "configreg_bootstrap", values: [{ key: descriptor.key, scope: { kind: "system" }, value: 2, configured: true, secret: false, version: 1, revision_id: "configreg_bootstrap", updated_at: "2026-07-20T21:00:00Z" }], updated_at: "2026-07-20T21:00:00Z" };
const effective = { schema_version: 1, scopes: [{ kind: "system" }], values: {
  [descriptor.key]: { key: descriptor.key, value: 2, configured: true, source_scope: { kind: "system" }, source_revision: "configreg_bootstrap", contributions: [], apply: "new_jobs", secret: false },
  [waiverDescriptor.key]: { key: waiverDescriptor.key, value: true, configured: true, source_scope: { kind: "built_in" }, source_revision: "built-in", contributions: [], apply: "new_jobs", secret: false },
  [rationaleDescriptor.key]: { key: rationaleDescriptor.key, value: true, configured: true, source_scope: { kind: "built_in" }, source_revision: "built-in", contributions: [], apply: "new_jobs", secret: false },
} };
const validation = { valid: true, issues: [], apply_modes: ["new_jobs"], requires_reauthentication: false };
const baseDraft = { id: "configdraft_fixture", scope: { kind: "system" }, operation: "apply", state: "draft", base_scope_version: 1, version: 1, author_id: "administrator", reason: "Increase bounded review capacity", entries: [{ key: descriptor.key, value: 3, reset: false, secret: false, configured: true }], created_at: "2026-07-20T21:01:00Z", updated_at: "2026-07-20T21:01:00Z" };

const jsonResponse = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("ConfigurationPage", () => {
  const requests: Request[] = [];
  beforeEach(() => {
    setCSRFToken("csrf-token-with-at-least-thirty-two-characters");
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const request = input instanceof Request ? input : new Request(input);
      requests.push(request.clone());
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/config/descriptors") return jsonResponse({ schema_version: 1, items: [descriptor, waiverDescriptor, rationaleDescriptor] });
      if (path === "/api/v1/config/values") return jsonResponse(state);
      if (path === "/api/v1/config/effective") return jsonResponse(effective);
      if (path === "/api/v1/config/drafts" && request.method === "GET") return jsonResponse({ items: [] });
      if (path === "/api/v1/config/registry-revisions") return jsonResponse({ items: [] });
      if (path === "/api/v1/config/prerequisites") return jsonResponse({ items: [] });
      if (path === "/api/v1/config/drafts" && request.method === "POST") return jsonResponse({ draft: baseDraft, validation }, 201);
      if (path === `/api/v1/config/drafts/${baseDraft.id}/checks`) return jsonResponse({ items: [] });
      if (path === `/api/v1/config/drafts/${baseDraft.id}/actions/review`) return jsonResponse({ draft: { ...baseDraft, state: "reviewed", version: 2, reviewer_id: "administrator" }, validation });
      if (path === `/api/v1/config/drafts/${baseDraft.id}/actions/apply`) return jsonResponse({ revision: { id: "configreg_applied", sequence: 2, scope: { kind: "system" }, scope_version: 2, actor_id: "administrator", actor_role: "administrator", operation: "apply", reason: baseDraft.reason, entries: [], created_at: "2026-07-20T21:02:00Z" }, scope: { ...state, version: 2 }, validation }, 201);
      return jsonResponse({ error: { code: "unmocked", message: `${request.method} ${path}` } }, 404);
    }));
  });

  afterEach(() => { cleanup(); requests.length = 0; vi.unstubAllGlobals(); window.history.pushState(null, "", "/"); });

  it("creates, reviews, and applies a typed ETag-bound draft accessibly", async () => {
    const { container } = render(<ConfigurationPage expert={false} />);
    const field = await screen.findByLabelText("Configured value");
    expect(field).toHaveValue(2);
    expect(screen.getAllByText("System").length).toBeGreaterThan(0);
    fireEvent.change(field, { target: { value: "3" } });
    fireEvent.change(screen.getByLabelText("Audited reason"), { target: { value: baseDraft.reason } });
    fireEvent.click(screen.getByRole("button", { name: "Create draft" }));
    expect(await screen.findByText(baseDraft.id)).toBeInTheDocument();

    const create = requests.find((request) => request.method === "POST" && new URL(request.url).pathname === "/api/v1/config/drafts");
    expect(create?.headers.get("If-Match")).toBe('"config-scope-1"');
    expect(create?.headers.get("X-CSRF-Token")).toBe("csrf-token-with-at-least-thirty-two-characters");
    expect(await create?.clone().json()).toMatchObject({ entries: [{ key: descriptor.key, value: 3, reset: false }] });

    fireEvent.click(screen.getByRole("button", { name: "Review exact draft" }));
    expect(await screen.findByRole("button", { name: "Apply reviewed draft" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Apply reviewed draft" }));
    expect(await screen.findByText("Applied revision configreg_applied.")).toBeInTheDocument();
    const apply = requests.find((request) => new URL(request.url).pathname.endsWith("/actions/apply"));
    expect(apply?.headers.get("If-Match")).toBe('"config-draft-2"');

    await waitFor(async () => {
      const result = await axe.run(container, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
      expect(result.violations).toEqual([]);
    });
  });

  it("previews dependencies and offers inherited and safe-default resets", async () => {
    render(<ConfigurationPage expert={false} />);
    const rationale = await screen.findByRole("checkbox", { name: "Require waiver rationale" });
    fireEvent.click(rationale);
    expect(await screen.findByText("Human waivers require audited rationale policy to remain enabled.")).toBeInTheDocument();
    expect(screen.getByText("Before / after and impact")).toBeInTheDocument();
    expect(screen.getByText("Enabled", { selector: "del" })).toBeInTheDocument();
    expect(screen.getByText("Disabled", { selector: "ins" })).toBeInTheDocument();

    const rationaleCard = rationale.closest("article");
    expect(rationaleCard).not.toBeNull();
    fireEvent.click(within(rationaleCard!).getByRole("button", { name: "Safe default" }));
    await waitFor(() => expect(screen.queryByText("Human waivers require audited rationale policy to remain enabled.")).not.toBeInTheDocument());
    fireEvent.click(within(rationaleCard!).getByRole("button", { name: "Inherited" }));
    expect(rationale).toBeChecked();
  });

  it("honors filtered dashboard deep links and exposes field consumer backlinks", async () => {
    window.history.pushState(null, "", "/#configuration?search=qc&from=quality");
    render(<ConfigurationPage expert={false} />);
    expect(await screen.findByDisplayValue("qc")).toBeInTheDocument();
    expect(await screen.findByText(/Filtered from Quality/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Return to Quality" })).toHaveAttribute("href", "#quality");
    expect(await screen.findByText("qc.human_waiver_enabled")).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "Policy and risk" }).length).toBeGreaterThan(0);
  });
});
