async (page) => {
  const approvalLabel = "I reviewed the retained egress preview before enabling a remote-capable route";
  const waitForText = async (text, timeout = 15000) => {
    await page.getByText(text, { exact: false }).first().waitFor({ state: "visible", timeout });
  };
  const panel = (heading) => page.locator("article.detail-panel").filter({ hasText: heading }).first();

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
  await routePanel.locator("input").nth(6).fill("browser E2E enabled remote documentation route after retained egress preview review");
  await routePanel.locator("input").nth(7).fill("browser reviewed retained preview for task_metadata and documentation_public_source only");
  const approval = routePanel.locator('input[type="checkbox"]').nth(1);
  if (!(await approval.isChecked())) {
    await approval.check();
  }
  await routePanel.getByRole("button", { name: /Save route version/ }).click();
  await waitForText("Route profile remote-documentation-ci-preview saved at version");

  const routeRequest = {
    project_id: "owner-repo",
    job_id: "browser-provider-route-e2e",
    role: "documentation",
    purpose: "browser approved fake remote documentation route",
    data_classes: ["task_metadata", "documentation_public_source"],
    artifact_ids: ["artifact-provider-docs"],
    estimated_bytes: 2048,
    estimated_tokens: 1024,
    requires_structured_output: true,
  };
  await page.locator("textarea").first().fill(JSON.stringify(routeRequest, null, 2));
  await page.getByRole("button", { name: "Simulate provider route", exact: true }).click();
  await waitForText("remote-documentation-ci-preview");
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

  return {
    page: "models-agents",
    provider: "fake-openai-responses",
    route: "remote-documentation-ci-preview",
    job_id: "browser-provider-route-e2e",
    route_saved_marker: "Route profile remote-documentation-ci-preview saved at version",
    approval_label: approvalLabel,
    evidence: "retained egress manifest visible after browser route simulation",
  };
}
