async (page) => {
  const approvalLabel = "I reviewed the retained egress preview before enabling a remote-capable route";
  const remoteDocumentationClasses = "task_metadata, documentation_public_source, context_packet, candidate_diff";
  const waitForText = async (text, timeout = 15000) => {
    await page.getByText(text, { exact: false }).first().waitFor({ state: "visible", timeout });
  };
  const panel = (heading) => page.locator("article.detail-panel").filter({ hasText: heading }).first();
  const controller = async (method, path, body) => page.evaluate(async ({ method, path, body }) => {
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
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    const payload = text ? JSON.parse(text) : null;
    if (!response.ok) throw new Error(`${method} ${path} returned ${response.status}: ${text}`);
    return payload;
  }, { method, path, body });
  const waitForJobState = async (jobID, wanted, timeout = 120000) => {
    const deadline = Date.now() + timeout;
    let last = "";
    while (Date.now() < deadline) {
      const detail = await controller("GET", `/jobs/${jobID}`);
      last = detail.job.state;
      if (last === wanted) return detail;
      await page.waitForTimeout(1000);
    }
    throw new Error(`job ${jobID} did not reach ${wanted}; last state ${last}`);
  };

  await page.getByRole("button", { name: "Models and agents", exact: true }).click();
  await page.getByRole("heading", { name: "Provider configuration workbench", exact: true }).waitFor({ state: "visible", timeout: 15000 });

  await page.getByRole("button", { name: "Probe capabilities for fake-remote-json", exact: true }).click();
  await waitForText("Passed capability probe retained for fake-remote-json");

  const providerPanel = panel("Provider profile");
  await providerPanel.locator("select").first().selectOption({ label: "CI fake OpenAI Responses" });
  await providerPanel.locator("input").first().waitFor({ state: "visible", timeout: 10000 });
  const providerEnabled = providerPanel.locator('input[type="checkbox"]').first();
  if (!(await providerEnabled.isChecked())) {
    await providerEnabled.check();
  }
  await providerPanel.locator("input").nth(2).fill(remoteDocumentationClasses);
  await providerPanel.locator("input").nth(4).fill("browser E2E enabled fake remote provider after retained probe");
  await providerPanel.getByRole("button", { name: /Save provider version/ }).click();
  await waitForText("Provider profile fake-openai-responses saved at version");

  const routePanel = panel("Route profile and egress approval");
  await routePanel.locator("select").first().selectOption("remote-documentation-ci-preview");
  await routePanel.locator("input").nth(1).waitFor({ state: "visible", timeout: 10000 });
  const routeEnabled = routePanel.locator('input[type="checkbox"]').nth(0);
  if (!(await routeEnabled.isChecked())) {
    await routeEnabled.check();
  }
  await routePanel.locator("input").nth(2).fill(remoteDocumentationClasses);
  await routePanel.locator("input").nth(6).fill("browser E2E enabled remote documentation route after retained egress preview review");
  await routePanel.locator("input").nth(7).fill("browser reviewed retained preview for documentation worker context_packet and candidate_diff classes");
  const approval = routePanel.locator('input[type="checkbox"]').nth(1);
  if (!(await approval.isChecked())) {
    await approval.check();
  }
  await routePanel.getByRole("button", { name: /Save route version/ }).click();
  await waitForText("Route profile remote-documentation-ci-preview saved at version");

  await routePanel.locator("select").first().selectOption("local-documentation-default");
  await routePanel.locator("input").nth(1).waitFor({ state: "visible", timeout: 10000 });
  const localDocumentationEnabled = routePanel.locator('input[type="checkbox"]').nth(0);
  if (await localDocumentationEnabled.isChecked()) {
    await localDocumentationEnabled.uncheck();
  }
  await routePanel.locator("input").nth(6).fill("browser E2E disabled local documentation route to prove fake remote job execution");
  await routePanel.getByRole("button", { name: /Save route version/ }).click();
  await waitForText("Route profile local-documentation-default saved at version");
  await routePanel.locator("select").first().selectOption("remote-documentation-ci-preview");

  const routeRequest = {
    project_id: "owner-repo",
    job_id: "browser-provider-route-e2e",
    role: "documentation",
    purpose: "browser approved fake remote documentation route",
    data_classes: ["task_metadata", "documentation_public_source", "context_packet", "candidate_diff"],
    artifact_ids: ["artifact-provider-docs"],
    estimated_bytes: 2048,
    estimated_tokens: 1024,
    requires_structured_output: true,
  };
  await page.locator("textarea").first().fill(JSON.stringify(routeRequest, null, 2));
  await page.getByRole("button", { name: "Simulate provider route", exact: true }).click();
  await waitForText("Decision hash");
  await waitForText("Egress manifest");
  const manifest = page.locator("pre").filter({ hasText: "browser approved fake remote documentation route" }).first();
  await manifest.waitFor({ state: "visible", timeout: 15000 });
  const manifestText = await manifest.textContent();
  for (const expected of ["fake-openai-responses", "fake-remote-json", "remote-documentation-ci-preview"]) {
    if (!manifestText || !manifestText.includes(expected)) {
      throw new Error(`egress manifest omitted ${expected}`);
    }
  }

  await page.getByRole("button", { name: "Repositories", exact: true }).click();
  await page.getByRole("heading", { name: "Add repository", exact: true }).waitFor({ state: "visible", timeout: 15000 });
  await page.locator("form.inline-form").first().locator("input").first().fill("fixture/arithmetic");
  await page.locator("form.inline-form").first().locator("select").first().selectOption("local");
  await page.locator("form.inline-form").first().locator("input").nth(1).fill("main");
  await page.locator("form.inline-form").first().locator("input").nth(2).fill("fixture.git");
  await page.getByRole("button", { name: "Register project", exact: true }).click();
  await waitForText("fixture/arithmetic");

  await page.getByRole("button", { name: "Jobs", exact: true }).click();
  await page.getByRole("heading", { name: "Submit maintenance task", exact: true }).waitFor({ state: "visible", timeout: 15000 });
  const jobForm = page.locator("form.job-form").first();
  await jobForm.locator("select").first().selectOption({ label: "fixture/arithmetic" });
  const taskText = `repair Add auth regression through browser provider-backed documentation route ${Date.now()}`;
  await jobForm.locator("textarea").first().fill(taskText);
  await page.getByRole("button", { name: "Queue job", exact: true }).click();
  await waitForText(taskText);
  const jobsAfterSubmit = await controller("GET", "/jobs");
  const job = jobsAfterSubmit.items.find((item) => item.task === taskText);
  if (!job) throw new Error("queued browser provider-backed job was not retained");
  const jobID = job.id;

  let detail = await waitForJobState(jobID, "awaiting_task_approval");
  await controller("POST", `/jobs/${jobID}/task-contract/actions/approve`, {
    expected_version: detail.task_contract.version,
    reason: "browser approved exact provider-backed local-fake contract",
  });
  detail = await waitForJobState(jobID, "awaiting_test_design_disposition");
  const pendingProposal = detail.test_designer_reports.flatMap((report) => report.proposals.map((proposal) => ({ report, proposal }))).find((item) => item.proposal.disposition === "pending");
  if (!pendingProposal) throw new Error("provider-backed browser job did not retain a pending Test Designer proposal");
  await controller("POST", `/test-designer-reports/${pendingProposal.report.id}/proposals/${pendingProposal.proposal.id}/actions/dispose`, {
    disposition: "accepted",
    reason: "browser accepted independent provider-backed Test Designer proposal",
  });
  detail = await waitForJobState(jobID, "awaiting_operator", 180000);
  await controller("POST", `/jobs/${jobID}/actions/approve-publication`, {
    rationale: "browser approved exact provider-backed local-fake publication after retained egress evidence inspection",
  });
  detail = await waitForJobState(jobID, "completed", 60000);

  const expertSwitch = page.getByRole("switch", { name: "Expert mode", exact: true });
  if ((await expertSwitch.getAttribute("aria-checked")) !== "true") {
    await expertSwitch.click();
  }
  await page.getByRole("button", { name: "Jobs", exact: true }).click();
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.getByRole("button", { name: jobID, exact: true }).click();
  await waitForText("Evidence traceability graph");
  await waitForText("Completed");
  const phaseText = await page.locator("details").filter({ hasText: "Commands, logs, tests, analysis, diffs, resources, and phase outcomes" }).locator("pre").first().textContent();
  for (const expected of [
    "provider_execution_status",
    "succeeded",
    "provider_execution_network_contacted",
    "false",
    "documentation worker task packet",
    "remote-documentation-ci-preview",
    "fake-openai-responses",
    "provider_egress_manifest_sha256",
  ]) {
    if (!phaseText || !phaseText.includes(expected)) {
      throw new Error(`completed provider-backed job evidence omitted ${expected}`);
    }
  }

  return {
    page: "models-agents",
    provider: "fake-openai-responses",
    route: "remote-documentation-ci-preview",
    job_id: jobID,
    job_state: detail.job.state,
    route_saved_marker: "Route profile remote-documentation-ci-preview saved at version",
    approval_label: approvalLabel,
    evidence: "completed browser job retained fake remote provider execution manifest evidence",
  };
}
