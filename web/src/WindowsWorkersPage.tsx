import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { Activity, Boxes, Cpu, KeyRound, MonitorCog, Play, RefreshCw, ShieldCheck } from "lucide-react";
import { api, getCSRFToken } from "./api/client";
import type { components } from "./api/schema";

type Profile = components["schemas"]["WindowsWorkerProfile"];
type Probe = components["schemas"]["WindowsWorkerProbe"];
type Result = components["schemas"]["WindowsWorkerResult"];
type JobType = components["schemas"]["WindowsJobType"];

const jobs: JobType[] = ["dotnet_restore_build_test", "powershell_pester", "windows_service_lifecycle", "inno_installer_lifecycle", "hamilton_discovery", "vpn_workflow", "release_consistency", "installer_iq_evidence", "equipment_simulation", "signing_request"];
const zeroSHA = "0123456789abcdef0123456789abcdef01234567";
const packChecksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const inventoryChecksum = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

function words(value: string) { return value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase()); }
function stamp(value?: string) { return value ? new Date(value).toLocaleString() : "—"; }
function Status({ value }: { value: string }) { const tone = ["healthy", "ready", "completed", "passed"].includes(value) ? "good" : ["failed", "unhealthy"].includes(value) ? "bad" : "neutral"; return <span className={`status-pill ${tone}`}><span aria-hidden="true">●</span>{words(value)}</span>; }

export function WindowsWorkersPage() {
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [profileID, setProfileID] = useState("windows-simulator");
  const [probe, setProbe] = useState<Probe | null>(null);
  const [runs, setRuns] = useState<Result[]>([]);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const selected = profiles.find((profile) => profile.id === profileID);

  const [name, setName] = useState("Windows worker simulator");
  const [mode, setMode] = useState<"simulator" | "remote">("simulator");
  const [endpoint, setEndpoint] = useState("simulator://windows-worker");
  const [allowlist, setAllowlist] = useState("simulator://windows-worker");
  const [credentialReference, setCredentialReference] = useState("");
  const [capacity, setCapacity] = useState(2);
  const [template, setTemplate] = useState("windows-2022-sim-v1");
  const [toolchains, setToolchains] = useState('{"dotnet":"8.0.302","powershell":"7.4.4","pester":"5.6.1","inno":"6.3.3"}');
  const [allowedJobs, setAllowedJobs] = useState<JobType[]>(jobs);
  const [timeout, setTimeoutValue] = useState(1800);
  const [simulators, setSimulators] = useState("hamilton-sim-v1\ninstrument-sim-v1");
  const [retention, setRetention] = useState(30);
  const [signingPolicy, setSigningPolicy] = useState("");
  const [hardwareGate, setHardwareGate] = useState(false);
  const [signingGate, setSigningGate] = useState(false);
  const [vpnGate, setVPNGate] = useState(false);
  const [enabled, setEnabled] = useState(true);
  const [reason, setReason] = useState("Update bounded Windows worker inventory");
  const [jobType, setJobType] = useState<JobType>("dotnet_restore_build_test");
  const [projectID, setProjectID] = useState("owner-repo");

  const load = useCallback(async () => {
    const response = await api.GET("/windows-workers/profiles");
    const items = response.data?.items ?? [];
    setProfiles(items);
    setProfileID((current) => current || items[0]?.id || "windows-simulator");
  }, []);
  const loadRuns = useCallback(async (id: string) => {
    if (!id) return;
    const response = await api.GET("/windows-workers/profiles/{profileID}/runs", { params: { path: { profileID: id } } });
    setRuns(response.data?.items ?? []);
  }, []);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => { void loadRuns(profileID); }, [loadRuns, profileID]);
  useEffect(() => {
    if (!selected) return;
    setName(selected.name); setMode(selected.mode); setEndpoint(selected.endpoint); setAllowlist(selected.endpoint_allowlist.join("\n"));
    setCredentialReference(""); setCapacity(selected.capacity); setTemplate(selected.vm_template_id); setToolchains(JSON.stringify(selected.toolchains, null, 2));
    setAllowedJobs(selected.allowed_job_types); setTimeoutValue(selected.timeout_seconds); setSimulators(selected.simulator_profile_ids.join("\n"));
    setRetention(selected.artifact_retention_days); setSigningPolicy(selected.signing_policy_reference ?? "");
    setHardwareGate(selected.manual_gates.physical_hardware); setSigningGate(selected.manual_gates.code_signing); setVPNGate(selected.manual_gates.production_vpn); setEnabled(selected.enabled);
  }, [selected]);

  const health = useMemo(() => probe?.health ?? selected?.health ?? "unknown", [probe, selected]);

  const save = async (event: FormEvent) => {
    event.preventDefault();
    let inventory: Record<string, string>;
    try { inventory = JSON.parse(toolchains) as Record<string, string>; } catch { setMessage("Toolchains must be a JSON object of pinned name/version pairs."); return; }
    setBusy(true);
    const response = await api.PUT("/windows-workers/profiles/{profileID}", {
      params: { path: { profileID }, header: { "X-CSRF-Token": getCSRFToken() } },
      body: {
        name, mode, endpoint, endpoint_allowlist: allowlist.split("\n").map((item) => item.trim()).filter(Boolean),
        ...(credentialReference.trim() ? { credential_reference: credentialReference.trim() } : {}),
        credential_status: mode === "simulator" ? "not_required" : (selected?.credential_status ?? "configured"),
        health: mode === "simulator" ? "simulated" : (selected?.health ?? "unknown"), capacity, vm_template_id: template,
        toolchains: inventory, allowed_job_types: allowedJobs, timeout_seconds: timeout,
        simulator_profile_ids: simulators.split("\n").map((item) => item.trim()).filter(Boolean), artifact_retention_days: retention,
        ...(signingPolicy.trim() ? { signing_policy_reference: signingPolicy.trim() } : {}),
        manual_gates: { physical_hardware: hardwareGate, code_signing: signingGate, production_vpn: vpnGate }, enabled,
        expected_revision: selected?.revision ?? 0, reason,
      },
    });
    setBusy(false);
    setMessage(response.data ? `Saved worker profile revision ${response.data.revision}.` : "Profile rejected. Sensitive credential, signing, and gate changes require recent reauthentication.");
    if (response.data) await load();
  };

  const testConnection = async () => {
    setBusy(true); const response = await api.POST("/windows-workers/profiles/{profileID}/actions/probe", { params: { path: { profileID }, header: { "X-CSRF-Token": getCSRFToken() } } }); setBusy(false);
    setProbe(response.data ?? null); setMessage(response.data ? "Structured worker probe completed." : "Worker probe failed without exposing credential material.");
  };

  const runFixture = async () => {
    const now = Date.now();
    const input: components["schemas"]["WindowsWorkerImmutableInput"] = { repository_sha: zeroSHA, capability_pack_checksum: packChecksum, toolchain_inventory_checksum: inventoryChecksum, source_artifact_id: "windows-fixture-source" };
    if (jobType === "windows_service_lifecycle") input.expected_service_name = "CodeMaintainerFixture";
    if (jobType === "hamilton_discovery") input.hamilton_profile_id = "hamilton-sim-v1";
    if (jobType === "equipment_simulation") input.simulator_profile_id = "instrument-sim-v1";
    if (["release_consistency", "signing_request"].includes(jobType)) input.release_version = "1.0.0-fixture";
    setBusy(true);
    const response = await api.POST("/windows-workers/profiles/{profileID}/runs", { params: { path: { profileID }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { project_id: projectID, job_id: `console-${now}`, job_type: jobType, input, idempotency_key: `console-${now}`, operator_gated: ["vpn_workflow", "signing_request"].includes(jobType) } });
    setBusy(false); setMessage(response.data ? `${words(jobType)} completed with ${response.data.checks.length} structured checks.` : "The worker operation was rejected by policy.");
    if (response.data) await loadRuns(profileID);
  };

  return <>
    <p className="page-intro">Operate Windows verification through fixed, non-executable job types. The controller sends exact source, capability-pack, and toolchain identities; remote workers never receive arbitrary commands or repository credentials.</p>
    <div className="metrics-grid compact">
      <article className="metric-card"><div className="card-label"><MonitorCog aria-hidden="true"/><span>Profiles</span></div><p className="metric-value">{profiles.length}</p><p className="metric-detail">Simulator and remote boundaries</p></article>
      <article className="metric-card"><div className="card-label"><Activity aria-hidden="true"/><span>Health</span></div><p className="metric-value">{words(health)}</p><p className="metric-detail">{selected?.mode ?? "No profile selected"}</p></article>
      <article className="metric-card"><div className="card-label"><Cpu aria-hidden="true"/><span>Capacity</span></div><p className="metric-value">{probe?.capacity ?? selected?.capacity ?? 0}</p><p className="metric-detail">Bounded concurrent workers</p></article>
      <article className="metric-card"><div className="card-label"><ShieldCheck aria-hidden="true"/><span>Operations</span></div><p className="metric-value">{selected?.allowed_job_types.length ?? 0}</p><p className="metric-detail">Controller-approved job types</p></article>
    </div>

    <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Versioned worker boundary</p><h2>Connection, inventory, and operator gates</h2></div></div>
      <form className="forge-form" onSubmit={(event) => void save(event)}><div className="forge-form-grid">
        <label>Registered profile<span>Select a durable profile or type a new safe ID.</span><input list="worker-profile-list" required pattern="[A-Za-z0-9][A-Za-z0-9._-]{0,127}" value={profileID} onChange={(event) => { setProbe(null); setProfileID(event.target.value); }}/><datalist id="worker-profile-list">{profiles.map((profile) => <option key={profile.id} value={profile.id}>{profile.name}</option>)}</datalist></label>
        <label>Display name<input required maxLength={128} value={name} onChange={(event) => setName(event.target.value)}/></label>
        <label>Mode<select value={mode} onChange={(event) => setMode(event.target.value as typeof mode)}><option value="simulator">Deterministic simulator</option><option value="remote">Remote Windows adapter</option></select></label>
        <label>Capacity<input type="number" min={1} max={64} value={capacity} onChange={(event) => setCapacity(Number(event.target.value))}/></label>
        <label>Endpoint<span>Remote endpoints require HTTPS; loopback HTTP is test-only.</span><input required value={endpoint} onChange={(event) => setEndpoint(event.target.value)}/></label>
        <label>VM template ID<input required value={template} onChange={(event) => setTemplate(event.target.value)}/></label>
        <label className="forge-span-two">Exact endpoint allow-list<textarea required rows={2} value={allowlist} onChange={(event) => setAllowlist(event.target.value)}/></label>
        {mode === "remote" && <label className="forge-span-two">Credential reference (write only)<span>Opaque adapter secret reference; never paste secret material.</span><div className="input-with-icon"><KeyRound aria-hidden="true"/><input type="password" autoComplete="new-password" value={credentialReference} onChange={(event) => setCredentialReference(event.target.value)}/></div></label>}
        <label className="forge-span-two">Pinned toolchains (JSON)<textarea required rows={4} value={toolchains} onChange={(event) => setToolchains(event.target.value)}/></label>
        <label>Timeout seconds<input type="number" min={30} max={86400} value={timeout} onChange={(event) => setTimeoutValue(Number(event.target.value))}/></label>
        <label>Artifact retention days<input type="number" min={1} max={3650} value={retention} onChange={(event) => setRetention(Number(event.target.value))}/></label>
        <label>Simulator / HAMILTON profiles<textarea rows={3} value={simulators} onChange={(event) => setSimulators(event.target.value)}/></label>
        <label>Signing policy reference<input value={signingPolicy} onChange={(event) => setSigningPolicy(event.target.value)}/></label>
        <fieldset className="forge-span-two"><legend>Approved job types</legend><div className="job-type-grid">{jobs.map((job) => <label key={job}><input type="checkbox" checked={allowedJobs.includes(job)} onChange={(event) => setAllowedJobs((current) => event.target.checked ? [...current, job] : current.filter((item) => item !== job))}/>{words(job)}</label>)}</div></fieldset>
        <label className="forge-span-two">Audit reason<input required maxLength={1000} value={reason} onChange={(event) => setReason(event.target.value)}/></label>
      </div><div className="forge-checks"><label><input type="checkbox" checked={hardwareGate} disabled={mode === "simulator"} onChange={(event) => setHardwareGate(event.target.checked)}/>Physical hardware gate</label><label><input type="checkbox" checked={signingGate} disabled={mode === "simulator"} onChange={(event) => setSigningGate(event.target.checked)}/>Code-signing gate</label><label><input type="checkbox" checked={vpnGate} disabled={mode === "simulator"} onChange={(event) => setVPNGate(event.target.checked)}/>Production VPN gate</label><label><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)}/>Profile enabled</label></div>
      <div className="card-actions"><button type="submit" disabled={busy}>Save profile</button><button type="button" className="secondary-button" disabled={busy || !selected?.enabled} onClick={() => void testConnection()}><Activity aria-hidden="true"/>Test connection</button></div></form>{message && <p className="inline-message" role="status">{message}</p>}
    </section>

    {probe && <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Credential-safe diagnostics</p><h2>Worker probe</h2></div><Status value={probe.ready ? "ready" : "failed"}/></div><div className="detail-grid"><div><span>VM template</span><strong>{probe.vm_template_id}</strong></div><div><span>Capacity</span><strong>{probe.capacity}</strong></div><div><span>Checked</span><strong>{stamp(probe.checked_at)}</strong></div></div></section>}

    <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Simulator-first verification</p><h2>Run a closed fixture operation</h2></div></div><div className="configuration-toolbar"><label>Project ID<input value={projectID} onChange={(event) => setProjectID(event.target.value)}/></label><label>Approved operation<select value={jobType} onChange={(event) => setJobType(event.target.value as JobType)}>{allowedJobs.map((job) => <option key={job} value={job}>{words(job)}</option>)}</select></label><div><span className="field-help">Uses immutable fixture hashes and structured results only.</span></div><button type="button" disabled={busy || !selected?.enabled} onClick={() => void runFixture()}><Play aria-hidden="true"/>Run fixture</button></div></section>

    <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Durable evidence</p><h2>Windows run history</h2></div><button type="button" className="secondary-button" disabled={!profileID} onClick={() => void loadRuns(profileID)}><RefreshCw aria-hidden="true"/>Refresh</button></div>{runs.length === 0 ? <div className="empty-state"><Boxes aria-hidden="true"/><div><strong>No Windows evidence</strong><p>Completed runs retain input checksums, checks, artifact metadata, and idempotent replay state.</p></div></div> : <div className="table-scroll" tabIndex={0} aria-label="Scrollable Windows worker run history"><table><thead><tr><th>Operation</th><th>Result</th><th>Checks</th><th>Input identity</th><th>Completed</th></tr></thead><tbody>{runs.map((run) => <tr key={run.run_id}><td>{words(run.job_type)}</td><td><Status value={run.state}/>{run.replay && <small>Replay</small>}</td><td>{run.checks.length} structured</td><td><code>{run.input_sha256.slice(0, 12)}</code></td><td>{stamp(run.completed_at)}</td></tr>)}</tbody></table></div>}</section>
  </>;
}
