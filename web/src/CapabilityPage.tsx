import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Boxes,
  CheckCircle2,
  FileSearch,
  RefreshCw,
  ShieldCheck,
} from "lucide-react";
import { api, getCSRFToken } from "./api/client";
import type { components } from "./api/schema";

type Project = components["schemas"]["Project"];
type Manifest = components["schemas"]["CapabilityManifest"];
type Installation = components["schemas"]["CapabilityInstallation"];
type Assignment = components["schemas"]["CapabilityAssignment"];
type Scan = components["schemas"]["RepoDoctorScan"];
type Proposal = components["schemas"]["RepoDoctorProposal"];
type UIField = Manifest["ui_schema"][number];
type PackAction =
  "install" | "enable" | "disable" | "upgrade" | "rollback" | "pin" | "unpin";
type ConfigValue = string | number | boolean;
type ConfigDocument = Record<string, unknown>;
const date = (value?: string) =>
  value ? new Date(value).toLocaleString() : "—";
const short = (value: string) => value.slice(0, 12);
const errorText = (value: unknown, fallback: string) => {
  if (value && typeof value === "object" && "error" in value) {
    const nested = (value as { error?: { message?: string } }).error;
    if (nested?.message) return nested.message;
  }
  return fallback;
};
const readConfig = (source: unknown, key: string) =>
  key
    .split(".")
    .reduce<unknown>(
      (value, part) =>
        value && typeof value === "object" && !Array.isArray(value)
          ? (value as ConfigDocument)[part]
          : undefined,
      source,
    );
const writeConfig = (
  source: ConfigDocument,
  key: string,
  value: ConfigValue,
) => {
  const result = structuredClone(source);
  let target = result;
  const parts = key.split(".");
  parts.slice(0, -1).forEach((part) => {
    const current = target[part];
    if (!current || typeof current !== "object" || Array.isArray(current))
      target[part] = {};
    target = target[part] as ConfigDocument;
  });
  target[parts.at(-1)!] = value;
  return result;
};
const configFor = (manifest?: Manifest, source?: unknown) => {
  let result: ConfigDocument = {};
  for (const field of manifest?.ui_schema ?? []) {
    const current = readConfig(source, field.key);
    result = writeConfig(
      result,
      field.key,
      (current ?? field.default) as ConfigValue,
    );
  }
  return result;
};

export function CapabilityPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState("");
  const [catalog, setCatalog] = useState<Manifest[]>([]);
  const [installations, setInstallations] = useState<Installation[]>([]);
  const [assignments, setAssignments] = useState<Assignment[]>([]);
  const [scans, setScans] = useState<Scan[]>([]);
  const [selectedPack, setSelectedPack] = useState("");
  const [selectedScanID, setSelectedScanID] = useState("");
  const [selectedProposalID, setSelectedProposalID] = useState("");
  const [reason, setReason] = useState("");
  const [password, setPassword] = useState("");
  const [proposalConfig, setProposalConfig] = useState<ConfigDocument>({});
  const [assignmentConfig, setAssignmentConfig] = useState<ConfigDocument>({});
  const [preview, setPreview] = useState<Record<string, unknown> | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const selectedManifest = useMemo(
    () => catalog.find((item) => item.id === selectedPack) ?? catalog[0],
    [catalog, selectedPack],
  );
  const selectedScan = useMemo(
    () => scans.find((item) => item.id === selectedScanID) ?? scans[0],
    [scans, selectedScanID],
  );
  const selectedProposal = useMemo(
    () =>
      selectedScan?.proposals.find((item) => item.id === selectedProposalID) ??
      selectedScan?.proposals[0],
    [selectedScan, selectedProposalID],
  );
  const installed = useMemo(
    () => installations.find((item) => item.pack_id === selectedManifest?.id),
    [installations, selectedManifest],
  );
  const selectedAssignment = useMemo(
    () => assignments.find((item) => item.pack_id === selectedManifest?.id),
    [assignments, selectedManifest],
  );
  const proposalManifest = useMemo(
    () =>
      selectedProposal?.kind === "capability_pack"
        ? catalog.find((item) => item.id === selectedProposal.key)
        : undefined,
    [catalog, selectedProposal],
  );
  const loadGlobal = useCallback(async () => {
    setLoading(true);
    const [projectResult, catalogResult, installationResult] =
      await Promise.all([
        api.GET("/projects"),
        api.GET("/capability-packs"),
        api.GET("/capability-packs/installations"),
      ]);
    setProjects(projectResult.data?.items ?? []);
    setCatalog(catalogResult.data?.items ?? []);
    setInstallations(installationResult.data?.items ?? []);
    if (!projectID && projectResult.data?.items[0])
      setProjectID(projectResult.data.items[0].id);
    if (!selectedPack && catalogResult.data?.items[0])
      setSelectedPack(catalogResult.data.items[0].id);
    if (projectResult.error || catalogResult.error || installationResult.error)
      setError("Capability catalog data could not be loaded.");
    setLoading(false);
  }, [projectID, selectedPack]);
  const loadProject = useCallback(async () => {
    if (!projectID) {
      setAssignments([]);
      setScans([]);
      return;
    }
    const path = { projectID };
    const [assignmentResult, scanResult] = await Promise.all([
      api.GET("/projects/{projectID}/capability-packs", { params: { path } }),
      api.GET("/projects/{projectID}/repo-doctor/scans", { params: { path } }),
    ]);
    setAssignments(assignmentResult.data?.items ?? []);
    setScans(scanResult.data?.items ?? []);
    if (!selectedScanID && scanResult.data?.items[0])
      setSelectedScanID(scanResult.data.items[0].id);
    if (assignmentResult.error || scanResult.error)
      setError("Project onboarding evidence could not be loaded.");
  }, [projectID, selectedScanID]);
  useEffect(() => {
    void loadGlobal();
  }, [loadGlobal]);
  useEffect(() => {
    void loadProject();
  }, [loadProject]);
  useEffect(() => {
    setAssignmentConfig(
      configFor(selectedManifest, selectedAssignment?.config),
    );
  }, [selectedManifest?.id, selectedAssignment?.revision]);
  useEffect(() => {
    setProposalConfig(
      configFor(
        proposalManifest,
        assignments.find((item) => item.pack_id === selectedProposal?.key)
          ?.config,
      ),
    );
  }, [proposalManifest?.id, selectedProposal?.id]);
  const runScan = async () => {
    if (!projectID) return;
    setError("");
    setMessage("");
    const result = await api.POST(
      "/projects/{projectID}/repo-doctor/actions/scan",
      {
        params: {
          path: { projectID },
          header: { "X-CSRF-Token": getCSRFToken() },
        },
      },
    );
    if (!result.data) {
      setError(
        errorText(
          result.error,
          "Repo Doctor could not read the trusted snapshot.",
        ),
      );
      return;
    }
    setSelectedScanID(result.data.id);
    setMessage(
      `Scan ${result.data.id} completed without repository changes; ${result.data.proposals.length} proposal(s) await review.`,
    );
    await loadProject();
  };
  const reauthenticate = async () => {
    if (!password.trim()) {
      setError(
        "Enter the current administrator password before changing pack installation state.",
      );
      return false;
    }
    const result = await api.POST("/auth/reauthenticate", {
      params: { header: { "X-CSRF-Token": getCSRFToken() } },
      body: { password },
    });
    setPassword("");
    if (!result.data) {
      setError("Reauthentication failed.");
      return false;
    }
    return true;
  };
  const previewPack = async (
    action: "install" | "upgrade" | "rollback" = "install",
  ) => {
    if (!selectedManifest) return;
    const target =
      action === "rollback"
        ? installed?.previous_version || selectedManifest.version
        : selectedManifest.version;
    const result = await api.POST(
      "/capability-packs/{packID}/actions/preview",
      {
        params: {
          path: { packID: selectedManifest.id },
          query: { action },
          header: { "X-CSRF-Token": getCSRFToken() },
        },
        body: {
          target_version: target,
          expected_revision: installed?.revision ?? 0,
          reason: reason || "read-only lifecycle preview",
        },
      },
    );
    if (!result.data) {
      setError(errorText(result.error, "Pack preview failed."));
      return;
    }
    setPreview(result.data);
    setMessage(
      "Preview completed without changing installation or project assignment state.",
    );
  };
  const transitionPack = async (action: PackAction) => {
    if (!selectedManifest || !reason.trim() || !(await reauthenticate()))
      return;
    const target =
      action === "rollback"
        ? installed?.previous_version || ""
        : selectedManifest.version;
    if (
      !window.confirm(
        `${action} trusted pack ${selectedManifest.id}@${target || installed?.pack_version}?`,
      )
    )
      return;
    const result = await api.POST(
      "/capability-packs/{packID}/actions/{action}",
      {
        params: {
          path: { packID: selectedManifest.id, action },
          header: { "X-CSRF-Token": getCSRFToken() },
        },
        body: {
          target_version: target,
          expected_revision: installed?.revision ?? 0,
          reason,
        },
      },
    );
    if (!result.data) {
      setError(errorText(result.error, "Pack transition failed."));
      return;
    }
    setMessage(
      `${result.data.installation.pack_id}@${result.data.installation.pack_version} is ${result.data.installation.state}${result.data.installation.pinned ? " and pinned" : ""}.`,
    );
    await loadGlobal();
  };
  const reviewProposal = async (action: "dry-run" | "accept" | "reject") => {
    if (!projectID || !selectedScan || !selectedProposal || !reason.trim())
      return;
    const config =
      selectedProposal.kind === "capability_pack" ? proposalConfig : {};
    const params = {
      path: {
        projectID,
        scanID: selectedScan.id,
        proposalID: selectedProposal.id,
      },
      header: { "X-CSRF-Token": getCSRFToken() },
    };
    if (action === "dry-run") {
      const result = await api.POST(
        "/projects/{projectID}/repo-doctor/scans/{scanID}/proposals/{proposalID}/actions/dry-run",
        {
          params,
          body: { expected_version: selectedProposal.version, reason, config },
        },
      );
      if (!result.data) {
        setError(errorText(result.error, "Proposal preview failed."));
        return;
      }
      setPreview(result.data);
      setMessage(
        "Proposal dry run completed; no repository, pack, or configuration state changed.",
      );
      return;
    }
    if (
      !window.confirm(
        `${action} ${selectedProposal.kind.replaceAll("_", " ")} proposal ${selectedProposal.key}?`,
      )
    )
      return;
    const result = await api.POST(
      "/projects/{projectID}/repo-doctor/scans/{scanID}/proposals/{proposalID}/actions/{action}",
      {
        params: { ...params, path: { ...params.path, action } },
        body: { expected_version: selectedProposal.version, reason, config },
      },
    );
    if (!result.data) {
      setError(errorText(result.error, "Proposal review failed."));
      return;
    }
    setMessage(
      `Proposal ${result.data.proposal.state}. ${result.data.assignment ? `Assigned exact pack ${result.data.assignment.pack_id}@${result.data.assignment.pack_version}.` : "No pack assignment was changed."}`,
    );
    await loadProject();
  };
  const previewAssignmentConfiguration = async () => {
    if (!projectID || !selectedManifest || !selectedAssignment) return;
    const result = await api.POST(
      "/projects/{projectID}/capability-packs/{packID}/actions/preview-configuration",
      {
        params: {
          path: { projectID, packID: selectedManifest.id },
          header: { "X-CSRF-Token": getCSRFToken() },
        },
        body: {
          expected_revision: selectedAssignment.revision,
          reason: reason || "preview typed pack configuration",
          config: assignmentConfig,
        },
      },
    );
    if (!result.data) {
      setError(errorText(result.error, "Pack configuration preview failed."));
      return;
    }
    setPreview(result.data);
    setMessage(
      "Typed pack configuration passed controller validation without modifying project or repository state.",
    );
  };
  const updateAssignmentConfiguration = async () => {
    if (
      !projectID ||
      !selectedManifest ||
      !selectedAssignment ||
      !reason.trim()
    )
      return;
    if (
      !window.confirm(
        `Apply revision ${selectedAssignment.revision + 1} configuration for ${selectedManifest.id}?`,
      )
    )
      return;
    const result = await api.PUT(
      "/projects/{projectID}/capability-packs/{packID}/configuration",
      {
        params: {
          path: { projectID, packID: selectedManifest.id },
          header: { "X-CSRF-Token": getCSRFToken() },
        },
        body: {
          expected_revision: selectedAssignment.revision,
          reason,
          config: assignmentConfig,
        },
      },
    );
    if (!result.data) {
      setError(errorText(result.error, "Pack configuration update failed."));
      return;
    }
    setMessage(
      `Applied validated ${result.data.pack_id} configuration revision ${result.data.revision}.`,
    );
    await loadProject();
  };
  return (
    <>
      <p className="page-intro">
        Onboard repositories from bounded trusted snapshots, inspect untrusted
        evidence, and explicitly assign checksummed declarative capability
        packs. Scans and previews never modify repository or project
        configuration.
      </p>
      <section
        className="configuration-toolbar"
        aria-label="Onboarding project controls"
      >
        <label>
          Registered project
          <select
            value={projectID}
            onChange={(event) => {
              setProjectID(event.target.value);
              setSelectedScanID("");
            }}
          >
            <option value="">Select a project</option>
            {projects.map((project) => (
              <option key={project.id} value={project.id}>
                {project.id} · {project.repository}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          disabled={!projectID || loading}
          onClick={() => void runScan()}
        >
          <FileSearch aria-hidden="true" />
          Run Repo Doctor
        </button>
        <button
          className="secondary-button"
          type="button"
          disabled={loading}
          onClick={() => void Promise.all([loadGlobal(), loadProject()])}
        >
          <RefreshCw aria-hidden="true" />
          Reload
        </button>
      </section>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {message && (
        <p className="inline-message" role="status">
          {message}
        </p>
      )}
      <div className="configuration-summary">
        <div>
          <span>Trusted pack versions</span>
          <strong>{catalog.length}</strong>
        </div>
        <div>
          <span>Installed</span>
          <strong>{installations.length}</strong>
        </div>
        <div>
          <span>Project assignments</span>
          <strong>{assignments.length}</strong>
        </div>
        <div>
          <span>Repo Doctor scans</span>
          <strong>{scans.length}</strong>
        </div>
        <div>
          <span>Pending proposals</span>
          <strong>
            {
              scans
                .flatMap((item) => item.proposals)
                .filter((item) => item.state === "pending").length
            }
          </strong>
        </div>
      </div>
      <section className="content-section">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Trust and version lifecycle</p>
            <h2>Capability pack catalog</h2>
          </div>
          <Boxes aria-hidden="true" />
        </div>
        <div className="detail-grid">
          <div className="evidence-stack" aria-label="Trusted capability packs">
            {catalog.map((manifest) => (
              <button
                type="button"
                className={
                  manifest.id === selectedManifest?.id
                    ? "draft-row active"
                    : "draft-row"
                }
                key={`${manifest.id}-${manifest.version}`}
                onClick={() => {
                  setSelectedPack(manifest.id);
                  setPreview(null);
                }}
              >
                <div>
                  <strong>{manifest.name}</strong>
                  <span>
                    {manifest.id}@{manifest.version}
                  </span>
                </div>
                <span>
                  {installations.some((item) => item.pack_id === manifest.id)
                    ? "Installed"
                    : "Available"}
                </span>
              </button>
            ))}
          </div>
          {selectedManifest && (
            <article className="detail-panel">
              <div className="resource-title">
                <ShieldCheck aria-hidden="true" />
                <div>
                  <h3>{selectedManifest.name}</h3>
                  <p>
                    <code>
                      {selectedManifest.id}@{selectedManifest.version}
                    </code>
                  </p>
                </div>
              </div>
              <p>{selectedManifest.description}</p>
              <dl>
                <div>
                  <dt>Checksum</dt>
                  <dd>
                    <code>{short(selectedManifest.checksum_sha256)}…</code>
                  </dd>
                </div>
                <div>
                  <dt>Compatibility</dt>
                  <dd>
                    {selectedManifest.compatibility.controller_constraint} ·{" "}
                    {selectedManifest.compatibility.platforms.join(", ")}
                  </dd>
                </div>
                <div>
                  <dt>Trusted profiles</dt>
                  <dd>{selectedManifest.runner_profile_ids.join(", ")}</dd>
                </div>
                <div>
                  <dt>Operation classes</dt>
                  <dd>{selectedManifest.operation_classes.length}</dd>
                </div>
                <div>
                  <dt>State</dt>
                  <dd>
                    {installed
                      ? `${installed.state} · revision ${installed.revision}${installed.pinned ? " · pinned" : ""}`
                      : "not installed"}
                  </dd>
                </div>
              </dl>
              <div className="card-actions">
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() =>
                    void previewPack(
                      installed?.previous_version ? "rollback" : "install",
                    )
                  }
                >
                  Preview workflow
                </button>
                {!installed ? (
                  <button
                    type="button"
                    onClick={() => void transitionPack("install")}
                  >
                    Install exact version
                  </button>
                ) : (
                  <>
                    <button
                      type="button"
                      onClick={() =>
                        void transitionPack(
                          installed.state === "enabled" ? "disable" : "enable",
                        )
                      }
                    >
                      {installed.state === "enabled" ? "Disable" : "Enable"}
                    </button>
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={() =>
                        void transitionPack(installed.pinned ? "unpin" : "pin")
                      }
                    >
                      {installed.pinned ? "Unpin" : "Pin version"}
                    </button>
                    {installed.previous_version && (
                      <button
                        className="danger-button"
                        type="button"
                        onClick={() => void transitionPack("rollback")}
                      >
                        Rollback to {installed.previous_version}
                      </button>
                    )}
                  </>
                )}
              </div>
              {selectedAssignment && (
                <div className="configuration-editor">
                  <div className="section-heading">
                    <div>
                      <p className="eyebrow">
                        Project assignment revision{" "}
                        {selectedAssignment.revision}
                      </p>
                      <h3>Validated effective configuration</h3>
                    </div>
                    <span>
                      {selectedAssignment.enabled ? "Enabled" : "Disabled"}
                    </span>
                  </div>
                  <CapabilityConfigEditor
                    manifest={selectedManifest}
                    config={assignmentConfig}
                    setConfig={setAssignmentConfig}
                  />
                  <PackConfigurationSummary
                    manifest={selectedManifest}
                    config={assignmentConfig}
                  />
                  <div className="card-actions">
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={() => void previewAssignmentConfiguration()}
                    >
                      Preview configuration
                    </button>
                    <button
                      type="button"
                      disabled={!reason.trim()}
                      onClick={() => void updateAssignmentConfiguration()}
                    >
                      Apply configuration
                    </button>
                  </div>
                </div>
              )}
              <details>
                <summary>Effective declarative content</summary>
                <p>
                  Only controller-registered identifiers are usable. The
                  manifest has no browser-selected image, executable, mount,
                  network, host path, capability, or environment field.
                </p>
                <pre>{JSON.stringify(selectedManifest, null, 2)}</pre>
              </details>
              <details>
                <summary>
                  Golden and rehearsal primitives (
                  {selectedManifest.rehearsals.length})
                </summary>
                <ul>
                  {selectedManifest.rehearsals.map((item) => (
                    <li key={item.id}>
                      <strong>{item.id}</strong> · {item.kind} /{" "}
                      {item.comparison_class} · {item.approval_policy}
                    </li>
                  ))}
                </ul>
              </details>
            </article>
          )}
        </div>
      </section>
      <section className="content-section">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Evidence-backed, never automatic</p>
            <h2>Repo Doctor proposal review</h2>
          </div>
          <span>
            {selectedScan ? date(selectedScan.created_at) : "No scan"}
          </span>
        </div>
        {!selectedScan ? (
          <div className="empty-state">
            <FileSearch aria-hidden="true" />
            <div>
              <strong>No onboarding scan yet</strong>
              <p>
                Run Repo Doctor to inspect the exact synchronized Git revision.
                Repository text is displayed as untrusted evidence and never
                becomes controller instruction.
              </p>
            </div>
          </div>
        ) : (
          <>
            <div className="filter-bar">
              <label>
                Scan
                <select
                  value={selectedScan.id}
                  onChange={(event) => {
                    setSelectedScanID(event.target.value);
                    setSelectedProposalID("");
                  }}
                >
                  {scans.map((scan) => (
                    <option key={scan.id} value={scan.id}>
                      {short(scan.id)} · {short(scan.revision)} ·{" "}
                      {date(scan.created_at)}
                    </option>
                  ))}
                </select>
              </label>
              <div>
                <strong>
                  {selectedScan.files_observed} observed ·{" "}
                  {selectedScan.excluded_files} excluded
                </strong>
                <p>
                  {selectedScan.drift.length
                    ? selectedScan.drift.join("; ")
                    : "No accepted-configuration drift detected."}
                </p>
              </div>
              <span>
                {
                  selectedScan.proposals.filter(
                    (item) => item.state === "pending",
                  ).length
                }{" "}
                pending
              </span>
            </div>
            <div className="detail-grid">
              <div className="evidence-stack">
                <h3>Proposals</h3>
                {selectedScan.proposals.map((proposal) => (
                  <button
                    type="button"
                    className={
                      proposal.id === selectedProposal?.id
                        ? "draft-row active"
                        : "draft-row"
                    }
                    key={proposal.id}
                    onClick={() => {
                      setSelectedProposalID(proposal.id);
                      setPreview(null);
                    }}
                  >
                    <div>
                      <strong>{proposal.key}</strong>
                      <span>
                        {proposal.kind.replaceAll("_", " ")} ·{" "}
                        {proposal.confidence}% confidence
                      </span>
                    </div>
                    <span>{proposal.state}</span>
                  </button>
                ))}
              </div>
              {selectedProposal && (
                <ProposalReview
                  proposal={selectedProposal}
                  manifest={proposalManifest}
                  assignment={assignments.find(
                    (item) => item.pack_id === selectedProposal.key,
                  )}
                  config={proposalConfig}
                  setConfig={setProposalConfig}
                  review={reviewProposal}
                />
              )}
            </div>
            <details>
              <summary>
                All detected repository findings ({selectedScan.findings.length}
                )
              </summary>
              <div className="table-scroll" tabIndex={0}>
                <table>
                  <thead>
                    <tr>
                      <th>Category</th>
                      <th>Observation</th>
                      <th>Confidence</th>
                      <th>Untrusted evidence</th>
                    </tr>
                  </thead>
                  <tbody>
                    {selectedScan.findings.map((finding, index) => (
                      <tr key={`${finding.category}-${finding.value}-${index}`}>
                        <td>{finding.category}</td>
                        <td>{finding.value}</td>
                        <td>{finding.confidence}%</td>
                        <td>
                          {finding.evidence.map((item) => (
                            <div key={item.path}>
                              <code>{item.path}</code> · {short(item.sha256)}…
                            </div>
                          ))}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </details>
          </>
        )}
      </section>
      {preview && (
        <section className="content-section" aria-live="polite">
          <div className="section-heading">
            <div>
              <p className="eyebrow">Read-only simulation</p>
              <h2>Proposed workflow diff</h2>
            </div>
            <CheckCircle2 aria-hidden="true" />
          </div>
          <pre>{JSON.stringify(preview, null, 2)}</pre>
        </section>
      )}
      <section className="content-section">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Explicit confirmation</p>
            <h2>Review credentials and reason</h2>
          </div>
        </div>
        <div className="destructive-confirmation">
          <label>
            Audited reason
            <textarea
              value={reason}
              maxLength={1000}
              onChange={(event) => setReason(event.target.value)}
              placeholder="Why this lifecycle or proposal action is appropriate"
            />
          </label>
          <label>
            Current administrator password
            <input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
            <span>
              Required only for pack install, enable, disable, upgrade,
              rollback, and pin state.
            </span>
          </label>
          <span>
            Proposal acceptance remains an explicit optimistic review.
          </span>
        </div>
      </section>
    </>
  );
}

function ProposalReview({
  proposal,
  manifest,
  assignment,
  config,
  setConfig,
  review,
}: {
  proposal: Proposal;
  manifest?: Manifest;
  assignment?: Assignment;
  config: ConfigDocument;
  setConfig: (value: ConfigDocument) => void;
  review: (action: "dry-run" | "accept" | "reject") => Promise<void>;
}) {
  return (
    <article className="detail-panel">
      <h3>{proposal.key}</h3>
      <p>
        <strong>{proposal.state}</strong> · {proposal.kind.replaceAll("_", " ")}{" "}
        · revision {proposal.version}
      </p>
      <div className="detail-grid">
        <div>
          <h4>Current accepted state</h4>
          <pre>
            {JSON.stringify(assignment ?? { assigned: false }, null, 2)}
          </pre>
        </div>
        <div>
          <h4>Proposed disabled state</h4>
          <pre>{JSON.stringify(proposal.value, null, 2)}</pre>
        </div>
      </div>
      {manifest ? (
        <>
          <h4>Controller-registered pack configuration</h4>
          <CapabilityConfigEditor
            manifest={manifest}
            config={config}
            setConfig={setConfig}
          />
          <PackConfigurationSummary manifest={manifest} config={config} />
        </>
      ) : (
        <div className="notice">
          <strong>No browser-supplied execution configuration</strong>
          <p>
            This proposal can only enable a controller-registered operation or
            protected-path rule. Repository content cannot add a command, image,
            mount, path, network, environment, or argument.
          </p>
        </div>
      )}
      <h4>Untrusted repository evidence</h4>
      <ul>
        {proposal.evidence.map((item) => (
          <li key={item.path}>
            <code>{item.path}</code> · {item.observation} · {short(item.sha256)}
            …
          </li>
        ))}
      </ul>
      {proposal.state === "pending" && (
        <div className="card-actions">
          <button
            className="secondary-button"
            type="button"
            onClick={() => void review("dry-run")}
          >
            Dry run proposal
          </button>
          <button type="button" onClick={() => void review("accept")}>
            Accept explicitly
          </button>
          <button
            className="danger-button"
            type="button"
            onClick={() => void review("reject")}
          >
            Reject
          </button>
        </div>
      )}
    </article>
  );
}

function CapabilityConfigEditor({
  manifest,
  config,
  setConfig,
}: {
  manifest: Manifest;
  config: ConfigDocument;
  setConfig: (value: ConfigDocument) => void;
}) {
  const grouped = manifest.ui_schema.reduce<Record<string, UIField[]>>(
    (result, field) => {
      const group = field.key.split(".")[0];
      (result[group] ??= []).push(field);
      return result;
    },
    {},
  );
  return (
    <div className="configuration-grid">
      {Object.entries(grouped).map(([group, fields]) => (
        <fieldset key={group}>
          <legend>{group.replaceAll("_", " ")}</legend>
          {fields.map((field) => {
            const value = readConfig(config, field.key) as ConfigValue;
            const update = (next: ConfigValue) =>
              setConfig(writeConfig(config, field.key, next));
            if (field.kind === "boolean")
              return (
                <label className="checkbox-row" key={field.key}>
                  <input
                    type="checkbox"
                    checked={Boolean(value)}
                    onChange={(event) => update(event.target.checked)}
                  />
                  <span>
                    <strong>{field.label}</strong>
                    <small>{field.help}</small>
                  </span>
                </label>
              );
            if (field.kind === "enum")
              return (
                <label key={field.key}>
                  {field.label}
                  <select
                    value={String(value)}
                    onChange={(event) => update(event.target.value)}
                  >
                    {(field.allowed ?? []).map((option) => (
                      <option value={option} key={option}>
                        {option.replaceAll("-", " ")}
                      </option>
                    ))}
                  </select>
                  <span>{field.help}</span>
                </label>
              );
            if (field.kind === "number")
              return (
                <label key={field.key}>
                  {field.label}
                  <input
                    type="number"
                    required
                    min={field.minimum}
                    max={field.maximum}
                    step="any"
                    value={typeof value === "number" ? value : ""}
                    onChange={(event) => {
                      if (!Number.isNaN(event.target.valueAsNumber))
                        update(event.target.valueAsNumber);
                    }}
                  />
                  <span>{field.help}</span>
                </label>
              );
            return (
              <label key={field.key}>
                {field.label}
                <input
                  type="text"
                  maxLength={field.max_length}
                  value={String(value ?? "")}
                  placeholder={
                    field.format === "date-time"
                      ? "2026-08-01T00:00:00Z"
                      : undefined
                  }
                  onChange={(event) => update(event.target.value)}
                />
                <span>{field.help}</span>
              </label>
            );
          })}
        </fieldset>
      ))}
    </div>
  );
}

function PackConfigurationSummary({
  manifest,
  config,
}: {
  manifest: Manifest;
  config: ConfigDocument;
}) {
  if (manifest.id === "r-statistical-validation")
    return (
      <section className="notice" aria-label="R statistical comparison profile">
        <strong>Rendered statistical comparison profile</strong>
        <p>
          Golden{" "}
          <code>{String(readConfig(config, "golden.dataset_reference"))}</code>{" "}
          uses absolute tolerance{" "}
          <code>{String(readConfig(config, "tolerance.absolute"))}</code>,
          relative tolerance{" "}
          <code>{String(readConfig(config, "tolerance.relative"))}</code>, seed{" "}
          <code>{String(readConfig(config, "random.seed"))}</code>, locale{" "}
          <code>{String(readConfig(config, "environment.locale"))}</code>, and
          time zone{" "}
          <code>{String(readConfig(config, "environment.timezone"))}</code>.
          Baseline changes remain{" "}
          <strong>{String(readConfig(config, "golden.update_policy"))}</strong>.
        </p>
        <ul>
          {["tables", "models", "charts", "serialized"]
            .filter((kind) => Boolean(readConfig(config, `comparison.${kind}`)))
            .map((kind) => (
              <li key={kind}>{kind} comparison evidence enabled</li>
            ))}
        </ul>
      </section>
    );
  if (manifest.id === "sbom-fmea-security") {
    const scanners = ["syft", "grype", "trivy", "codeql"].filter((scanner) =>
      Boolean(readConfig(config, `scanner.${scanner}`)),
    );
    return (
      <section
        className="notice"
        aria-label="Security and SBOM release profile"
      >
        <strong>Security evidence and release profile</strong>
        <p>
          Selected scanners: {scanners.length ? scanners.join(", ") : "none"}.
          Database <code>{String(readConfig(config, "database.profile"))}</code>{" "}
          /{" "}
          <code>
            {String(readConfig(config, "database.snapshot_reference"))}
          </code>{" "}
          is operator-managed. Release policy is{" "}
          <strong>{String(readConfig(config, "release.gate"))}</strong> at{" "}
          {String(readConfig(config, "threshold.severity"))} severity and{" "}
          {String(readConfig(config, "threshold.confidence"))} confidence.
        </p>
        <p>
          SBOM baseline{" "}
          <code>{String(readConfig(config, "sbom.baseline_reference"))}</code>,
          FMEA set <code>{String(readConfig(config, "fmea.record_set"))}</code>,
          risk matrix{" "}
          <code>{String(readConfig(config, "risk.matrix_profile"))}</code>, and
          evidence links{" "}
          <code>{String(readConfig(config, "evidence.reference"))}</code> remain
          project-scoped references. CAPEC and ATT&amp;CK mappings are
          references, not exploitability proof.
        </p>
      </section>
    );
  }
  return null;
}
