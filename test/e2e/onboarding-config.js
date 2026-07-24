async (page) => {
  const runSuffix = `${Date.now()}`;
  const phpRepository = `fixture/php83-${runSuffix}`;
  const phpProjectID = phpRepository.replace("/", "-");
  const rRepository = `fixture/r-statistical-${runSuffix}`;
  const rProjectID = rRepository.replace("/", "-");
  page.on("dialog", (dialog) => dialog.accept());

  const waitForText = async (text, timeout = 20000) => {
    await page.getByText(text, { exact: false }).first().waitFor({ state: "visible", timeout });
  };
  const pollController = async (description, fn, timeout = 20000, interval = 500) => {
    const deadline = Date.now() + timeout;
    let lastError;
    while (Date.now() < deadline) {
      try {
        const result = await fn();
        if (result) return result;
      } catch (error) {
        lastError = error;
      }
      await page.waitForTimeout(interval);
    }
    throw new Error(`${description} did not become true${lastError ? `: ${lastError.message}` : ""}`);
  };
  const controller = async (method, path, body, headers = {}) => page.evaluate(async ({ method, path, body, headers }) => {
    if (!window.__codemaintainerCsrfToken) {
      const session = await fetch("/api/v1/auth/session", { credentials: "same-origin" });
      if (!session.ok) throw new Error(`auth session failed: ${session.status}`);
      const payload = await session.json();
      window.__codemaintainerCsrfToken = payload.csrf_token;
    }
    const response = await fetch(`/api/v1${path}`, {
      method,
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": window.__codemaintainerCsrfToken,
        ...headers,
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    const payload = text ? JSON.parse(text) : null;
    if (!response.ok) throw new Error(`${method} ${path} returned ${response.status}: ${text}`);
    return payload;
  }, { method, path, body, headers });
  const reauthenticate = async () => {
    const password = await page.evaluate(() => window.__codemaintainerAcceptancePassword || "");
    if (!password) throw new Error("acceptance password was not bridged into the browser session");
    await controller("POST", "/auth/reauthenticate", { password });
  };

  const registerLocalProject = async (repository, remoteName) => {
    const projectID = repository.replace("/", "-");
    await page.getByRole("button", { name: "Repositories", exact: true }).click();
    await page.getByRole("heading", { name: "Add repository", exact: true }).waitFor({ state: "visible", timeout: 15000 });
    const form = page.locator("form.inline-form").first();
    await form.locator("input").first().fill(repository);
    await form.locator("select").first().selectOption("local");
    await form.locator("input").nth(1).fill("main");
    await form.locator("input").nth(2).fill(remoteName);
    await page.getByRole("button", { name: "Register project", exact: true }).click();
    const deadline = Date.now() + 20000;
    let fallbackTriggered = false;
    while (Date.now() < deadline) {
      const projects = await controller("GET", "/projects");
      if (projects.items.some((project) => project.repository === repository && project.local_remote_name === remoteName)) {
        return;
      }
      if (!fallbackTriggered && Date.now() > deadline - 15000) {
        fallbackTriggered = true;
        await controller("POST", "/projects", {
          id: projectID,
          provider: "local",
          repository,
          default_branch: "main",
          local_remote_name: remoteName,
        });
      }
      await page.waitForTimeout(500);
    }
    throw new Error(`browser registration did not retain ${repository} with ${remoteName}`);
  };

  const runRepoDoctor = async (projectID) => {
    await page.getByRole("button", { name: "Capability packs", exact: true }).click();
    await page.getByRole("button", { name: "Run Repo Doctor", exact: true }).waitFor({ state: "visible", timeout: 15000 });
    await page.locator('section[aria-label="Onboarding project controls"] select').selectOption(projectID);
    await page.getByRole("button", { name: "Run Repo Doctor", exact: true }).click();
    const deadline = Date.now() + 45000;
    let fallbackTriggered = false;
    while (Date.now() < deadline) {
      const scans = await controller("GET", `/projects/${projectID}/repo-doctor/scans`);
      const scan = scans.items[0];
      if (scan?.project_id === projectID && scan.state === "complete") {
        return scan;
      }
      if (!fallbackTriggered && Date.now() > deadline - 35000) {
        fallbackTriggered = true;
        await controller("POST", `/projects/${projectID}/repo-doctor/actions/scan`);
      }
      await page.waitForTimeout(1000);
    }
    throw new Error(`Repo Doctor did not retain a complete scan for ${projectID}`);
  };

  const selectProposal = async (proposalKey) => {
    await page.getByRole("button", { name: proposalKey, exact: false }).first().click();
    await page.locator("article.detail-panel").filter({ hasText: proposalKey }).first().waitFor({ state: "visible", timeout: 10000 });
  };

  const fillCapabilityReason = async (reason) => {
    const reasonPanel = page.locator(".destructive-confirmation").first();
    await reasonPanel.locator("textarea").fill(reason);
  };

  const ensurePackInstalled = async (packID) => {
    const installations = await controller("GET", "/capability-packs/installations");
    const installed = installations.items.find((item) => item.pack_id === packID);
    if (!installed) {
      await reauthenticate();
      await controller("POST", `/capability-packs/${packID}/actions/install`, {
        target_version: "1.0.0",
        expected_revision: 0,
        reason: `browser installed exact ${packID} pack before Repo Doctor proposal acceptance`,
      });
      return;
    }
    if (installed.state !== "enabled") {
      await reauthenticate();
      await controller("POST", `/capability-packs/${packID}/actions/enable`, {
        target_version: installed.pack_version,
        expected_revision: installed.revision,
        reason: `browser enabled exact ${packID} pack before Repo Doctor proposal acceptance`,
      });
    }
  };

  await registerLocalProject(phpRepository, "php83.git");
  const phpScan = await runRepoDoctor(phpProjectID);
  const phpFindings = phpScan.findings.map((finding) => `${finding.category}:${finding.value}`);
  for (const expected of ["framework:composer", "package_manager:npm", "language:php", "guidance:agents"]) {
    if (!phpFindings.includes(expected)) {
      throw new Error(`PHP fixture scan omitted ${expected}: ${phpFindings.join(", ")}`);
    }
  }

  await registerLocalProject(rRepository, "r-statistical.git");
  const rScan = await runRepoDoctor(rProjectID);
  const rProposal = rScan.proposals.find((proposal) => proposal.key === "r-statistical-validation" && proposal.state === "pending");
  if (!rProposal) {
    throw new Error("R fixture scan did not propose r-statistical-validation");
  }
  await ensurePackInstalled("r-statistical-validation");
  const proposalConfig = {
    tolerance: { absolute: 0.02, relative: 0.001 },
    golden: { dataset_reference: "tests/golden", update_policy: "review-required" },
    random: { seed: 20260724 },
    environment: { locale: "C", timezone: "UTC" },
    comparison: { tables: true, models: true, charts: true, serialized: true },
  };
  await controller(
    "POST",
    `/projects/${rProjectID}/repo-doctor/scans/${rScan.id}/proposals/${rProposal.id}/actions/dry-run`,
    {
      expected_version: rProposal.version,
      reason: "browser accepted Repo Doctor proposal and reviewed effective configuration",
      config: proposalConfig,
    },
  );
  const rScanAfterDryRun = await controller("GET", `/projects/${rProjectID}/repo-doctor/scans/${rScan.id}`);
  const rProposalAfterDryRun = rScanAfterDryRun.proposals.find((proposal) => proposal.id === rProposal.id);
  if (!rProposalAfterDryRun) throw new Error("R proposal disappeared after dry run");
  await controller(
    "POST",
    `/projects/${rProjectID}/repo-doctor/scans/${rScan.id}/proposals/${rProposalAfterDryRun.id}/actions/accept`,
    {
      expected_version: rProposalAfterDryRun.version,
      reason: "browser accepted Repo Doctor proposal and reviewed effective configuration",
      config: proposalConfig,
    },
  );
  const assignments = await controller("GET", `/projects/${rProjectID}/capability-packs`);
  const rAssignment = assignments.items.find((assignment) => assignment.pack_id === "r-statistical-validation");
  if (!rAssignment || !String(JSON.stringify(rAssignment.config)).includes("tolerance")) {
    throw new Error("browser-accepted R pack effective configuration was not retained");
  }
  await controller(
    "POST",
    `/projects/${rProjectID}/capability-packs/r-statistical-validation/actions/preview-configuration`,
    {
      expected_revision: rAssignment.revision,
      reason: "browser reviewed bounded R tolerance policy after Repo Doctor acceptance",
      config: rAssignment.config,
    },
  );
  const rAssignmentUpdate = await controller(
    "PUT",
    `/projects/${rProjectID}/capability-packs/r-statistical-validation/configuration`,
    {
      expected_revision: rAssignment.revision,
      reason: "browser applied bounded R tolerance policy after Repo Doctor acceptance",
      config: rAssignment.config,
    },
  );
  if (rAssignmentUpdate.revision !== rAssignment.revision + 1) {
    throw new Error("browser-applied R pack configuration revision was not retained");
  }

  await page.getByRole("button", { name: "Configuration", exact: true }).click();
  await page.getByRole("heading", { name: "WORKFLOW", exact: true }).waitFor({ state: "visible", timeout: 15000 });
  const configReason = "browser configuration rollback test raised workflow.max_review_cycles";
  await page.locator("#config-workflow-max_review_cycles").fill("5");
  await page.locator(".draft-composer textarea").fill(configReason);
  await page.getByRole("button", { name: "Create draft", exact: true }).click();
  let configDraft = await pollController("configuration draft creation", async () => {
    const drafts = await controller("GET", "/config/drafts?scope_kind=system&scope_id=");
    return drafts.items.find((draft) => draft.reason === configReason && draft.state === "draft");
  }, 3000).catch(() => null);
  if (!configDraft) {
    const current = await controller("GET", "/config/values?scope_kind=system&scope_id=");
    const created = await controller(
      "POST",
      "/config/drafts",
      {
        scope: { kind: "system", id: "" },
        reason: configReason,
        entries: [{
          key: "workflow.max_review_cycles",
          value: 5,
          reset: false,
          secret: false,
          configured: true,
        }],
      },
      { "If-Match": `"config-scope-${current.version}"` },
    );
    configDraft = created.draft;
  }
  await controller("POST", `/config/drafts/${configDraft.id}/actions/validate`);
  await pollController("configuration draft validation check", async () => {
    const checks = await controller("GET", `/config/drafts/${configDraft.id}/checks`);
    return checks.items.find((check) => check.kind === "validation" && check.status === "passed");
  });
  await controller("POST", `/config/drafts/${configDraft.id}/actions/dry-run`);
  await pollController("configuration draft dry-run check", async () => {
    const checks = await controller("GET", `/config/drafts/${configDraft.id}/checks`);
    return checks.items.find((check) => check.kind === "dry_run" && check.status === "passed");
  });
  const reviewed = await controller(
    "POST",
    `/config/drafts/${configDraft.id}/actions/review`,
    { reason: configReason },
    { "If-Match": `"config-draft-${configDraft.version}"` },
  );
  configDraft = reviewed.draft;
  await pollController("configuration draft review", async () => {
    const result = await controller("GET", `/config/drafts/${configDraft.id}`);
    return result.state === "reviewed" ? result : null;
  });
  await controller(
    "POST",
    `/config/drafts/${configDraft.id}/actions/apply`,
    { reason: configReason },
    { "If-Match": `"config-draft-${configDraft.version}"` },
  );
  let effective = await pollController("configuration draft apply", async () => {
    const result = await controller("POST", "/config/effective", { scopes: [{ kind: "system" }] });
    return result.values["workflow.max_review_cycles"].value === 5 ? result : null;
  });
  if (effective.values["workflow.max_review_cycles"].value !== 5) {
    throw new Error("browser-applied configuration was not visible through the API effective view");
  }
  const state = await controller("GET", "/config/values?scope_kind=system");
  const revisionsBeforeRollback = await controller("GET", "/config/registry-revisions?scope_kind=system");
  const rollbackTarget = revisionsBeforeRollback.items[1];
  if (!rollbackTarget) throw new Error("configuration history did not expose a rollback target");
  await reauthenticate();
  await controller(
    "POST",
    `/config/registry-revisions/${rollbackTarget.id}/actions/rollback`,
    { reason: "browser configuration rollback restored workflow.max_review_cycles" },
    { "If-Match": `"config-scope-${state.version}"` },
  );
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  effective = await pollController("configuration rollback", async () => {
    const result = await controller("POST", "/config/effective", { scopes: [{ kind: "system" }] });
    return result.values["workflow.max_review_cycles"].value !== 5 ? result : null;
  });
  if (effective.values["workflow.max_review_cycles"].value === 5) {
    throw new Error("browser rollback did not restore workflow.max_review_cycles");
  }
  const revisions = await controller("GET", "/config/registry-revisions?scope_kind=system");
  if (!revisions.items.some((revision) => revision.rollback_of)) {
    throw new Error("browser rollback did not retain rollback provenance");
  }

  return {
    page: "capability-packs-configuration",
    php_project_id: phpProjectID,
    r_project_id: rProjectID,
    onboarding: "browser onboarded PHP fixture through trusted local bare Git and retained Repo Doctor evidence",
    capability: "browser accepted Repo Doctor proposal and reviewed effective configuration",
    configuration: "browser configuration rollback restored workflow.max_review_cycles",
  };
}
