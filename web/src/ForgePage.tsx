import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { Activity, Database, GitBranch, KeyRound, RefreshCw, ShieldCheck } from "lucide-react";
import { api, getCSRFToken } from "./api/client";
import type { components } from "./api/schema";

type Project = components["schemas"]["Project"];
type ForgeProfile = components["schemas"]["ForgeProfile"];
type ForgeProbe = components["schemas"]["ForgeProbe"];
type ForgeObject = components["schemas"]["ForgeObject"];
type ForgeSyncRun = components["schemas"]["ForgeSyncRun"];
type Provider = ForgeProfile["provider"];

function words(value: string): string {
  return value.replaceAll("_", " ").replace(/\b\w/g, (character) => character.toUpperCase());
}

function timestamp(value?: string): string {
  return value ? new Date(value).toLocaleString() : "—";
}

function statusClass(value: string): string {
  if (["ready", "configured", "complete", "supported"].includes(value)) return "good";
  if (["unsupported", "failed", "missing"].includes(value)) return "bad";
  if (["partial", "operator_only", "polling"].includes(value)) return "warn";
  return "neutral";
}

function Status({ value }: { value: string }) {
  return <span className={`status-pill ${statusClass(value)}`}><span aria-hidden="true">●</span>{words(value)}</span>;
}

function defaultEndpoint(provider: Provider): string {
  if (provider === "local") return "local://bare-git";
  if (provider === "github") return "https://api.github.com";
  return "https://gitlab.com";
}

export function ForgePage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [profiles, setProfiles] = useState<ForgeProfile[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [probe, setProbe] = useState<ForgeProbe | null>(null);
  const [runs, setRuns] = useState<ForgeSyncRun[]>([]);
  const [objects, setObjects] = useState<ForgeObject[]>([]);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  const selectedProject = projects.find((project) => project.id === selectedID);
  const saved = profiles.find((profile) => profile.project_id === selectedID);
  const provider = (selectedProject?.provider ?? "local") as Provider;

  const [endpoint, setEndpoint] = useState("local://bare-git");
  const [allowlist, setAllowlist] = useState("local://bare-git");
  const [credentialReference, setCredentialReference] = useState("");
  const [syncDirection, setSyncDirection] = useState<"pull" | "bidirectional_draft">("pull");
  const [pollingMinutes, setPollingMinutes] = useState(5);
  const [branchConvention, setBranchConvention] = useState("maintainer/{job_id}");
  const [changeRequestConvention, setChangeRequestConvention] = useState("draft");
  const [labelMapping, setLabelMapping] = useState("{}");
  const [artifactPolicy, setArtifactPolicy] = useState("metadata_only");
  const [releasePolicy, setReleasePolicy] = useState("observe");
  const [submodulesEnabled, setSubmodulesEnabled] = useState(true);
  const [enabled, setEnabled] = useState(true);
  const [reason, setReason] = useState("Configure provider-neutral forge synchronization");

  const load = useCallback(async () => {
    const [projectResponse, profileResponse] = await Promise.all([api.GET("/projects"), api.GET("/forges/profiles")]);
    const projectItems = projectResponse.data?.items ?? [];
    setProjects(projectItems);
    setProfiles(profileResponse.data?.items ?? []);
    setSelectedID((current) => current || projectItems[0]?.id || "");
  }, []);

  const loadEvidence = useCallback(async (projectID: string) => {
    if (!projectID) return;
    const [runResponse, objectResponse] = await Promise.all([
      api.GET("/projects/{projectID}/forge-sync-runs", { params: { path: { projectID } } }),
      api.GET("/projects/{projectID}/forge-objects", { params: { path: { projectID } } }),
    ]);
    setRuns(runResponse.data?.items ?? []);
    setObjects(objectResponse.data?.items ?? []);
  }, []);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => { void loadEvidence(selectedID); }, [loadEvidence, selectedID]);
  useEffect(() => {
    const profile = profiles.find((item) => item.project_id === selectedID);
    const project = projects.find((item) => item.id === selectedID);
    const selectedProvider = (project?.provider ?? "local") as Provider;
    setEndpoint(profile?.endpoint ?? defaultEndpoint(selectedProvider));
    setAllowlist((profile?.endpoint_allowlist ?? [defaultEndpoint(selectedProvider)]).join("\n"));
    setCredentialReference("");
    setSyncDirection(profile?.sync_direction ?? "pull");
    setPollingMinutes(profile?.polling_minutes ?? 5);
    setBranchConvention(profile?.branch_convention ?? "maintainer/{job_id}");
    setChangeRequestConvention(profile?.change_request_convention ?? "draft");
    setLabelMapping(JSON.stringify(profile?.label_mapping ?? {}, null, 2));
    setArtifactPolicy(profile?.ci_artifact_policy ?? "metadata_only");
    setReleasePolicy(profile?.release_policy ?? "observe");
    setSubmodulesEnabled(profile?.submodules_enabled ?? true);
    setEnabled(profile?.enabled ?? true);
  }, [profiles, projects, selectedID]);

  const counts = useMemo(() => objects.reduce<Record<string, number>>((result, item) => {
    result[item.kind] = (result[item.kind] ?? 0) + 1;
    return result;
  }, {}), [objects]);

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!selectedProject) return;
    let mapping: Record<string, string>;
    try {
      mapping = JSON.parse(labelMapping) as Record<string, string>;
    } catch {
      setMessage("Label mapping must be a JSON object of string pairs.");
      return;
    }
    setBusy(true);
    const response = await api.PUT("/projects/{projectID}/forge-profile", {
      params: { path: { projectID: selectedProject.id }, header: { "X-CSRF-Token": getCSRFToken() } },
      body: {
        provider, endpoint, endpoint_allowlist: allowlist.split("\n").map((item) => item.trim()).filter(Boolean),
        repository: selectedProject.repository,
        ...(credentialReference.trim() || saved?.credential_reference ? { credential_reference: credentialReference.trim() || saved?.credential_reference } : {}),
        credential_status: provider === "local" ? "not_required" : (saved?.credential_status ?? "configured"),
        webhook_status: provider === "local" ? "not_supported" : (saved?.webhook_status ?? "polling"),
        sync_direction: syncDirection, polling_minutes: pollingMinutes,
        branch_convention: branchConvention, change_request_convention: changeRequestConvention,
        label_mapping: mapping, ci_artifact_policy: artifactPolicy, release_policy: releasePolicy,
        submodules_enabled: submodulesEnabled, enabled,
        expected_revision: saved?.revision ?? 0, reason,
      },
    });
    if (response.data) {
      setProbe(null);
      await load();
    }
    setBusy(false);
    setMessage(response.data ? `Saved revision ${response.data.revision} for ${selectedProject.repository}.` : "Forge profile was not saved. Credential-reference changes require recent reauthentication.");
  };

  const runProbe = async () => {
    if (!selectedID) return;
    setBusy(true);
    const response = await api.POST("/projects/{projectID}/forge-profile/actions/probe", { params: { path: { projectID: selectedID }, header: { "X-CSRF-Token": getCSRFToken() } } });
    setBusy(false);
    setProbe(response.data ?? null);
    setMessage(response.data ? `Connection probe completed for ${selectedProject?.repository}.` : "Connection probe failed without exposing credential material.");
  };

  const sync = async () => {
    if (!selectedID) return;
    setBusy(true);
    const latest = runs[0]?.output_cursor || undefined;
    const response = await api.POST("/projects/{projectID}/forge-profile/actions/sync", {
      params: { path: { projectID: selectedID }, header: { "X-CSRF-Token": getCSRFToken() } },
      body: { ...(latest ? { cursor: latest } : {}), idempotency_key: `console-${selectedID}-${Date.now()}` },
    });
    setBusy(false);
    setMessage(response.data ? `Sync ${response.data.state}: ${response.data.objects} normalized object(s).` : "Forge synchronization failed.");
    if (response.data) await loadEvidence(selectedID);
  };

  return <>
    <p className="page-intro">Configure GitHub, GitLab, and local bare Git through one credential-isolated contract. Secret values stay in the Git bridge; this page only writes opaque references and reads redacted status.</p>
    <div className="metrics-grid compact">
      <article className="metric-card"><div className="card-label"><GitBranch aria-hidden="true" /><span>Providers</span></div><p className="metric-value">{profiles.length}</p><p className="metric-detail">Versioned profiles</p></article>
      <article className="metric-card"><div className="card-label"><Activity aria-hidden="true" /><span>Last sync</span></div><p className="metric-value">{runs[0]?.state ? words(runs[0].state) : "None"}</p><p className="metric-detail">{timestamp(runs[0]?.created_at)}</p></article>
      <article className="metric-card"><div className="card-label"><Database aria-hidden="true" /><span>Inventory</span></div><p className="metric-value">{objects.length}</p><p className="metric-detail">Normalized retained objects</p></article>
      <article className="metric-card"><div className="card-label"><ShieldCheck aria-hidden="true" /><span>Credentials</span></div><p className="metric-value">Write only</p><p className="metric-detail">Held by Git bridge</p></article>
    </div>

    <section className="content-section">
      <div className="section-heading"><div><p className="eyebrow">Versioned provider profile</p><h2>Connection and synchronization policy</h2></div></div>
      {projects.length === 0 ? <div className="empty-state"><GitBranch aria-hidden="true" /><div><strong>No registered projects</strong><p>Register a project before binding a forge profile.</p></div></div> : <form className="forge-form" onSubmit={(event) => void save(event)}>
        <div className="forge-form-grid">
          <label>Project<span>The registered provider fixes the credential boundary.</span><select value={selectedID} onChange={(event) => { setProbe(null); setSelectedID(event.target.value); }}>{projects.map((project) => <option key={project.id} value={project.id}>{project.repository} · {words(project.provider)}</option>)}</select></label>
          <label>Endpoint<span>Must exactly match one allow-list entry.</span><input required value={endpoint} onChange={(event) => setEndpoint(event.target.value)} /></label>
          <label className="forge-span-two">Endpoint allow-list<span>One exact HTTPS endpoint per line; loopback HTTP is test-only.</span><textarea required rows={3} value={allowlist} onChange={(event) => setAllowlist(event.target.value)} /></label>
          {provider !== "local" && <label className="forge-span-two">Credential reference (write only)<span>{saved?.credential_status ? `Current status: ${words(saved.credential_status)}. Leave blank to keep the current binding.` : "Enter an opaque Git bridge secret reference. The secret itself is never returned."}</span><div className="input-with-icon"><KeyRound aria-hidden="true" /><input type="password" autoComplete="new-password" value={credentialReference} onChange={(event) => setCredentialReference(event.target.value)} placeholder={saved ? "Current binding retained" : "gitbridge-secret:provider-main"} /></div></label>}
          <label>Sync direction<select value={syncDirection} onChange={(event) => setSyncDirection(event.target.value as typeof syncDirection)}><option value="pull">Pull only</option><option value="bidirectional_draft">Bidirectional draft</option></select></label>
          <label>Polling interval (minutes)<input required type="number" min={1} max={10080} value={pollingMinutes} onChange={(event) => setPollingMinutes(Number(event.target.value))} /></label>
          <label>Branch convention<input required maxLength={256} value={branchConvention} onChange={(event) => setBranchConvention(event.target.value)} /></label>
          <label>Change-request convention<input required maxLength={256} value={changeRequestConvention} onChange={(event) => setChangeRequestConvention(event.target.value)} /></label>
          <label>CI artifact policy<select value={artifactPolicy} onChange={(event) => setArtifactPolicy(event.target.value)}><option value="metadata_only">Metadata only</option><option value="retain_bounded">Retain bounded evidence</option><option value="disabled">Disabled</option></select></label>
          <label>Release policy<select value={releasePolicy} onChange={(event) => setReleasePolicy(event.target.value)}><option value="observe">Observe</option><option value="draft_only">Draft only</option><option value="disabled">Disabled</option></select></label>
          <label className="forge-span-two">Label mapping (JSON)<span>Internal labels to provider labels; malformed or oversized maps are rejected.</span><textarea rows={4} value={labelMapping} onChange={(event) => setLabelMapping(event.target.value)} /></label>
          <label className="forge-span-two">Audit reason<span>Recorded with the versioned profile change.</span><input required maxLength={1000} value={reason} onChange={(event) => setReason(event.target.value)} /></label>
        </div>
        <div className="forge-checks"><label><input type="checkbox" checked={submodulesEnabled} onChange={(event) => setSubmodulesEnabled(event.target.checked)} />Collect submodule metadata</label><label><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />Profile enabled</label></div>
        <div className="card-actions"><button type="submit" disabled={busy}>Save profile</button><button className="secondary-button" type="button" disabled={busy || !saved?.enabled} onClick={() => void runProbe()}><Activity aria-hidden="true" />Test connection</button><button className="secondary-button" type="button" disabled={busy || !saved?.enabled} onClick={() => void sync()}><RefreshCw aria-hidden="true" />Sync inventory</button></div>
      </form>}
      {message && <p className="inline-message" role="status">{message}</p>}
    </section>

    {probe && <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Credential-safe diagnostics</p><h2>Connection probe</h2></div><Status value={probe.ready ? "ready" : "failed"} /></div><div className="capability-grid">{probe.capabilities.map((capability) => <article key={capability.feature}><div><strong>{words(capability.feature)}</strong><Status value={capability.status} /></div>{capability.reason && <p>{capability.reason}</p>}</article>)}</div>{probe.problems.length > 0 && <div className="notice" role="status">{probe.problems.join(" · ")}</div>}</section>}

    <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Provider-neutral evidence</p><h2>Normalized inventory</h2></div><button className="secondary-button" type="button" disabled={!selectedID} onClick={() => void loadEvidence(selectedID)}><RefreshCw aria-hidden="true" />Refresh</button></div>{objects.length === 0 ? <div className="empty-state"><Database aria-hidden="true" /><div><strong>No synchronized objects</strong><p>Run a sync to retain repositories, issues, change requests, CI evidence, refs, releases, and submodules supported by the selected provider.</p></div></div> : <><div className="inventory-summary">{Object.entries(counts).sort().map(([kind, count]) => <div key={kind}><span>{words(kind)}</span><strong>{count}</strong></div>)}</div><div className="table-scroll" tabIndex={0} aria-label="Scrollable normalized forge inventory"><table><thead><tr><th>Kind</th><th>Title or ref</th><th>State</th><th>External ID</th><th>Updated</th></tr></thead><tbody>{objects.map((object) => <tr key={`${object.provider}:${object.kind}:${object.external_id}`}><td><Status value={object.kind} /></td><td>{object.title || object.ref || "—"}</td><td>{object.state ? <Status value={object.state} /> : "—"}</td><td><code>{object.external_id}</code></td><td>{timestamp(object.updated_at)}</td></tr>)}</tbody></table></div></>}</section>

    <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Transactional and replay-safe</p><h2>Sync history</h2></div></div>{runs.length === 0 ? <div className="empty-state"><Activity aria-hidden="true" /><div><strong>No sync runs</strong><p>Each attempt records its input cursor, output cursor, idempotency key, partial state, and unsupported capabilities.</p></div></div> : <div className="audit-list">{runs.map((run) => <div key={run.id}><time>{timestamp(run.created_at)}</time><strong><Status value={run.state} /></strong><span>{run.objects} object(s){run.replay ? " · replay" : ""}{run.unsupported.length ? ` · ${run.unsupported.length} unsupported` : ""}</span></div>)}</div>}</section>
  </>;
}
