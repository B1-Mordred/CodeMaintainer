import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CapabilityPage } from "./CapabilityPage";
import { setCSRFToken } from "./api/client";

const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
const project = {
  id: "project-one",
  provider: "local",
  repository: "owner/repo",
  default_branch: "main",
  local_remote_name: "fixture.git",
  enabled: true,
  created_at: "2026-07-20T22:00:00Z",
  updated_at: "2026-07-20T22:00:00Z",
};
const manifest = {
  schema_version: 1,
  id: "php83-intranet",
  name: "PHP 8.3 intranet",
  version: "1.0.0",
  checksum_sha256: "a".repeat(64),
  description: "Evidence-driven PHP verification.",
  languages: ["php"],
  compatibility: {
    controller_constraint: ">=2.0.0",
    platforms: ["linux/amd64"],
  },
  prerequisites: [],
  detection_rules: [],
  runner_profile_ids: ["php83-verify"],
  operation_classes: ["composer-validate"],
  parser_ids: ["junit-v1"],
  policy_fragments: [],
  context_selectors: [],
  risk_rules: [],
  documentation_rules: [],
  workflow_changes: [
    {
      stage: "verify",
      operation_id: "composer-validate",
      required: true,
      description: "Validate metadata.",
    },
  ],
  ui_schema: [],
  rehearsals: [
    {
      id: "php-web-journey",
      kind: "browser",
      operation_id: "playwright-test",
      artifact_kinds: ["screenshot"],
      comparison_class: "masked-visual",
      approval_policy: "review-required",
    },
  ],
};
const rManifest = {
  ...manifest,
  id: "r-statistical-validation",
  name: "R statistical validation",
  checksum_sha256: "c".repeat(64),
  description: "Reproducible R checks and numerical golden comparisons.",
  languages: ["r"],
  ui_schema: [
    {
      key: "tolerance.absolute",
      label: "Absolute tolerance",
      kind: "number",
      default: 0.000001,
      minimum: 0,
      maximum: 1,
      help: "Maximum absolute numeric delta.",
    },
    {
      key: "golden.dataset_reference",
      label: "Golden dataset reference",
      kind: "string",
      default: "tests/golden",
      max_length: 256,
      format: "repository-reference",
      help: "Repository-relative immutable reference.",
    },
    {
      key: "golden.update_policy",
      label: "Golden baseline updates",
      kind: "enum",
      default: "review-required",
      allowed: ["review-required", "disabled"],
      help: "Explicit approval policy.",
    },
    {
      key: "random.seed",
      label: "Random seed",
      kind: "number",
      default: 1,
      minimum: 0,
      maximum: 2147483647,
      help: "Pinned deterministic seed.",
    },
    {
      key: "environment.locale",
      label: "Locale",
      kind: "enum",
      default: "C",
      allowed: ["C", "en_US.UTF-8"],
      help: "Trusted locale.",
    },
    {
      key: "environment.timezone",
      label: "Time zone",
      kind: "enum",
      default: "UTC",
      allowed: ["UTC"],
      help: "Trusted time zone.",
    },
    {
      key: "comparison.tables",
      label: "Compare tables",
      kind: "boolean",
      default: true,
      help: "Render table comparison evidence.",
    },
  ],
  rehearsals: [
    {
      id: "r-reference-results",
      kind: "statistical",
      operation_id: "r-golden-compare",
      artifact_kinds: ["table-diff"],
      comparison_class: "numeric-tolerance",
      approval_policy: "review-required",
    },
  ],
};
const securityManifest = {
  ...manifest,
  id: "sbom-fmea-security",
  name: "SBOM, FMEA, and security",
  checksum_sha256: "d".repeat(64),
  description: "Selectable security evidence.",
  languages: ["mixed"],
  ui_schema: [
    {
      key: "scanner.syft",
      label: "Syft SBOM",
      kind: "boolean",
      default: true,
      help: "Generate a pinned SBOM.",
    },
    {
      key: "database.profile",
      label: "Scanner database profile",
      kind: "string",
      default: "offline-current",
      max_length: 128,
      format: "identifier",
      help: "Registered database profile.",
    },
    {
      key: "threshold.severity",
      label: "Release severity",
      kind: "enum",
      default: "high",
      allowed: ["medium", "high", "critical"],
      help: "Release gate threshold.",
    },
    {
      key: "threshold.confidence",
      label: "Minimum confidence",
      kind: "number",
      default: 0.8,
      minimum: 0,
      maximum: 1,
      help: "Normalized confidence.",
    },
    {
      key: "fmea.record_set",
      label: "FMEA record set",
      kind: "string",
      default: "project-fmea",
      max_length: 128,
      format: "identifier",
      help: "Reviewed FMEA records.",
    },
    {
      key: "release.gate",
      label: "Release gate",
      kind: "enum",
      default: "block-new-high",
      allowed: ["observe", "block-new-high"],
      help: "Deterministic release behavior.",
    },
    {
      key: "sbom.baseline_reference",
      label: "SBOM baseline",
      kind: "string",
      default: "initial",
      max_length: 128,
      format: "identifier",
      help: "Approved baseline.",
    },
  ],
  rehearsals: [
    {
      id: "sbom-release-diff",
      kind: "security",
      operation_id: "sbom-diff",
      artifact_kinds: ["sbom-diff"],
      comparison_class: "structured",
      approval_policy: "release-review",
    },
  ],
};
const evidence = {
  path: "composer.json",
  observation: "trusted snapshot path and content hash",
  sha256: "b".repeat(64),
};
const proposal = {
  id: "proposal_11111111111111111111111111111111",
  scan_id: "scan_11111111111111111111111111111111",
  project_id: "project-one",
  kind: "capability_pack",
  key: "php83-intranet",
  value: { pack_id: "php83-intranet", version: "1.0.0", enabled: false },
  confidence: 95,
  evidence: [evidence],
  state: "pending",
  version: 1,
};
const scan = {
  id: "scan_11111111111111111111111111111111",
  project_id: "project-one",
  repository: "owner/repo",
  revision: "abc123",
  state: "complete",
  findings: [
    {
      category: "framework",
      value: "composer",
      confidence: 95,
      evidence: [evidence],
    },
  ],
  proposals: [proposal],
  drift: [],
  files_observed: 3,
  excluded_files: 0,
  created_at: "2026-07-20T22:00:00Z",
};

describe("CapabilityPage", () => {
  const requests: Request[] = [];
  let catalogItems: unknown[] = [];
  let assignmentItems: Record<string, unknown>[] = [];
  beforeEach(() => {
    catalogItems = [manifest];
    assignmentItems = [];
    setCSRFToken("csrf-token-with-at-least-thirty-two-characters");
    vi.stubGlobal(
      "confirm",
      vi.fn(() => true),
    );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const request = input instanceof Request ? input : new Request(input);
        requests.push(request.clone());
        const path = new URL(request.url).pathname;
        if (path === "/api/v1/projects") return response({ items: [project] });
        if (path === "/api/v1/capability-packs")
          return response({ schema_version: 1, items: catalogItems });
        if (path === "/api/v1/capability-packs/installations")
          return response({ items: [] });
        if (path.endsWith("/actions/preview-configuration"))
          return response({
            changed: true,
            effective: {},
            will_modify_repository: false,
          });
        if (path.endsWith("/configuration") && request.method === "PUT")
          return response({ ...assignmentItems[0], revision: 2 });
        if (path.endsWith("/capability-packs"))
          return response({ items: assignmentItems });
        if (path.endsWith("/repo-doctor/scans"))
          return response({ items: [scan] });
        if (path.endsWith("/repo-doctor/actions/scan"))
          return response(scan, 201);
        if (path.endsWith("/actions/preview"))
          return response({
            action: "install",
            workflow_changes: manifest.workflow_changes,
            trust: {
              checksum_valid: true,
              authority_safe: true,
              compatible: true,
            },
          });
        if (path.endsWith("/actions/dry-run"))
          return response({
            proposal,
            will_modify_repository: false,
            requires_explicit_acceptance: true,
            valid: true,
          });
        return response(
          { error: { code: "unmocked", message: `${request.method} ${path}` } },
          404,
        );
      }),
    );
  });
  afterEach(() => {
    cleanup();
    requests.length = 0;
    vi.unstubAllGlobals();
  });
  it("keeps scan source trusted and previews evidence-backed proposals accessibly", async () => {
    const { container } = render(<CapabilityPage />);
    expect(
      await screen.findByText("Evidence-driven PHP verification."),
    ).toBeInTheDocument();
    expect(
      (await screen.findAllByText(/composer.json/)).length,
    ).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Run Repo Doctor" }));
    expect(
      await screen.findByText(/completed without repository changes/),
    ).toBeInTheDocument();
    const scanRequest = requests.find((item) =>
      new URL(item.url).pathname.endsWith("/repo-doctor/actions/scan"),
    );
    expect(scanRequest?.method).toBe("POST");
    expect(await scanRequest?.clone().text()).toBe("");
    fireEvent.change(screen.getByLabelText("Audited reason"), {
      target: { value: "reviewed repository evidence" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Dry run proposal" }));
    expect(
      await screen.findByText(/Proposal dry run completed/),
    ).toBeInTheDocument();
    const dryRun = requests.find((item) =>
      new URL(item.url).pathname.endsWith("/actions/dry-run"),
    );
    expect(dryRun?.headers.get("X-CSRF-Token")).toBe(
      "csrf-token-with-at-least-thirty-two-characters",
    );
    await waitFor(async () =>
      expect(
        (
          await axe.run(container, {
            runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] },
          })
        ).violations,
      ).toEqual([]),
    );
  });
  it("uses typed R and security editors for durable controller-validated assignment configuration", async () => {
    catalogItems = [rManifest, securityManifest];
    assignmentItems = [
      {
        project_id: "project-one",
        pack_id: "r-statistical-validation",
        pack_version: "1.0.0",
        enabled: true,
        config: {
          tolerance: { absolute: 0.01 },
          golden: {
            dataset_reference: "tests/golden",
            update_policy: "review-required",
          },
          random: { seed: 41 },
          environment: { locale: "C", timezone: "UTC" },
          comparison: { tables: true },
        },
        revision: 1,
        updated_at: "2026-07-22T00:00:00Z",
      },
      {
        project_id: "project-one",
        pack_id: "sbom-fmea-security",
        pack_version: "1.0.0",
        enabled: true,
        config: {
          scanner: { syft: true },
          database: { profile: "offline-current" },
          threshold: { severity: "high", confidence: 0.8 },
          fmea: { record_set: "project-fmea" },
          release: { gate: "block-new-high" },
          sbom: { baseline_reference: "initial" },
        },
        revision: 1,
        updated_at: "2026-07-22T00:00:00Z",
      },
    ];
    const { container } = render(<CapabilityPage />);
    fireEvent.click(
      await screen.findByRole("button", { name: /R statistical validation/ }),
    );
    expect(
      await screen.findByText(
        "Reproducible R checks and numerical golden comparisons.",
      ),
    ).toBeInTheDocument();
    expect(await screen.findByLabelText(/Absolute tolerance/)).toHaveValue(
      0.01,
    );
    expect(screen.getByLabelText(/Golden dataset reference/)).toHaveValue(
      "tests/golden",
    );
    expect(screen.getByLabelText(/Random seed/)).toHaveValue(41);
    expect(
      screen.getByLabelText("R statistical comparison profile"),
    ).toHaveTextContent("review-required");
    fireEvent.change(screen.getByLabelText(/Absolute tolerance/), {
      target: { value: "0.02" },
    });
    fireEvent.change(screen.getByLabelText("Audited reason"), {
      target: { value: "reviewed numerical profile" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Preview configuration" }),
    );
    expect(
      await screen.findByText(/passed controller validation/),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Apply configuration" }),
    );
    expect(
      await screen.findByText(/configuration revision 2/),
    ).toBeInTheDocument();
    const update = requests.find(
      (item) =>
        item.method === "PUT" &&
        new URL(item.url).pathname.endsWith("/configuration"),
    );
    expect(await update?.clone().json()).toMatchObject({
      expected_revision: 1,
      config: { tolerance: { absolute: 0.02 } },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /SBOM, FMEA, and security/ }),
    );
    expect(await screen.findByLabelText(/Syft SBOM/)).toBeChecked();
    expect(screen.getByLabelText(/Scanner database profile/)).toHaveValue(
      "offline-current",
    );
    expect(screen.getByLabelText(/Release severity/)).toHaveValue("high");
    expect(
      screen.getByLabelText("Security and SBOM release profile"),
    ).toHaveTextContent("block-new-high");
    expect(
      screen.queryByLabelText(/configuration \(JSON\)/i),
    ).not.toBeInTheDocument();
    await waitFor(async () =>
      expect(
        (
          await axe.run(container, {
            runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] },
          })
        ).violations,
      ).toEqual([]),
    );
  });
});
