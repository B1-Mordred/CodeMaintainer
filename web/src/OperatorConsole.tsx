import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Activity, Bell, BookOpen, Boxes, BrainCircuit, CheckCircle2, ChevronRight,
  CircleDot, ClipboardCheck, Cpu, Database, FileClock, FolderGit2,
  Gauge, GitBranch, HardDrive, ListChecks, MemoryStick, MonitorCog, RefreshCw, Settings,
  ShieldCheck, SlidersHorizontal, TerminalSquare, TimerReset, Users,
} from "lucide-react";
import { api, getCSRFToken } from "./api/client";
import type { components } from "./api/schema";
import { ConfigurationPage } from "./ConfigurationPage";
import { IntelligencePage } from "./IntelligencePage";
import { CapabilityPage } from "./CapabilityPage";
import { ForgePage } from "./ForgePage";
import { WindowsWorkersPage } from "./WindowsWorkersPage";

type SystemStatus = components["schemas"]["SystemStatus"];
type Job = components["schemas"]["Job"];
type Project = components["schemas"]["Project"];
type MemoryRecord = components["schemas"]["MemoryRecord"];
type Schedule = components["schemas"]["Schedule"];
type ScheduleRun = components["schemas"]["ScheduleRun"];
type SchedulerDecision = components["schemas"]["SchedulerDecision"];
type SchedulerResourceProfile = components["schemas"]["SchedulerResourceProfile"];
type SchedulerTopology = components["schemas"]["SchedulerTopology"];
type SchedulerSimulationRequest = components["schemas"]["SchedulerSimulationRequest"];
type SkillProposal = components["schemas"]["SkillProposal"];
type Notification = components["schemas"]["Notification"];
type ConfigRevision = components["schemas"]["ConfigRevision"];
type Finding = components["schemas"]["Finding"];
type User = components["schemas"]["User"];
type BackupRecord = components["schemas"]["BackupRecord"];
type Artifact = components["schemas"]["Artifact"];
type EvidenceGraph = components["schemas"]["EvidenceGraph"];
type ModelProfile = components["schemas"]["ModelProfile"];
type ModelStatus = components["schemas"]["ModelStatus"];
type RuntimeBenchmark = components["schemas"]["RuntimeBenchmark"];
type ProviderGatewayStatus = components["schemas"]["ProviderGatewayStatus"];
type ProviderRouteDecision = components["schemas"]["ProviderRouteDecision"];
type ProviderRouteRequest = components["schemas"]["ProviderRouteRequest"];
type ProviderCapabilityProbe = components["schemas"]["ProviderCapabilityProbe"];
type PolicyBundle = components["schemas"]["PolicyBundle"];
type PolicyActivation = components["schemas"]["PolicyActivation"];
type PolicySimulation = components["schemas"]["PolicySimulation"];
type PolicyTestCase = components["schemas"]["PolicyTestCase"];
type PolicyTestRun = components["schemas"]["PolicyTestRun"];
type TestDesignerReport = components["schemas"]["TestDesignerReport"];
type DocumentationPolicyProfile = components["schemas"]["DocumentationPolicyProfile"];
type DocumentationPolicySimulationResult = components["schemas"]["DocumentationPolicySimulationResult"];

type PageID = "first-run" | "overview" | "projects" | "onboarding" | "jobs" | "quality" | "policy" | "intelligence" | "models" | "memory" | "forges" | "windows-workers" | "automation" | "configuration" | "administration";

const navigation: Array<{ id: PageID; label: string; icon: ReactNode; group: "operate" | "integrate" | "manage" }> = [
  { id: "first-run", label: "First run", icon: <ListChecks aria-hidden="true" />, group: "operate" },
  { id: "overview", label: "Overview", icon: <Gauge aria-hidden="true" />, group: "operate" },
  { id: "projects", label: "Projects", icon: <FolderGit2 aria-hidden="true" />, group: "operate" },
  { id: "onboarding", label: "Onboarding & packs", icon: <Boxes aria-hidden="true" />, group: "operate" },
  { id: "jobs", label: "Jobs", icon: <TerminalSquare aria-hidden="true" />, group: "operate" },
  { id: "quality", label: "QC / QA", icon: <ClipboardCheck aria-hidden="true" />, group: "operate" },
  { id: "policy", label: "Policy", icon: <ShieldCheck aria-hidden="true" />, group: "operate" },
  { id: "intelligence", label: "Code intelligence", icon: <BrainCircuit aria-hidden="true" />, group: "operate" },
  { id: "models", label: "Models", icon: <BrainCircuit aria-hidden="true" />, group: "integrate" },
  { id: "memory", label: "Memory", icon: <Database aria-hidden="true" />, group: "integrate" },
  { id: "forges", label: "Git forges", icon: <GitBranch aria-hidden="true" />, group: "integrate" },
  { id: "windows-workers", label: "Windows workers", icon: <MonitorCog aria-hidden="true" />, group: "integrate" },
  { id: "automation", label: "Scheduling / Hermes", icon: <FileClock aria-hidden="true" />, group: "integrate" },
  { id: "configuration", label: "Configuration", icon: <SlidersHorizontal aria-hidden="true" />, group: "manage" },
  { id: "administration", label: "Administration", icon: <Settings aria-hidden="true" />, group: "manage" },
];

function label(value: string): string {
  return value.replaceAll("_", " ").replace(/\b\w/g, (character) => character.toUpperCase());
}

function date(value?: string): string {
  return value ? new Date(value).toLocaleString() : "—";
}

function shortSHA(value?: string): string {
  return value ? value.slice(0, 10) : "—";
}

function gibibytes(value?: number): string {
  return `${((value ?? 0) / 1073741824).toFixed(1)} GiB`;
}

function statusTone(value: string): "good" | "warn" | "bad" | "neutral" {
  if (["healthy", "completed", "canonical", "verified", "closed", "read", "draft_pr_created"].includes(value)) return "good";
  if (["failed", "cancelled", "open", "blocker", "must_fix", "stale", "rejected"].includes(value)) return "bad";
  if (["awaiting_operator", "awaiting_task_approval", "awaiting_test_design_disposition", "awaiting_golden_approval", "awaiting_repair", "quarantine", "disputed", "delivered"].includes(value)) return "warn";
  return "neutral";
}

export function OperatorConsole({
  initialStatus, initialJobs, loading: initialLoading, error: initialError, reloadOverview,
}: {
  initialStatus: SystemStatus | null;
  initialJobs: Job[];
  loading: boolean;
  error: string | null;
  reloadOverview: () => Promise<void>;
}) {
  const initialPage = window.location.hash.slice(1) as PageID;
  const [page, setPage] = useState<PageID>(navigation.some((item) => item.id === initialPage) ? initialPage : "overview");
  const [expert, setExpert] = useState(false);
  const initialHeadingFocus = useRef(true);
  const current = navigation.find((item) => item.id === page)!;

  useEffect(() => {
    document.title = `${current.label} | CodeMaintainer`;
    if (initialHeadingFocus.current) {
      initialHeadingFocus.current = false;
      return;
    }
    document.querySelector<HTMLElement>("#page-heading")?.focus();
  }, [current.label]);

  useEffect(() => {
    const navigateHistory = () => {
      const candidate = window.location.hash.slice(1) as PageID;
      if (navigation.some((item) => item.id === candidate)) setPage(candidate);
    };
    window.addEventListener("popstate", navigateHistory);
    window.addEventListener("hashchange", navigateHistory);
    return () => { window.removeEventListener("popstate", navigateHistory); window.removeEventListener("hashchange", navigateHistory); };
  }, []);

  const navigate = (target: PageID) => {
    if (target !== page) window.history.pushState(null, "", `#${target}`);
    setPage(target);
  };

  return <div className="console-shell">
    <aside className="side-nav">
      <nav aria-label="Primary navigation">
        {(["operate", "integrate", "manage"] as const).map((group) => <div className="nav-group" key={group}>
          <p className="nav-label">{group}</p>
          {navigation.filter((item) => item.group === group).map((item) => <button
            key={item.id}
            className={page === item.id ? "nav-item active" : "nav-item"}
            type="button"
            aria-current={page === item.id ? "page" : undefined}
            onClick={() => navigate(item.id)}
          >{item.icon}<span>{item.label}</span></button>)}
        </div>)}
      </nav>
      <div className="mode-switch">
        <div><span>Expert mode</span><small>Schema-bound fields</small></div>
        <button type="button" role="switch" aria-label="Expert mode" aria-checked={expert} onClick={() => setExpert((value) => !value)} className={expert ? "switch on" : "switch"}><span /></button>
      </div>
    </aside>
    <div className="console-content">
      <header className="page-heading">
        <div><p className="eyebrow">Control plane / {current.group}</p><h1 id="page-heading" tabIndex={-1}>{current.label}</h1></div>
        <span className="mode-badge"><SlidersHorizontal aria-hidden="true" /> {expert ? "Expert" : "Safe"} mode</span>
      </header>
      {initialError && <p className="error" role="alert">{initialError}</p>}
      {page === "first-run" && <FirstRunPage status={initialStatus} jobs={initialJobs} navigate={navigate} />}
      {page === "overview" && <OverviewPage status={initialStatus} jobs={initialJobs} loading={initialLoading} reload={reloadOverview} />}
      {page === "projects" && <ProjectsPage expert={expert} />}
      {page === "onboarding" && <CapabilityPage />}
      {page === "jobs" && <JobsPage initialJobs={initialJobs} expert={expert} />}
      {page === "quality" && <QualityPage jobs={initialJobs} />}
      {page === "policy" && <PolicyPage expert={expert} />}
      {page === "intelligence" && <IntelligencePage />}
      {page === "models" && <ModelsPage status={initialStatus} expert={expert} />}
      {page === "memory" && <MemoryPage />}
      {page === "forges" && <ForgePage />}
      {page === "windows-workers" && <WindowsWorkersPage />}
      {page === "automation" && <AutomationPage expert={expert} />}
      {page === "configuration" && <ConfigurationPage expert={expert} />}
      {page === "administration" && <AdministrationPage status={initialStatus} />}
    </div>
  </div>;
}

function PageIntro({ children }: { children: ReactNode }) {
  return <p className="page-intro">{children}</p>;
}

function Section({ title, eyebrow, action, children }: { title: string; eyebrow?: string; action?: ReactNode; children: ReactNode }) {
  return <section className="content-section">
    <div className="section-heading"><div>{eyebrow && <p className="eyebrow">{eyebrow}</p>}<h2>{title}</h2></div>{action}</div>
    {children}
  </section>;
}

function Empty({ title, detail }: { title: string; detail: string }) {
  return <div className="empty-state"><CircleDot aria-hidden="true" /><div><strong>{title}</strong><p>{detail}</p></div></div>;
}

function Badge({ value }: { value: string }) {
  return <span className={`status-pill ${statusTone(value)}`}><span aria-hidden="true">●</span>{label(value)}</span>;
}

function Metric({ icon, name, value, detail }: { icon: ReactNode; name: string; value: string; detail?: string }) {
  return <article className="metric-card"><div className="card-label">{icon}<span>{name}</span></div><p className="metric-value">{value}</p>{detail && <p className="metric-detail">{detail}</p>}</article>;
}

function FirstRunPage({ status, jobs, navigate }: { status: SystemStatus | null; jobs: Job[]; navigate: (page: PageID) => void }) {
  const steps = [
    ["Administrator account", "Complete", "The authenticated local administrator is active.", "administration"],
    ["Host capacity", status ? "Detected" : "Check", "Review CPU, memory, storage, SMT and NUMA guidance.", "overview"],
    ["Model manifests", "Review", "Import a checksum-bound manifest or retain the CI fake model.", "models"],
    ["Git provider", "Review", "Use local bare Git or configure the narrow GitHub or GitLab bridge boundary.", "forges"],
    ["Repository", "Required", "Register a repository and its exact default branch.", "projects"],
    ["Toolchains", "Per project", "Detection occurs during the isolated dependency and verification phases.", "projects"],
    ["OpenViking", "Optional", "Validate the scoped index before enabling semantic memory.", "memory"],
    ["Hermes", "Optional", "Review the ten-tool, non-authoritative automation boundary.", "automation"],
    ["Smoke-test job", jobs.length ? "Complete" : "Required", "Submit one deterministic maintenance task and inspect its final report.", "jobs"],
  ] as const;
  return <>
    <PageIntro>Finish the local appliance in a deliberate order. Optional integrations never block the deterministic mock workflow.</PageIntro>
    <div className="setup-progress"><div><strong>{steps.filter((step) => ["Complete", "Detected"].includes(step[1])).length} of {steps.length}</strong><span>setup checks confirmed</span></div><div className="progress-track" aria-hidden="true"><span style={{ width: `${Math.round(100 * steps.filter((step) => ["Complete", "Detected"].includes(step[1])).length / steps.length)}%` }} /></div></div>
    <ol className="setup-list">
      {steps.map(([name, state, detail, target], index) => <li key={name}>
        <span className="step-number">{index + 1}</span><div><div className="step-title"><h2>{name}</h2><Badge value={state.toLowerCase().replaceAll(" ", "_")} /></div><p>{detail}</p></div>
        <button className="tertiary-button" type="button" onClick={() => navigate(target)} aria-label={`Open ${name}`}><ChevronRight aria-hidden="true" /></button>
      </li>)}
    </ol>
  </>;
}

function OverviewPage({ status, jobs, loading, reload }: { status: SystemStatus | null; jobs: Job[]; loading: boolean; reload: () => Promise<void> }) {
  const active = jobs.filter((job) => !["completed", "failed", "cancelled"].includes(job.state));
  const failed = jobs.filter((job) => job.state === "failed");
  const drafts = jobs.filter((job) => job.state === "draft_pr_created");
  const [openFindings, setOpenFindings] = useState(0);
  const [syncWarnings, setSyncWarnings] = useState(0);
  useEffect(() => { void (async () => { const projects = await api.GET("/projects"); const details = await Promise.all(jobs.slice(0, 50).map((job) => api.GET("/jobs/{jobID}", { params: { path: { jobID: job.id } } }))); setSyncWarnings((projects.data?.items ?? []).filter((project) => !project.enabled).length); setOpenFindings(details.flatMap((detail) => detail.data?.findings ?? []).filter((finding) => ["open", "fixed", "verified", "disputed", "rejected"].includes(finding.status)).length); })(); }, [jobs]);
  return <>
    <PageIntro>Live operating state across the controller, queue, inference boundary, memory index, and publication workflow.</PageIntro>
    <div className="metrics-grid">
      <Metric icon={<Activity aria-hidden="true" />} name="Controller" value={status?.status ?? "Checking"} detail={`Uptime ${status ? Math.floor(status.uptime_seconds / 60) : 0} min`} />
      <Metric icon={<Cpu aria-hidden="true" />} name="CPU" value={`${status?.host?.physical_cores ?? 0} cores / ${status?.host?.logical_cpus ?? 0} threads`} detail={`${status?.host?.architecture ?? "unknown"} · SMT ${status?.host?.smt_enabled ? "on" : "off"} · NUMA ${status?.host?.numa_nodes ?? "unknown"}`} />
      <Metric icon={<MemoryStick aria-hidden="true" />} name="Available RAM" value={`${((status?.host?.memory_available_bytes ?? 0) / 1073741824).toFixed(1)} GiB`} detail={`${((status?.host?.memory_total_bytes ?? 0) / 1073741824).toFixed(1)} GiB total`} />
      <Metric icon={<HardDrive aria-hidden="true" />} name="Available disk" value={`${((status?.host?.disk_available_bytes ?? 0) / 1073741824).toFixed(1)} GiB`} detail={`${((status?.host?.disk_total_bytes ?? 0) / 1073741824).toFixed(1)} GiB total`} />
      <Metric icon={<TimerReset aria-hidden="true" />} name="Active jobs" value={String(active.length)} detail={`${jobs.length} total retained`} />
      <Metric icon={<Cpu aria-hidden="true" />} name="Model" value={status?.components.model ?? "Unknown"} detail="Sequential supervisor" />
      <Metric icon={<Database aria-hidden="true" />} name="Memory" value={status?.components.memory ?? "Unknown"} detail="Project-scoped retrieval" />
      <Metric icon={<HardDrive aria-hidden="true" />} name="Runner" value={status?.components.runner ?? "Unknown"} detail="Policy-bound execution" />
      <Metric icon={<ShieldCheck aria-hidden="true" />} name="Recent failures" value={String(failed.length)} detail="Within retained jobs" />
      <Metric icon={<ClipboardCheck aria-hidden="true" />} name="Open QC findings" value={String(openFindings)} detail="Resolution or waiver required" />
      <Metric icon={<GitBranch aria-hidden="true" />} name="Draft PRs" value={String(drafts.length)} detail="Publication remains explicit" />
      <Metric icon={<RefreshCw aria-hidden="true" />} name="Sync warnings" value={String(syncWarnings)} detail="Disabled or attention-needed projects" />
    </div>
    <Section title="Service health" eyebrow="Boundaries" action={<button className="secondary-button" type="button" onClick={() => void reload()} disabled={loading}><RefreshCw aria-hidden="true" />Refresh</button>}>
      <div className="health-grid">{Object.entries(status?.components ?? {}).map(([name, value]) => <div key={name}><span>{label(name)}</span><Badge value={value} /></div>)}</div>
    </Section>
    <Section title="Queue and recent jobs" eyebrow="Workflow">
      <JobTable jobs={jobs} loading={loading} />
    </Section>
  </>;
}

function JobTable({ jobs, loading = false, onSelect }: { jobs: Job[]; loading?: boolean; onSelect?: (job: Job) => void }) {
  return <div className="table-scroll" tabIndex={0} aria-label="Scrollable maintenance jobs table"><table>
    <thead><tr><th scope="col">Job</th><th scope="col">Repository</th><th scope="col">State</th><th scope="col">Result</th><th scope="col">Updated</th></tr></thead>
    <tbody>
      {loading && <tr><td className="empty" colSpan={5}>Loading jobs…</td></tr>}
      {!loading && jobs.length === 0 && <tr><td className="empty" colSpan={5}>No jobs submitted yet.</td></tr>}
      {jobs.map((job) => <tr key={job.id} className={onSelect ? "selectable-row" : undefined} onClick={() => onSelect?.(job)}>
        <td><button className="table-link" type="button" onClick={() => onSelect?.(job)}>{job.id}</button></td><td>{job.repository}</td><td><Badge value={job.state} /></td><td><code>{shortSHA(job.result_sha)}</code></td><td>{date(job.updated_at)}</td>
      </tr>)}
    </tbody>
  </table></div>;
}

function ProjectsPage({ expert }: { expert: boolean }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [repository, setRepository] = useState("");
  const [branch, setBranch] = useState("main");
  const [provider, setProvider] = useState<"local" | "github" | "gitlab">("local");
  const load = useCallback(async () => { setLoading(true); const result = await api.GET("/projects"); setProjects(result.data?.items ?? []); setLoading(false); }, []);
  useEffect(() => { void load(); }, [load]);
  const submit = async (event: FormEvent) => {
    event.preventDefault(); const id = repository.replace("/", "-");
    const response = await api.POST("/projects", { body: { id, provider, repository, default_branch: branch, ...(provider === "local" ? { local_remote_name: `${id}.git` } : {}) } });
    setMessage(response.data ? `Registered ${response.data.repository}.` : "The project could not be registered.");
    if (response.data) { setRepository(""); await load(); }
  };
  const sync = async (project: Project) => {
    setMessage(`Synchronizing ${project.repository}…`);
    const response = await api.POST("/projects/{projectID}/actions/sync", { params: { path: { projectID: project.id }, header: { "X-CSRF-Token": getCSRFToken() } } });
    setMessage(response.data ? `Synchronized ${project.repository} at ${shortSHA(response.data.base_sha)}.` : `Synchronization failed for ${project.repository}.`);
    await load();
  };
  const toggleProject = async (project: Project) => {
    if (project.enabled && !window.confirm(`Disable ${project.repository}? Existing jobs and audit history will be retained.`)) return;
    let succeeded = false;
    if (project.enabled) {
      const response = await api.DELETE("/projects/{projectID}", { params: { path: { projectID: project.id }, header: { "X-CSRF-Token": getCSRFToken() } } });
      succeeded = Boolean(response.data);
    } else {
      const response = await api.POST("/projects", { body: { id: project.id, provider: project.provider, repository: project.repository, default_branch: project.default_branch, ...(project.provider === "local" ? { local_remote_name: project.local_remote_name } : {}) } });
      succeeded = Boolean(response.data);
    }
    setMessage(succeeded ? `${project.enabled ? "Disabled" : "Enabled"} ${project.repository}.` : `Could not ${project.enabled ? "disable" : "enable"} ${project.repository}.`);
    await load();
  };
  return <>
    <PageIntro>Registered repositories are the authorization boundary for worktrees, memory namespaces, schedules, and publication.</PageIntro>
    <Section title="Add repository" eyebrow="Registration">
      <form className="inline-form" onSubmit={(event) => void submit(event)}>
        <label>Repository<span>owner/repository</span><input required pattern="[A-Za-z0-9._-]+/[A-Za-z0-9._-]+" value={repository} onChange={(event) => setRepository(event.target.value)} placeholder="owner/repository" /></label>
        <label>Provider<select value={provider} onChange={(event) => setProvider(event.target.value as "local" | "github" | "gitlab")}><option value="local">Local bare remote</option><option value="github">GitHub App</option><option value="gitlab">GitLab</option></select></label>
        <label>Default branch<input required value={branch} onChange={(event) => setBranch(event.target.value)} /></label>
        <button type="submit">Register project</button>
      </form>
      {message && <p className="inline-message" role="status">{message}</p>}
    </Section>
    <Section title="Projects" eyebrow={`${projects.length} registered`} action={<button className="secondary-button" type="button" onClick={() => void load()} disabled={loading}><RefreshCw aria-hidden="true" />Refresh</button>}>
      {projects.length === 0 && !loading ? <Empty title="No repositories registered" detail="Register a local fixture or a repository authorized through the GitHub App." /> : <div className="card-grid">{projects.map((project) => <article className="resource-card" key={project.id}>
        <div className="resource-title"><FolderGit2 aria-hidden="true" /><div><h3>{project.repository}</h3><p>{project.id}</p></div><Badge value={project.enabled ? "enabled" : "disabled"} /></div>
        <dl><div><dt>Provider</dt><dd>{label(project.provider)}</dd></div><div><dt>Default branch</dt><dd><code>{project.default_branch}</code></dd></div><div><dt>Memory namespace</dt><dd><code>viking://resources/projects/{project.repository}/</code></dd></div><div><dt>Updated</dt><dd>{date(project.updated_at)}</dd></div></dl>
        <div className="card-actions"><button className="secondary-button" type="button" disabled={!project.enabled} onClick={() => void sync(project)}>Sync</button><button className="tertiary-button" type="button" disabled title="AGENTS.md is inspected in the isolated job context">Inspect AGENTS.md in job</button><button className={project.enabled ? "danger-button" : "secondary-button"} type="button" onClick={() => void toggleProject(project)}>{project.enabled ? "Remove" : "Enable"}</button></div>
        {expert && <p className="expert-note">Runner, commands, protected paths, and publication policy inherit the validated active configuration.</p>}
      </article>)}</div>}
    </Section>
  </>;
}

function JobsPage({ initialJobs, expert }: { initialJobs: Job[]; expert: boolean }) {
  const [jobs, setJobs] = useState(initialJobs);
  const [projects, setProjects] = useState<Project[]>([]);
  const [selected, setSelected] = useState<Job | null>(null);
  const [detail, setDetail] = useState<any>(null);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [evidenceGraph, setEvidenceGraph] = useState<EvidenceGraph | null>(null);
  const [task, setTask] = useState("");
  const [projectID, setProjectID] = useState("");
  const [rationale, setRationale] = useState("");
  const [password, setPassword] = useState("");
  const [message, setMessage] = useState("");
  const [goldenAbsolute, setGoldenAbsolute] = useState("0");
  const [goldenRelative, setGoldenRelative] = useState("0");
  const [goldenMaskDynamic, setGoldenMaskDynamic] = useState(false);
  const [goldenMaskSelectors, setGoldenMaskSelectors] = useState("");
  const load = useCallback(async () => { const [jobsResult, projectsResult] = await Promise.all([api.GET("/jobs"), api.GET("/projects")]); setJobs(jobsResult.data?.items ?? []); setProjects(projectsResult.data?.items ?? []); }, []);
  useEffect(() => { void load(); }, [load]);
  const inspect = async (job: Job) => { setSelected(job); const [response, artifactResponse, graphResponse] = await Promise.all([api.GET("/jobs/{jobID}", { params: { path: { jobID: job.id } } }), api.GET("/jobs/{jobID}/artifacts", { params: { path: { jobID: job.id } } }), api.GET("/jobs/{jobID}/evidence-graph", { params: { path: { jobID: job.id } } })]); setDetail(response.data ?? null); setArtifacts(artifactResponse.data?.items ?? []); setEvidenceGraph(graphResponse.data ?? null); };
  const submit = async (event: FormEvent) => { event.preventDefault(); const project = projects.find((item) => item.id === projectID); if (!project) return; const response = await api.POST("/jobs", { body: { project_id: project.id, repository: project.repository, task } }); if (response.data) { setTask(""); await load(); await inspect(response.data); } };
  const action = async (name: "cancel" | "retry") => { if (!selected) return; await (name === "cancel" ? api.POST("/jobs/{jobID}/actions/cancel", { params: { path: { jobID: selected.id } } }) : api.POST("/jobs/{jobID}/actions/retry", { params: { path: { jobID: selected.id } } })); await load(); };
  const inspectAction = async (name: "verify" | "review") => { if (!selected) return; const response = name === "verify" ? await api.POST("/jobs/{jobID}/actions/verify", { params: { path: { jobID: selected.id }, header: { "X-CSRF-Token": getCSRFToken() } } }) : await api.POST("/jobs/{jobID}/actions/review", { params: { path: { jobID: selected.id }, header: { "X-CSRF-Token": getCSRFToken() } } }); setMessage(response.data ? `${label(name)} request recorded.` : `${label(name)} request failed.`); };
  const disposeTestProposal = async (reportID: string, proposalID: string, disposition: "accepted" | "rejected" | "not_applicable") => {
    if (!selected || !rationale.trim()) { setMessage("Test Designer dispositions require a reviewer rationale."); return; }
    const response = await api.POST("/test-designer-reports/{reportID}/proposals/{proposalID}/actions/dispose", { params: { path: { reportID, proposalID }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { disposition, reason: rationale } });
    setMessage(response.data ? `${label(disposition)} recorded; ${response.data.pending_dispositions} pending disposition(s) remain.` : "Test Designer disposition failed.");
    if (response.data) { setRationale(""); await load(); const refreshed = response.data.job ?? selected; await inspect(refreshed); }
  };
  const approveGoldenComparison = async (reportID: string, comparisonID: string, approved: boolean) => {
    if (!selected || !rationale.trim() || !password) { setMessage("Golden update decisions require a rationale and current reviewer password."); return; }
    const auth = await api.POST("/auth/reauthenticate", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { password } });
    setPassword("");
    if (!auth.data) { setMessage("Reauthentication failed; golden decision was not recorded."); return; }
    const response = await api.POST("/golden-rehearsals/{reportID}/comparisons/{comparisonID}/actions/approve", { params: { path: { reportID, comparisonID }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { approved, reason: rationale } });
    setMessage(response.data ? `${approved ? "Approval" : "Rejection"} recorded; ${response.data.pending_approvals} pending golden approval(s) remain.` : "Golden approval failed.");
    if (response.data) { setRationale(""); await load(); const refreshed = response.data.job ?? selected; await inspect(refreshed); }
  };
  const configureGoldenProfile = async (reportID: string, comparisonID: string) => {
    if (!selected || !rationale.trim() || !password) { setMessage("Golden tolerance and mask profiles require a rationale and current reviewer password."); return; }
    const absolute = Number(goldenAbsolute);
    const relative = Number(goldenRelative);
    if (!Number.isFinite(absolute) || !Number.isFinite(relative) || absolute < 0 || relative < 0 || relative > 1) { setMessage("Golden tolerances must be non-negative, and relative tolerance must be at most 1."); return; }
    const auth = await api.POST("/auth/reauthenticate", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { password } });
    setPassword("");
    if (!auth.data) { setMessage("Reauthentication failed; golden profile was not recorded."); return; }
    const selectors = goldenMaskSelectors.split(",").map((value) => value.trim()).filter(Boolean);
    const response = await api.POST("/golden-rehearsals/{reportID}/comparisons/{comparisonID}/actions/configure-profile", { params: { path: { reportID, comparisonID }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { reason: rationale, tolerance_absolute: absolute, tolerance_relative: relative, mask_dynamic_regions: goldenMaskDynamic, mask_selectors: selectors } });
    setMessage(response.data ? `Golden profile ${response.data.profile.id} recorded.` : "Golden profile update failed.");
    if (response.data) { setRationale(""); await inspect(selected); }
  };
  const approveContract = async () => { if (!selected || !detail?.task_contract || !rationale.trim()) { setMessage("Task-contract approval requires a retained draft contract and rationale."); return; } const response = await api.POST("/jobs/{jobID}/task-contract/actions/approve", { params: { path: { jobID: selected.id }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { expected_version: detail.task_contract.version, reason: rationale } }); setMessage(response.data ? `Task contract approved at version ${response.data.contract.version}.` : "Task-contract approval failed; refresh and inspect the contract version."); if (response.data) { setRationale(""); const refreshed = await api.GET("/jobs/{jobID}", { params: { path: { jobID: selected.id } } }); if (refreshed.data) { setSelected(refreshed.data.job); setDetail(refreshed.data); } await load(); } };
  const approve = async () => { if (!selected || !rationale.trim() || !password) { setMessage("Approval requires a rationale and current reviewer password."); return; } const auth = await api.POST("/auth/reauthenticate", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { password } }); setPassword(""); if (!auth.data) { setMessage("Reauthentication failed; publication was not approved."); return; } const response = await api.POST("/jobs/{jobID}/actions/approve-publication", { params: { path: { jobID: selected.id }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { rationale } }); setMessage(response.data ? `Publication approved for exact commit ${shortSHA(response.data.job.result_sha)}.` : "Publication approval failed; inspect findings, verification, role, and exact commit state."); if (response.data) { setRationale(""); await load(); await inspect(response.data.job); } };
  return <>
    <PageIntro>Submit work, follow every durable phase, inspect exact commits and bounded outputs, and intervene through audited state-machine actions.</PageIntro>
    <Section title="Submit maintenance task" eyebrow="New job">
      <form className="job-form" onSubmit={(event) => void submit(event)}><label>Project<select required value={projectID} onChange={(event) => setProjectID(event.target.value)}><option value="">Select a registered project</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.repository}</option>)}</select></label><label>Task<textarea required maxLength={1048576} rows={3} value={task} onChange={(event) => setTask(event.target.value)} placeholder="Describe the bounded maintenance task and expected outcome." /></label><button type="submit">Queue job</button></form>
    </Section>
    <Section title="Job history" eyebrow="Durable queue" action={<button className="secondary-button" type="button" onClick={() => void load()}><RefreshCw aria-hidden="true" />Refresh</button>}><JobTable jobs={jobs} onSelect={(job) => void inspect(job)} /></Section>
    {selected && <Section title={`Job ${selected.id}`} eyebrow="Inspection">
      <div className="detail-grid"><article className="detail-panel"><h3>Task and commits</h3><p>{selected.task}</p><dl><div><dt>State</dt><dd><Badge value={selected.state} /></dd></div><div><dt>Base SHA</dt><dd><code>{selected.base_sha || "Pending"}</code></dd></div><div><dt>Result SHA</dt><dd><code>{selected.result_sha || "Pending"}</code></dd></div><div><dt>Token budget</dt><dd>{selected.reserved_tokens.toLocaleString()} / {selected.max_tokens.toLocaleString()}</dd></div></dl><div className="card-actions"><button className="secondary-button" type="button" onClick={() => void action("retry")}>Retry / resume</button><button className="secondary-button" type="button" onClick={() => void inspectAction("verify")}>Inspect verification</button><button className="secondary-button" type="button" onClick={() => void inspectAction("review")}>Request review</button><button className="danger-button" type="button" disabled={["completed", "failed", "cancelled"].includes(selected.state)} onClick={() => void action("cancel")}>Cancel</button></div></article>
      <article className="detail-panel"><h3>Acceptance criteria</h3><pre>{JSON.stringify(selected.acceptance_criteria, null, 2)}</pre></article>
      {detail?.task_contract && <article className="detail-panel"><h3>Task contract</h3><dl><div><dt>Status</dt><dd><Badge value={detail.task_contract.status} /></dd></div><div><dt>Version</dt><dd>{detail.task_contract.version}</dd></div><div><dt>Contract hash</dt><dd><code>{shortSHA(detail.task_contract.contract_sha256)}</code></dd></div></dl><p>{detail.task_contract.requested_behavior}</p><details><summary>Questions, assumptions, evidence, and checklist</summary><pre>{JSON.stringify({ questions: detail.task_contract.questions, assumptions: detail.task_contract.assumptions, required_evidence: detail.task_contract.required_evidence, completion_checklist: detail.task_contract.completion_checklist }, null, 2)}</pre></details></article>}
      {detail?.risk_assessments?.[0] && <article className="detail-panel"><h3>Risk routing</h3><dl><div><dt>Level</dt><dd><Badge value={detail.risk_assessments[0].level} /></dd></div><div><dt>Score</dt><dd>{detail.risk_assessments[0].score}</dd></div><div><dt>Required stages</dt><dd>{detail.risk_assessments[0].routing.required_stages.map(label).join(", ")}</dd></div></dl><details><summary>Deterministic signals</summary><pre>{JSON.stringify(detail.risk_assessments[0].signals.filter((signal: any) => signal.matched), null, 2)}</pre></details></article>}
      {detail?.test_designer_reports?.length > 0 && <article className="detail-panel"><h3>Independent Test Designer</h3><dl><div><dt>Status</dt><dd><Badge value={detail.test_designer_reports[0].status} /></dd></div><div><dt>Risk</dt><dd><Badge value={detail.test_designer_reports[0].risk_level} /></dd></div><div><dt>Proposals</dt><dd>{detail.test_designer_reports[0].proposals.length}</dd></div><div><dt>Dispositions</dt><dd>{detail.test_designer_reports[0].proposals.filter((proposal: any) => proposal.disposition === "pending").length} pending</dd></div></dl><div className="finding-list compact-list">{detail.test_designer_reports[0].proposals.map((proposal: any) => <article className="finding-card" key={proposal.id}><header><Badge value={proposal.disposition} /><span>{label(proposal.category)}</span></header><h4>{proposal.claim}</h4><p>{proposal.rationale}</p>{proposal.disposition_reason && <p>Disposition reason: {proposal.disposition_reason}</p>}<details><summary>Suggested tests and golden rehearsals</summary><pre>{JSON.stringify({ evidence_ids: proposal.evidence_ids, suggested_tests: proposal.suggested_tests, golden_rehearsals: proposal.golden_rehearsals }, null, 2)}</pre></details></article>)}</div><details open={expert}><summary>Raw reports and disposition history</summary><pre>{JSON.stringify({ reports: detail.test_designer_reports, dispositions: detail.test_designer_dispositions ?? [] }, null, 2)}</pre></details></article>}
      {detail?.golden_rehearsal_reports?.length > 0 && <article className="detail-panel"><h3>Golden and rehearsal gate</h3><dl><div><dt>Status</dt><dd><Badge value={detail.golden_rehearsal_reports[0].status} /></dd></div><div><dt>Comparisons</dt><dd>{detail.golden_rehearsal_reports[0].comparisons.length}</dd></div><div><dt>Approvals</dt><dd>{detail.golden_update_approvals?.length ?? 0}</dd></div><div><dt>Profiles</dt><dd>{detail.golden_comparison_profiles?.length ?? 0}</dd></div><div><dt>Policy</dt><dd>{detail.golden_rehearsal_reports[0].policy_summary}</dd></div></dl><details open={expert}><summary>Candidate-vs-approved comparisons, tolerances, masks, approvals, and typed profiles</summary><pre>{JSON.stringify({ reports: detail.golden_rehearsal_reports, approvals: detail.golden_update_approvals ?? [], profiles: detail.golden_comparison_profiles ?? [] }, null, 2)}</pre></details></article>}
      {detail?.documentation_manifests?.length > 0 && <article className="detail-panel"><h3>Documentation Agent</h3><dl><div><dt>Status</dt><dd><Badge value={detail.documentation_manifests[0].status} /></dd></div><div><dt>Requirements</dt><dd>{detail.documentation_manifests[0].requirements.length}</dd></div><div><dt>Changes</dt><dd>{detail.documentation_manifests[0].changes.length}</dd></div><div><dt>Findings</dt><dd>{(detail.findings ?? []).filter((finding: Finding) => finding.category.startsWith("documentation")).length}</dd></div><div><dt>Policy</dt><dd>{detail.documentation_manifests[0].policy_summary}</dd></div></dl><details open={expert}><summary>Impact, source-of-truth mappings, checks, edits, unsupported claims, and documentation findings</summary><pre>{JSON.stringify({ manifests: detail.documentation_manifests, findings: (detail.findings ?? []).filter((finding: Finding) => finding.category.startsWith("documentation")) }, null, 2)}</pre></details></article>}
      {detail?.policy_decisions?.length > 0 && <article className="detail-panel"><h3>OPA policy decision</h3><dl><div><dt>Decision</dt><dd><Badge value={detail.policy_decisions[0].outcome} /></dd></div><div><dt>Point</dt><dd>{label(detail.policy_decisions[0].decision_point)}</dd></div><div><dt>Bundle</dt><dd><code>{detail.policy_decisions[0].bundle_id}</code> v{detail.policy_decisions[0].bundle_version}</dd></div><div><dt>Fail closed</dt><dd>{detail.policy_decisions[0].fail_closed ? "Yes" : "No"}</dd></div></dl><p>{detail.policy_decisions[0].explanation}</p><details open={expert}><summary>Required stages, redacted inputs, and decision history</summary><pre>{JSON.stringify(detail.policy_decisions, null, 2)}</pre></details></article>}
      {detail?.agent_contract_validations?.length > 0 && <article className="detail-panel"><h3>Structured output contracts</h3><dl><div><dt>Latest phase</dt><dd>{label(detail.agent_contract_validations[0].phase)}</dd></div><div><dt>Schema</dt><dd>{label(detail.agent_contract_validations[0].contract_kind)} v{detail.agent_contract_validations[0].schema_version}</dd></div><div><dt>Schema hash</dt><dd><code>{shortSHA(detail.agent_contract_validations[0].schema_sha256)}</code></dd></div><div><dt>Valid</dt><dd><Badge value={detail.agent_contract_validations[0].valid ? "valid" : "invalid"} /></dd></div></dl><details><summary>Validation history</summary><pre>{JSON.stringify(detail.agent_contract_validations, null, 2)}</pre></details></article>}
      {detail?.configuration_snapshot && <article className="detail-panel"><h3>Accepted configuration</h3><p>This immutable redacted snapshot remains stable when live settings change.</p><dl><div><dt>Snapshot</dt><dd><code>{detail.configuration_snapshot.sha256}</code></dd></div><div><dt>Registry</dt><dd><code>{detail.configuration_snapshot.registry_hash}</code></dd></div><div><dt>Accepted</dt><dd>{date(detail.configuration_snapshot.created_at)}</dd></div></dl>{expert && <details><summary>Effective values and provenance</summary><pre>{JSON.stringify(detail.configuration_snapshot.document, null, 2)}</pre></details>}</article>}</div>
      {selected.state === "awaiting_task_approval" && <div className="detail-panel"><h3>Task-contract approval</h3><label>Approval rationale<textarea required rows={2} maxLength={4000} value={rationale} onChange={(event) => setRationale(event.target.value)} /></label><button type="button" disabled={!detail?.task_contract} onClick={() => void approveContract()}>Approve contract version {detail?.task_contract?.version ?? "—"}</button></div>}
      {selected.state === "awaiting_test_design_disposition" && <div className="detail-panel"><h3>Test Designer dispositions</h3><p>Each pending independent proposal needs an explicit reviewer decision before quality gates continue.</p><label>Disposition rationale<textarea required rows={2} maxLength={4000} value={rationale} onChange={(event) => setRationale(event.target.value)} /></label>{(detail?.test_designer_reports ?? []).flatMap((report: TestDesignerReport) => report.proposals.filter((proposal) => proposal.disposition === "pending").map((proposal) => <article className="finding-card" key={`${report.id}/${proposal.id}`}><header><Badge value="pending" /><span>{proposal.id}</span></header><p>{proposal.claim}</p><div className="card-actions"><button type="button" onClick={() => void disposeTestProposal(report.id, proposal.id, "accepted")}>Accept</button><button className="secondary-button" type="button" onClick={() => void disposeTestProposal(report.id, proposal.id, "rejected")}>Reject</button><button className="tertiary-button" type="button" onClick={() => void disposeTestProposal(report.id, proposal.id, "not_applicable")}>Not applicable</button></div></article>))}</div>}
      {selected.state === "awaiting_golden_approval" && <div className="detail-panel"><h3>Golden update approval</h3><p>Changed or missing approved golden artifacts require explicit reviewer approval before documentation and QC continue.</p><label>Golden decision rationale<textarea required rows={2} maxLength={1000} value={rationale} onChange={(event) => setRationale(event.target.value)} /></label><div className="inline-form-grid"><label>Absolute tolerance<input type="number" min={0} step="0.000001" value={goldenAbsolute} onChange={(event) => setGoldenAbsolute(event.target.value)} /></label><label>Relative tolerance<input type="number" min={0} max={1} step="0.000001" value={goldenRelative} onChange={(event) => setGoldenRelative(event.target.value)} /></label><label>Mask selectors<span>Comma-separated bounded selectors.</span><input value={goldenMaskSelectors} onChange={(event) => setGoldenMaskSelectors(event.target.value)} placeholder=".timestamp, [data-dynamic]" /></label><label className="checkbox-line"><input type="checkbox" checked={goldenMaskDynamic} onChange={(event) => setGoldenMaskDynamic(event.target.checked)} />Mask declared dynamic regions</label></div><label>Current reviewer password<input type="password" autoComplete="current-password" minLength={14} value={password} onChange={(event) => setPassword(event.target.value)} /></label>{(detail?.golden_rehearsal_reports ?? []).flatMap((report: any) => report.comparisons.filter((comparison: any) => comparison.approval_required).map((comparison: any) => <article className="finding-card" key={`${report.id}/${comparison.id}`}><header><Badge value={comparison.status} /><span>{comparison.id}</span></header><p>{comparison.diff_summary}</p><details open><summary>Rendered diff preview, tolerance, masks, and artifact identities</summary><pre>{JSON.stringify({ diff_summary: comparison.diff_summary, tolerance_policy: comparison.tolerance_policy, mask_policy: comparison.mask_policy, typed_profiles: (detail?.golden_comparison_profiles ?? []).filter((profile: any) => profile.report_id === report.id && profile.comparison_id === comparison.id), approved_artifact_sha256: comparison.approved_artifact_sha256, candidate_artifact_sha256: comparison.candidate_artifact_sha256 }, null, 2)}</pre></details><div className="card-actions"><button className="secondary-button" type="button" onClick={() => void configureGoldenProfile(report.id, comparison.id)}>Save tolerance/mask profile</button><button type="button" onClick={() => void approveGoldenComparison(report.id, comparison.id, true)}>Approve update</button><button className="secondary-button" type="button" onClick={() => void approveGoldenComparison(report.id, comparison.id, false)}>Reject update</button></div></article>))}</div>}
      {selected.state === "awaiting_operator" && <div className="detail-panel"><h3>Exact-commit publication approval</h3><label>Reviewer rationale<textarea required rows={2} maxLength={4096} value={rationale} onChange={(event) => setRationale(event.target.value)} /></label><label>Current reviewer password<input type="password" autoComplete="current-password" minLength={14} value={password} onChange={(event) => setPassword(event.target.value)} /></label><button type="button" onClick={() => void approve()}>Approve draft publication for {shortSHA(selected.result_sha)}</button></div>}
      {message && <p className="inline-message" role="status">{message}</p>}
      <div className="timeline"><h3>State timeline</h3>{detail?.transitions?.map((transition: any, index: number, transitions: any[]) => { const elapsed = index > 0 ? Math.max(0, new Date(transition.created_at).getTime() - new Date(transitions[index - 1].created_at).getTime()) : 0; return <div className="timeline-row" key={transition.sequence}><span /><div><strong>{label(transition.to)}</strong><p>{transition.reason}{index > 0 ? ` · ${(elapsed / 1000).toFixed(1)}s in prior state` : " · queued"}</p></div><time>{date(transition.created_at)}</time></div>; })}</div>
      <details open={expert}><summary>Commands, logs, tests, analysis, diffs, resources, and phase outcomes</summary><pre>{JSON.stringify(detail?.phases ?? [], null, 2)}</pre></details>
      <h3>Retained artifacts</h3>{artifacts.length === 0 ? <Empty title="No retained artifacts" detail="Bounded logs, reports, diffs, and coverage outputs appear here when produced." /> : <div className="backup-list">{artifacts.map((artifact) => <article key={artifact.id}><div><strong>{label(artifact.kind)}</strong><p>{artifact.media_type} · {(artifact.bytes / 1024).toFixed(1)} KiB · <code>{artifact.sha256.slice(0, 12)}</code></p></div><a className="secondary-button" href={`/api/v1/jobs/${selected.id}/artifacts/${artifact.id}`}>Open</a></article>)}</div>}
      <h3>Evidence traceability graph</h3>{!evidenceGraph || evidenceGraph.nodes.length === 0 ? <Empty title="No graph evidence retained" detail="New retained artifacts are indexed as typed evidence nodes with produced edges from the job." /> : <div className="backup-list">{evidenceGraph.edges.map((edge) => { const from = evidenceGraph.nodes.find((node) => node.id === edge.from_node_id); const to = evidenceGraph.nodes.find((node) => node.id === edge.to_node_id); return <article key={edge.id}><div><strong>{label(edge.relationship)}</strong><p>{from?.label ?? edge.from_node_id} → {to?.label ?? edge.to_node_id}</p><p>{edge.reason} · {date(edge.created_at)}</p></div><code>{shortSHA(to?.subject_sha256)}</code></article>; })}</div>}
      {expert && evidenceGraph && <details><summary>Typed evidence nodes and edges</summary><pre>{JSON.stringify(evidenceGraph, null, 2)}</pre></details>}
    </Section>}
  </>;
}

function QualityPage({ jobs: initialJobs }: { jobs: Job[] }) {
  const [findings, setFindings] = useState<Finding[]>([]);
  const [loading, setLoading] = useState(true);
  const [rationales, setRationales] = useState<Record<string, string>>({});
  const [password, setPassword] = useState("");
  const [message, setMessage] = useState("");
  const load = useCallback(async () => { setLoading(true); const listed = await api.GET("/jobs"); const jobs = listed.data?.items ?? initialJobs; const results = await Promise.all(jobs.slice(0, 50).map((job) => api.GET("/jobs/{jobID}", { params: { path: { jobID: job.id } } }))); setFindings(results.flatMap((result) => result.data?.findings ?? [])); setLoading(false); }, [initialJobs]);
  useEffect(() => { void load(); }, [load]);
  const act = async (finding: Finding, action: "dispute" | "accept" | "waive" | "escalate") => {
    const key = `${finding.job_id}/${finding.id}`;
    const rationale = rationales[key]?.trim();
    if (!rationale) { setMessage("Record a rationale before changing a finding."); return; }
    if (action === "waive") {
      if (!password) { setMessage("Enter your current password before waiving a finding."); return; }
      const auth = await api.POST("/auth/reauthenticate", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { password } });
      setPassword("");
      if (!auth.data) { setMessage("Reauthentication failed; the finding was not waived."); return; }
    }
    const response = await api.POST("/jobs/{jobID}/findings/{findingID}/actions/{action}", { params: { path: { jobID: finding.job_id, findingID: finding.id, action }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { rationale, expected_version: finding.version } });
    setMessage(response.data ? `${label(action)} recorded for ${finding.id}.` : `${label(action)} could not be recorded.`);
    if (response.data) { setRationales((value) => ({ ...value, [key]: "" })); await load(); }
  };
  const counts = useMemo(() => ["blocker", "must_fix", "should_fix", "note"].map((severity) => [severity, findings.filter((finding) => finding.severity === severity).length] as const), [findings]);
  return <>
    <PageIntro>Quality findings retain evidence and history. Required open findings keep publication visibly blocked until deterministic verification or an authorized waiver.</PageIntro>
    <div className="metrics-grid compact">{counts.map(([severity, count]) => <Metric key={severity} icon={<ShieldCheck aria-hidden="true" />} name={label(severity)} value={String(count)} />)}</div>
    <label className="sensitive-password">Reviewer password for waiver<span>Only required for a waiver; cleared immediately after reauthentication.</span><input type="password" autoComplete="current-password" minLength={14} value={password} onChange={(event) => setPassword(event.target.value)} /></label>
    {message && <p className="inline-message" role="status">{message}</p>}
    <Section title="Findings" eyebrow="Independent QC">
      {loading ? <p className="loading-line" role="status">Loading retained findings…</p> : findings.length === 0 ? <Empty title="No findings retained" detail="Independent QC findings will appear here with their affected code, evidence, and required resolution." /> : <div className="finding-list">{findings.map((finding) => { const key = `${finding.job_id}/${finding.id}`; return <article className="finding-card" key={key}><header><Badge value={finding.severity} /><Badge value={finding.status} /><code>{finding.job_id}</code></header><h3>{finding.claim}</h3><p>{finding.required_resolution}</p><dl><div><dt>Category</dt><dd>{finding.category}</dd></div><div><dt>Verification</dt><dd>{finding.verification_method}</dd></div><div><dt>Cycle</dt><dd>{finding.last_seen_cycle}</dd></div></dl><details><summary>Evidence and location</summary><pre>{JSON.stringify(finding.location, null, 2)}</pre></details><label>Decision rationale<textarea rows={2} maxLength={4096} value={rationales[key] ?? ""} onChange={(event) => setRationales((value) => ({ ...value, [key]: event.target.value }))} /></label><div className="card-actions"><button className="secondary-button" type="button" disabled={finding.status !== "open"} onClick={() => void act(finding, "dispute")}>Dispute</button><button className="secondary-button" type="button" disabled={finding.status !== "disputed"} onClick={() => void act(finding, "accept")}>Accept</button><button className="tertiary-button" type="button" disabled={finding.status !== "open"} onClick={() => void act(finding, "waive")}>Waive with reauthentication</button><button className="tertiary-button" type="button" onClick={() => void act(finding, "escalate")}>Escalate</button></div></article>; })}</div>}
    </Section>
  </>;
}

function PolicyPage({ expert }: { expert: boolean }) {
  const [bundles, setBundles] = useState<PolicyBundle[]>([]);
  const [activations, setActivations] = useState<PolicyActivation[]>([]);
  const [simulations, setSimulations] = useState<PolicySimulation[]>([]);
  const [testRuns, setTestRuns] = useState<PolicyTestRun[]>([]);
  const [documentationProfile, setDocumentationProfile] = useState<DocumentationPolicyProfile | null>(null);
  const [documentationSimulation, setDocumentationSimulation] = useState<DocumentationPolicySimulationResult | null>(null);
  const [bundleID, setBundleID] = useState("");
  const [decisionPoint, setDecisionPoint] = useState("qc_requirement");
  const [simulationInput, setSimulationInput] = useState(`{"risk_level":"medium","result_sha":"0123456789abcdef0123456789abcdef01234567","full_verification_passed":true,"documentation_status":"passed","golden_status":"passed"}`);
  const [documentationInput, setDocumentationInput] = useState(`{
  "paths": ["internal/api/openapi.yaml", "cmd/maintainctl/main.go"],
  "change_classes": ["public_api", "cli"],
  "risk_level": "medium",
  "languages": ["go"],
  "capability_packs": [],
  "labels": []
}`);
  const [testsJSON, setTestsJSON] = useState(`[
  {
    "id": "QC-ALLOW",
    "decision_point": "qc_requirement",
    "input": {
      "risk_level": "medium",
      "result_sha": "0123456789abcdef0123456789abcdef01234567",
      "full_verification_passed": true,
      "documentation_status": "passed",
      "golden_status": "passed"
    },
    "want_allowed": true
  }
]`);
  const [regoVersion, setRegoVersion] = useState("");
  const [regoReason, setRegoReason] = useState("");
  const [regoSource, setRegoSource] = useState(`package codemaintainer.policy

default decision := {
  "allowed": false,
  "outcome": "deny",
  "required_stages": ["baseline", "qc"],
  "explanation": "policy denied by default",
}
`);
  const [activationReason, setActivationReason] = useState("");
  const [message, setMessage] = useState("");
  const load = useCallback(async () => {
    const [bundleResponse, activationResponse, simulationResponse, testResponse, documentationPolicyResponse] = await Promise.all([
      api.GET("/policies/bundles", { params: { query: { limit: 100 } } }),
      api.GET("/policies/activations", { params: { query: { limit: 100 } } }),
      api.GET("/policies/simulations", { params: { query: { limit: 50 } } }),
      api.GET("/policies/test-runs", { params: { query: { limit: 100 } } }),
      api.GET("/documentation/policy/profile"),
    ]);
    const loaded = bundleResponse.data?.bundles ?? [];
    setBundles(loaded);
    setActivations(activationResponse.data?.activations ?? []);
    setSimulations(simulationResponse.data?.simulations ?? []);
    setTestRuns(testResponse.data?.test_runs ?? []);
    setDocumentationProfile(documentationPolicyResponse.data?.profile ?? null);
    setBundleID((current) => current || loaded.find((bundle) => bundle.status === "active")?.id || loaded[0]?.id || "");
  }, []);
  useEffect(() => { void load(); }, [load]);
  const parseObject = (value: string): Record<string, unknown> | null => {
    try {
      const parsed = JSON.parse(value);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) return parsed as Record<string, unknown>;
    } catch {
      // Reported below with a deterministic operator-facing message.
    }
    return null;
  };
  const parseTests = (): PolicyTestCase[] | null => {
    try {
      const parsed = JSON.parse(testsJSON);
      if (Array.isArray(parsed)) return parsed as PolicyTestCase[];
    } catch {
      // Reported below with a deterministic operator-facing message.
    }
    return null;
  };
  const simulate = async () => {
    const input = parseObject(simulationInput);
    if (!input) { setMessage("Simulation input must be a JSON object."); return; }
    const response = await api.POST("/policies/simulations", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { bundle_id: bundleID || undefined, decision_point: decisionPoint, input } });
    setMessage(response.data ? `Simulation ${response.data.simulation.status}: ${response.data.simulation.decision.explanation}` : "Policy simulation failed.");
    await load();
  };
  const simulateDocumentation = async () => {
    const input = parseObject(documentationInput);
    if (!input) { setMessage("Documentation simulation input must be a JSON object."); return; }
    const response = await api.POST("/documentation/policy/simulations", { body: input as any });
    setDocumentationSimulation(response.data?.simulation ?? null);
    setMessage(response.data ? response.data.simulation.policy_summary : "Documentation policy simulation failed.");
  };
  const runTests = async () => {
    if (!bundleID) { setMessage("Select a bundle before running tests."); return; }
    const tests = parseTests();
    if (!tests) { setMessage("Policy tests must be a JSON array."); return; }
    const response = await api.POST("/policies/bundles/{bundleID}/actions/test", { params: { path: { bundleID }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { tests } });
    setMessage(response.data ? `Policy test run ${response.data.test_run.status} with ${response.data.test_run.results.length} result(s).` : "Policy tests failed.");
    await load();
  };
  const createAdvanced = async () => {
    const tests = parseTests();
    if (!expert) { setMessage("Enable expert mode before creating advanced Rego bundles."); return; }
    if (!tests || !regoVersion.trim() || !regoReason.trim() || !regoSource.trim()) { setMessage("Advanced Rego creation requires version, reason, source, and test JSON."); return; }
    const response = await api.POST("/policies/bundles", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { version: regoVersion, reason: regoReason, rego_source: regoSource, tests } });
    setMessage(response.data ? `Created ${response.data.bundle.id}; retained tests ${response.data.test_run.status}.` : "Advanced policy bundle creation failed.");
    await load();
  };
  const activate = async (bundle: PolicyBundle, action: "activate" | "rollback" = "activate") => {
    const reason = activationReason.trim();
    if (!reason) { setMessage("Activation or rollback requires an operator reason."); return; }
    const response = await api.POST("/policies/bundles/{bundleID}/actions/activate", { params: { path: { bundleID: bundle.id }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { action, reason, staged_rollout_percent: 100 } });
    setMessage(response.data ? `${label(action)} recorded for ${bundle.id}.` : `${label(action)} failed; recent reauthentication may be required.`);
    await load();
  };
  const selectedRuns = testRuns.filter((run) => !bundleID || run.bundle_id === bundleID);
  return <>
    <PageIntro>Versioned policy bundles are fail-closed, source-retained, and test-gated before activation. Simulations retain redacted inputs and exact decisions for audit.</PageIntro>
    <div className="metrics-grid compact">
      <Metric icon={<ShieldCheck aria-hidden="true" />} name="Bundles" value={String(bundles.length)} detail={`${bundles.filter((bundle) => bundle.status === "active").length} active`} />
      <Metric icon={<ClipboardCheck aria-hidden="true" />} name="Test runs" value={String(testRuns.length)} detail={`${testRuns.filter((run) => run.status === "passed").length} passed`} />
      <Metric icon={<FileClock aria-hidden="true" />} name="Activations" value={String(activations.length)} detail="Append-only history" />
      <Metric icon={<CircleDot aria-hidden="true" />} name="Simulations" value={String(simulations.length)} detail="Redacted retained inputs" />
    </div>
    {message && <p className="inline-message" role="status">{message}</p>}
    <Section title="Policy bundles" eyebrow="Source and interpretation" action={<button className="secondary-button" type="button" onClick={() => void load()}><RefreshCw aria-hidden="true" />Refresh</button>}>
      <label>Selected bundle<select value={bundleID} onChange={(event) => setBundleID(event.target.value)}>{bundles.map((bundle) => <option key={bundle.id} value={bundle.id}>{bundle.version} · {bundle.id}</option>)}</select></label>
      <label>Activation / rollback reason<input maxLength={4000} value={activationReason} onChange={(event) => setActivationReason(event.target.value)} placeholder="Required before activation or rollback" /></label>
      {bundles.length === 0 ? <Empty title="No bundles retained" detail="The built-in policy bundle will appear after the controller initializes storage." /> : <div className="card-grid">{bundles.map((bundle) => {
        const runs = testRuns.filter((run) => run.bundle_id === bundle.id).slice(0, 3);
        return <article className="resource-card" key={bundle.id}><div className="resource-title"><ShieldCheck aria-hidden="true" /><div><h3>{bundle.version}</h3><p>{bundle.id}</p></div><Badge value={bundle.status} /></div><dl><div><dt>Source</dt><dd>{label(bundle.source_kind)}</dd></div><div><dt>Source hash</dt><dd><code>{shortSHA(bundle.source_sha256)}</code></dd></div><div><dt>Compiled hash</dt><dd><code>{shortSHA(bundle.compiled_sha256)}</code></dd></div><div><dt>Created</dt><dd>{date(bundle.created_at)}</dd></div></dl><details open={expert}><summary>Structured interpretation and retained source</summary><pre>{JSON.stringify({ rules: bundle.structured_rules, rego_source: bundle.rego_source, reason: bundle.reason }, null, 2)}</pre></details><details><summary>Recent retained tests</summary>{runs.length === 0 ? <p>No retained tests for this bundle.</p> : <pre>{JSON.stringify(runs, null, 2)}</pre>}</details><div className="card-actions"><button type="button" disabled={bundle.status === "active"} onClick={() => void activate(bundle)}>Activate</button><button className="tertiary-button" type="button" disabled={bundle.status === "active"} onClick={() => void activate(bundle, "rollback")}>Rollback to this bundle</button></div></article>;
      })}</div>}
    </Section>
    <Section title="Simulation and retained tests" eyebrow="Pre-activation evidence">
      <div className="two-column">
        <div><label>Decision point<input value={decisionPoint} onChange={(event) => setDecisionPoint(event.target.value)} /></label><label>Simulation input<textarea rows={8} value={simulationInput} onChange={(event) => setSimulationInput(event.target.value)} /></label><button type="button" disabled={!decisionPoint.trim()} onClick={() => void simulate()}>Simulate policy decision</button></div>
        <div><label>Policy tests JSON<textarea rows={12} value={testsJSON} onChange={(event) => setTestsJSON(event.target.value)} /></label><button className="secondary-button" type="button" disabled={!bundleID} onClick={() => void runTests()}>Run retained tests</button></div>
      </div>
      <details open={expert}><summary>Selected bundle test coverage</summary><pre>{JSON.stringify(selectedRuns, null, 2)}</pre></details>
      <details><summary>Recent simulations</summary><pre>{JSON.stringify(simulations.slice(0, 10), null, 2)}</pre></details>
    </Section>
    <Section title="Documentation policy impact" eyebrow="Required docs and render checks">
      <div className="two-column">
        <div>
          <p>{documentationProfile?.summary ?? "The documentation policy profile will appear after the controller responds."}</p>
          <dl><div><dt>Profile</dt><dd>{documentationProfile ? `${documentationProfile.version} · ${documentationProfile.rules.length} rules` : "loading"}</dd></div><div><dt>Tool profile</dt><dd>{documentationProfile?.tool_profile.id ?? "loading"}</dd></div></dl>
          <details open={expert}><summary>Rules, tool checks, and preview modes</summary><pre>{JSON.stringify(documentationProfile, null, 2)}</pre></details>
        </div>
        <div>
          <label>Change evidence JSON<textarea rows={12} value={documentationInput} onChange={(event) => setDocumentationInput(event.target.value)} /></label>
          <button type="button" onClick={() => void simulateDocumentation()}>Simulate documentation impact</button>
        </div>
      </div>
      {documentationSimulation && <div className="card-grid"><article className="resource-card"><div className="resource-title"><BookOpen aria-hidden="true" /><div><h3>Required documents</h3><p>{documentationSimulation.policy_summary}</p></div><Badge value={documentationSimulation.no_documentation_required ? "not_required" : "required"} /></div><dl><div><dt>Rules</dt><dd>{documentationSimulation.matched_rules.length}</dd></div><div><dt>Requirements</dt><dd>{documentationSimulation.requirements.length}</dd></div><div><dt>Checks</dt><dd>{documentationSimulation.checks.length}</dd></div><div><dt>Publication ready</dt><dd>{documentationSimulation.publication_ready ? "Yes" : "No"}</dd></div></dl><details open><summary>Requirements, checks, reviewers, source mappings, and gates</summary><pre>{JSON.stringify(documentationSimulation, null, 2)}</pre></details></article></div>}
    </Section>
    {expert && <Section title="Advanced Rego authoring" eyebrow="Administrator and reauthentication required">
      <label>Version<input value={regoVersion} onChange={(event) => setRegoVersion(event.target.value)} placeholder="2026.07.24.1" /></label>
      <label>Reason<input value={regoReason} onChange={(event) => setRegoReason(event.target.value)} placeholder="Why this policy bundle should exist" /></label>
      <label>Rego source<textarea rows={14} value={regoSource} onChange={(event) => setRegoSource(event.target.value)} /></label>
      <button type="button" onClick={() => void createAdvanced()}>Format, test, and retain advanced bundle</button>
    </Section>}
    <Section title="Activation history" eyebrow="Append-only">
      {activations.length === 0 ? <Empty title="No activations retained" detail="Activation and rollback records appear here with actor, reason, rollout percentage, and prior bundle." /> : <div className="backup-list">{activations.map((activation) => <article key={activation.id}><div><strong>{activation.bundle_version}</strong><p>{label(activation.action)} · {activation.actor_id} · {date(activation.created_at)}</p><p>{activation.reason}</p></div><Badge value={`${activation.staged_rollout_percent}%`} /></article>)}</div>}
    </Section>
  </>;
}

function ModelsPage({ status, expert }: { status: SystemStatus | null; expert: boolean }) {
  const [profiles, setProfiles] = useState<ModelProfile[]>([]);
  const [modelStatus, setModelStatus] = useState<ModelStatus | null>(null);
  const [runtimeBenchmarks, setRuntimeBenchmarks] = useState<RuntimeBenchmark[]>([]);
  const [providerStatus, setProviderStatus] = useState<ProviderGatewayStatus | null>(null);
  const [providerDecision, setProviderDecision] = useState<ProviderRouteDecision | null>(null);
  const [providerProbe, setProviderProbe] = useState<ProviderCapabilityProbe | null>(null);
  const [routeInput, setRouteInput] = useState(`{
  "project_id": "owner-repo",
  "role": "implementation",
  "purpose": "operator egress preview for a structured implementation packet",
  "data_classes": ["task_metadata", "candidate_diff"],
  "estimated_bytes": 4096,
  "estimated_tokens": 1024,
  "requires_structured_output": true
}`);
  const [message, setMessage] = useState("");
  const load = useCallback(async () => { const [modelResponse, providerResponse, benchmarkResponse] = await Promise.all([api.GET("/models"), api.GET("/model-providers/status"), api.GET("/models/runtime-benchmarks")]); setProfiles(modelResponse.data?.items ?? []); setModelStatus(modelResponse.data?.status ?? null); setProviderStatus(providerResponse.data ?? null); setRuntimeBenchmarks(benchmarkResponse.data?.items ?? []); }, []);
  useEffect(() => { void load(); }, [load]);
  const modelAction = async (profile: ModelProfile, action: "benchmark" | "load") => {
    const params = { path: { profileID: profile.id }, header: { "X-CSRF-Token": getCSRFToken() } };
    const response = action === "benchmark" ? await api.POST("/models/{profileID}/actions/benchmark", { params }) : await api.POST("/models/{profileID}/actions/load", { params });
    setMessage(response.data ? `${label(action)} completed for ${profile.id}.` : `${label(action)} failed for ${profile.id}.`);
    await load();
  };
  const unload = async () => { const response = await api.POST("/models/actions/unload", { params: { header: { "X-CSRF-Token": getCSRFToken() } } }); setMessage(response.data ? "The resident model was unloaded." : "Model unload failed."); await load(); };
  const simulateRoute = async () => {
    let input: ProviderRouteRequest;
    try {
      const parsed = JSON.parse(routeInput);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("object required");
      input = parsed as ProviderRouteRequest;
    } catch {
      setMessage("Provider route simulation input must be a JSON object.");
      return;
    }
    const response = await api.POST("/model-providers/routes/simulations", { body: input });
    setProviderDecision(response.data?.decision ?? null);
    setMessage(response.data ? `${label(response.data.decision.status)}: ${response.data.decision.reason}` : "Provider route simulation failed.");
    if (response.data) await load();
  };
  const probeProviderModel = async (modelID: string) => {
    const response = await api.POST("/model-providers/models/{modelID}/actions/probe", { params: { path: { modelID }, header: { "X-CSRF-Token": getCSRFToken() } } });
    setProviderProbe(response.data?.probe ?? null);
    setMessage(response.data ? `${label(response.data.probe.status)} capability probe retained for ${modelID}.` : `Capability probe failed for ${modelID}.`);
    if (response.data) await load();
  };
  const remoteProfiles = providerStatus?.providers.filter((provider) => provider.remote) ?? [];
  const enabledRemoteProfiles = remoteProfiles.filter((provider) => provider.enabled);
  const providerByID = new Map((providerStatus?.providers ?? []).map((provider) => [provider.id, provider]));
  const probesByModel = new Map((providerStatus?.recent_capability_probes ?? []).map((probe) => [probe.model_profile_id, probe]));
  const benchmarksByProfile = new Map(runtimeBenchmarks.map((benchmark) => [benchmark.profile_id, benchmark]));
  return <>
    <PageIntro>Models are imported only through checksum-bound manifests. Remote providers are optional and route through the controller-owned gateway with retained egress previews.</PageIntro>
    <div className="metrics-grid"><Metric icon={<Cpu aria-hidden="true" />} name="Load state" value={modelStatus?.state ?? status?.components.model ?? "Unknown"} detail={modelStatus?.profile_id || "No weights required for CI"} /><Metric icon={<MemoryStick aria-hidden="true" />} name="Resident memory" value={`${((modelStatus?.memory_bytes ?? 0) / 1073741824).toFixed(2)} GiB`} detail="One model maximum" /><Metric icon={<Gauge aria-hidden="true" />} name="Inference timing" value={`${modelStatus?.prompt_tokens_second ?? 0} / ${modelStatus?.decode_tokens_second ?? 0} tok/s`} detail="Prompt / decode" /><Metric icon={<HardDrive aria-hidden="true" />} name="Gateway routes" value={String(providerStatus?.routes.length ?? 0)} detail={`${enabledRemoteProfiles.length} remote enabled / ${remoteProfiles.length} remote profiles`} /></div>
    {message && <p className="inline-message" role="status">{message}</p>}
    <Section title="Installed manifests" eyebrow="Allow list">{profiles.length === 0 ? <Empty title="No model manifest installed" detail="Import a checksum-bound manifest or retain the deterministic CI profile." /> : <div className="card-grid">{profiles.map((profile) => {
      const latestBenchmark = benchmarksByProfile.get(profile.id);
      return <article className="resource-card" key={profile.id}><div className="resource-title"><BrainCircuit aria-hidden="true" /><div><h3>{profile.id}</h3><p>{profile.model_family}</p></div><Badge value={modelStatus?.profile_id === profile.id ? "loaded" : "unloaded"} /></div><dl><div><dt>Role</dt><dd>{label(profile.role)}</dd></div><div><dt>Quantization</dt><dd>{profile.quantization}</dd></div><div><dt>Context</dt><dd>{profile.context.toLocaleString()}</dd></div><div><dt>Hash</dt><dd><code>{profile.sha256?.slice(0, 16) || "Deterministic fake"}</code></dd></div><div><dt>Disk</dt><dd>{profile.bytes ? `${(profile.bytes / 1073741824).toFixed(2)} GiB` : "No weight file"}</dd></div></dl>{latestBenchmark && <details open><summary>Latest runtime benchmark</summary><pre>{JSON.stringify({ recommendation: latestBenchmark.recommendation, reason: latestBenchmark.reason, runtime_identity_sha256: latestBenchmark.runtime_identity_sha256.slice(0, 16), prompt_tokens_second: latestBenchmark.prompt_tokens_second, decode_tokens_second: latestBenchmark.decode_tokens_second, memory_bytes: latestBenchmark.memory_bytes, cache_mode: latestBenchmark.cache_mode, quality_status: latestBenchmark.quality_status, determinism_status: latestBenchmark.determinism_status }, null, 2)}</pre></details>}{expert && <details><summary>Manifest runtime policy</summary><pre>{JSON.stringify({ filename: profile.filename, source_uri: profile.source_uri, license: profile.license, threads: profile.threads, batch: profile.batch, ubatch: profile.ubatch, numa: profile.numa, minimum_ram_bytes: profile.min_ram_bytes, sampling: profile.sampling }, null, 2)}</pre></details>}<div className="card-actions"><button className="secondary-button" type="button" onClick={() => void modelAction(profile, "benchmark")}>Benchmark</button><button type="button" onClick={() => void modelAction(profile, "load")}>Load</button></div></article>;
    })}</div>}</Section>
    <Section title="Runtime benchmark lab" eyebrow="Quality-gated recommendations">
      {runtimeBenchmarks.length === 0 ? <Empty title="No retained runtime benchmarks" detail="Run a bounded benchmark for an allow-listed profile to retain speed, memory, quality, determinism, cache, and exact identity evidence." /> : <div className="audit-list">{runtimeBenchmarks.slice(0, 20).map((benchmark) => <div key={benchmark.id}><time>{date(benchmark.created_at)}</time><strong>{label(benchmark.recommendation)} · {benchmark.profile_id}</strong><span>{benchmark.prompt_tokens_second} prompt tok/s · {benchmark.decode_tokens_second} decode tok/s · {gibibytes(benchmark.memory_bytes)} · {label(benchmark.cache_mode)} cache · {benchmark.reason}</span></div>)}</div>}
      <p className="expert-note">Benchmarks never activate speculative decoding, KV-cache quantization, experimental kernels, or route changes automatically. Candidate recommendations require explicit operator review in a later profile-activation workflow.</p>
    </Section>
    <Section title="Provider gateway" eyebrow="Local, LAN, and remote boundaries" action={<button className="secondary-button" type="button" onClick={() => void load()}><RefreshCw aria-hidden="true" />Refresh</button>}>
      {!providerStatus ? <Empty title="Provider gateway status unavailable" detail="The controller will seed safe local defaults when storage is ready." /> : <div className="card-grid">
        {providerStatus.providers.map((provider) => <article className="resource-card" key={provider.id}><div className="resource-title"><BrainCircuit aria-hidden="true" /><div><h3>{provider.display_name}</h3><p>{provider.interface_family}</p></div><Badge value={provider.enabled ? "enabled" : "disabled"} /></div><dl><div><dt>Trust tier</dt><dd>{label(provider.trust_tier)}</dd></div><div><dt>Boundary</dt><dd>{provider.remote ? "Remote provider" : "Local-only"}</dd></div><div><dt>Credentials</dt><dd>{provider.credential_configured ? "Configured write-only" : "Not configured"}</dd></div><div><dt>Data classes</dt><dd>{provider.approved_data_classes.join(", ")}</dd></div></dl>{expert && <details><summary>Operator assertions</summary><pre>{JSON.stringify(provider.operator_assertions, null, 2)}</pre></details>}</article>)}
      </div>}
    </Section>
    <Section title="Provider model probes" eyebrow="Protocol conformance">
      {!providerStatus || providerStatus.models.length === 0 ? <Empty title="No provider model profiles" detail="Default local and fake remote model profiles appear after the provider gateway is initialized." /> : <div className="card-grid">{providerStatus.models.map((model) => {
        const provider = providerByID.get(model.provider_id);
        const lastProbe = providerProbe?.model_profile_id === model.id ? providerProbe : probesByModel.get(model.id);
        const capabilityList = Object.entries(model.capabilities).filter(([, enabled]) => enabled).map(([name]) => label(name));
        return <article className="resource-card" key={model.id}><div className="resource-title"><BrainCircuit aria-hidden="true" /><div><h3>{model.display_name}</h3><p>{provider?.interface_family ?? model.provider_id}</p></div><Badge value={lastProbe?.status ?? model.quality_status} /></div><dl><div><dt>Model profile</dt><dd>{model.id}</dd></div><div><dt>Provider</dt><dd>{provider?.display_name ?? model.provider_id}</dd></div><div><dt>Context</dt><dd>{model.context_limit.toLocaleString()} / {model.output_limit.toLocaleString()}</dd></div><div><dt>Capabilities</dt><dd>{capabilityList.slice(0, 5).join(", ") || "None declared"}</dd></div></dl>{lastProbe && <details open><summary>Latest capability probe</summary><pre>{JSON.stringify({ status: lastProbe.status, native_api_shape: lastProbe.native_api_shape, observed_model_id: lastProbe.observed_model_id, request_schema_sha256: lastProbe.request_schema_sha256.slice(0, 16), response_schema_sha256: lastProbe.response_schema_sha256.slice(0, 16), latency_millis: lastProbe.latency_millis, errors: lastProbe.errors }, null, 2)}</pre></details>}<div className="card-actions"><button type="button" aria-label={`Probe capabilities for ${model.id}`} onClick={() => void probeProviderModel(model.id)}>Probe capabilities</button></div></article>;
      })}</div>}
      <details><summary>Recent retained capability probes</summary>{(providerStatus?.recent_capability_probes.length ?? 0) === 0 ? <p>No provider capability probes retained.</p> : <div className="audit-list">{providerStatus!.recent_capability_probes.map((probe) => <div key={probe.id}><time>{date(probe.created_at)}</time><strong>{label(probe.status)} · {probe.model_profile_id}</strong><span>{probe.native_api_shape} · {probe.observed_model_id} · {probe.latency_millis}ms</span></div>)}</div>}</details>
    </Section>
    <Section title="Route and egress preview" eyebrow="Controller-owned simulation">
      <div className="two-column">
        <div><label>Route request JSON<textarea rows={13} value={routeInput} onChange={(event) => setRouteInput(event.target.value)} /></label><button type="button" onClick={() => void simulateRoute()}>Simulate provider route</button></div>
        <div>{providerDecision ? <article className="resource-card"><div className="resource-title"><ShieldCheck aria-hidden="true" /><div><h3>{providerDecision.egress_manifest.model_profile_id}</h3><p>{providerDecision.reason}</p></div><Badge value={providerDecision.status} /></div><dl><div><dt>Route</dt><dd>{providerDecision.egress_manifest.route_id}</dd></div><div><dt>Provider</dt><dd>{providerDecision.egress_manifest.provider_id}</dd></div><div><dt>Decision hash</dt><dd><code>{providerDecision.egress_manifest.manifest_sha256.slice(0, 16)}</code></dd></div><div><dt>Data classes</dt><dd>{providerDecision.egress_manifest.data_classes.join(", ")}</dd></div></dl><details open><summary>Egress manifest</summary><pre>{JSON.stringify(providerDecision.egress_manifest, null, 2)}</pre></details></article> : <Empty title="No provider route simulated" detail="Preview the exact destination, model, data classes, redactions, retention, and policy decision before any remote request." />}</div>
      </div>
      <details><summary>Recent egress manifests</summary>{(providerStatus?.recent_egress_manifests.length ?? 0) === 0 ? <p>No provider egress manifests retained.</p> : <div className="audit-list">{providerStatus!.recent_egress_manifests.map((manifest) => <div key={manifest.id}><time>{date(manifest.created_at)}</time><strong>{manifest.policy_decision} · {manifest.model_profile_id}</strong><span>{manifest.project_id} · {manifest.data_classes.join(", ")} · {manifest.decision_reason}</span></div>)}</div>}</details>
    </Section>
    <Section title="Safe model actions" eyebrow="Supervisor"><div className="action-strip"><button type="button" disabled title="Use the documented checksum-bound maintainctl import workflow">Import via manifest</button><button className="tertiary-button" type="button" disabled={modelStatus?.state !== "loaded"} onClick={() => void unload()}>Unload resident model</button></div>{expert && <p className="expert-note">Threads, batch, NUMA, context, and sampling remain manifest-bound. Arbitrary model paths and llama.cpp arguments are never accepted.</p>}</Section>
  </>;
}

function MemoryPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState("");
  const [query, setQuery] = useState("");
  const [records, setRecords] = useState<MemoryRecord[]>([]);
  const [message, setMessage] = useState("");
  const [rationale, setRationale] = useState("");
  const [password, setPassword] = useState("");
  useEffect(() => { void (async () => { const response = await api.GET("/projects"); const items = response.data?.items ?? []; setProjects(items); setProjectID(items[0]?.id ?? ""); })(); }, []);
  const search = useCallback(async () => { if (!projectID) return; const response = await api.GET("/projects/{projectID}/memory", { params: { path: { projectID }, query: { ...(query ? { q: query } : {}), limit: 100 } } }); setRecords(response.data?.items ?? []); setMessage(response.data ? `${response.data.items.length} scoped record(s).` : "Memory search failed."); }, [projectID, query]);
  useEffect(() => { void search(); }, [search]);
  const reindex = async () => { if (!projectID) return; const response = await api.POST("/projects/{projectID}/memory/actions/reindex", { params: { path: { projectID }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { mode: "vectors_only" } }); setMessage(response.data ? "Reindex operations were queued." : "Reindex request failed."); };
  const lifecycle = async (record: MemoryRecord, action: "promote" | "invalidate" | "delete") => { if (!projectID || !rationale.trim()) { setMessage("Enter a rationale before changing memory."); return; } if (action === "delete") { if (!password) { setMessage("Enter your administrator password before deleting memory content."); return; } const auth = await api.POST("/auth/reauthenticate", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { password } }); setPassword(""); if (!auth.data) { setMessage("Reauthentication failed; memory was not deleted."); return; } } const params = { path: { projectID, memoryID: record.id }, header: { "X-CSRF-Token": getCSRFToken() } }; const response = action === "promote" ? await api.POST("/projects/{projectID}/memory/{memoryID}/actions/promote", { params, body: { rationale, basis: "human_approval", expected_version: record.version } }) : action === "invalidate" ? await api.POST("/projects/{projectID}/memory/{memoryID}/actions/invalidate", { params, body: { rationale, expected_version: record.version } }) : await api.POST("/projects/{projectID}/memory/{memoryID}/actions/delete", { params, body: { rationale, expected_version: record.version } }); const succeeded = response.response.ok; setMessage(succeeded ? `${label(action)} recorded for ${record.id}.` : `${label(action)} failed.`); if (succeeded) { setRationale(""); await search(); } };
  const inspectExport = async () => { if (!projectID) return; const response = await api.GET("/projects/{projectID}/memory/export", { params: { path: { projectID } } }); setMessage(response.data ? `Export manifest ${response.data.manifest_hash.slice(0, 12)} contains ${response.data.records.length} record(s).` : "Memory export failed."); };
  return <>
    <PageIntro>Memory is filtered by exact repository namespace before ranking. Quarantined candidates never promote themselves.</PageIntro>
    <Section title="Browse project memory" eyebrow="Scoped retrieval" action={<button className="secondary-button" type="button" onClick={() => void reindex()} disabled={!projectID}><RefreshCw aria-hidden="true" />Reindex</button>}>
      <div className="filter-bar"><label>Project<select value={projectID} onChange={(event) => setProjectID(event.target.value)}>{projects.map((project) => <option key={project.id} value={project.id}>{project.repository}</option>)}</select></label><label>Search<input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search canonical project knowledge" /></label><button type="button" onClick={() => void search()} disabled={!projectID}>Search</button><button className="secondary-button" type="button" onClick={() => void inspectExport()} disabled={!projectID}>Validate export</button></div>
      <label>Lifecycle rationale<input maxLength={4096} value={rationale} onChange={(event) => setRationale(event.target.value)} placeholder="Required for promote, reject, or delete" /></label>
      <label className="sensitive-password">Administrator password for deletion<span>Only required to clear memory content; the tombstone and audit history remain.</span><input type="password" autoComplete="current-password" minLength={14} value={password} onChange={(event) => setPassword(event.target.value)} /></label>
      {message && <p className="inline-message" role="status">{message}</p>}
      {records.length === 0 ? <Empty title="No matching memory" detail="Verified cases, failed cases, patterns, and issue history will appear only within the selected project namespace." /> : <div className="memory-list">{records.map((record) => <article className="memory-card" key={record.id}><header><Badge value={record.status} /><span>{label(record.kind)}</span><code>v{record.version}</code></header><p>{record.content}</p><dl><div><dt>Provenance</dt><dd>{record.source_uri || "Controller candidate"}</dd></div><div><dt>Base commit</dt><dd><code>{shortSHA(record.base_commit)}</code></dd></div><div><dt>Hash</dt><dd><code>{record.content_hash.slice(0, 12)}</code></dd></div></dl><div className="card-actions"><button type="button" disabled={record.status !== "quarantine"} onClick={() => void lifecycle(record, "promote")}>Promote</button><button className="secondary-button" type="button" disabled={record.status === "deleted"} onClick={() => void lifecycle(record, "invalidate")}>Reject / invalidate</button><button className="danger-button" type="button" disabled={record.status === "deleted"} onClick={() => void lifecycle(record, "delete")}>Delete</button></div></article>)}</div>}
    </Section>
  </>;
}

function AutomationPage({ expert }: { expert: boolean }) {
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [requests, setRequests] = useState<any[]>([]);
  const [runs, setRuns] = useState<ScheduleRun[]>([]);
  const [proposals, setProposals] = useState<SkillProposal[]>([]);
  const [schedulerTopology, setSchedulerTopology] = useState<SchedulerTopology | null>(null);
  const [schedulerProfiles, setSchedulerProfiles] = useState<SchedulerResourceProfile[]>([]);
  const [schedulerDecisions, setSchedulerDecisions] = useState<SchedulerDecision[]>([]);
  const [schedulerDecision, setSchedulerDecision] = useState<SchedulerDecision | null>(null);
  const [schedulerInput, setSchedulerInput] = useState(`{
  "mode": "quality_latency",
  "active": [],
  "queued": [
    {
      "job_id": "job_simulated",
      "project_id": "project_simulated",
      "state": "queued",
      "priority": 100,
      "profile_id": "verification_offline",
      "created_at": "2026-07-24T02:45:00Z",
      "reason": "operator simulation"
    }
  ],
  "maintenance_window": true,
  "fairness_window": 1
}`);
  const [scheduleProject, setScheduleProject] = useState("");
  const [scheduleName, setScheduleName] = useState("");
  const [scheduleTask, setScheduleTask] = useState("");
  const [scheduleType, setScheduleType] = useState<"maintenance" | "sync" | "audit">("maintenance");
  const [message, setMessage] = useState("");
  const load = useCallback(async () => { const [scheduleResult, notificationResult, requestResult, projectResult, runResult, proposalResult, schedulerResult] = await Promise.all([api.GET("/schedules"), api.GET("/notifications"), api.GET("/automation-requests"), api.GET("/projects"), api.GET("/schedule-runs"), api.GET("/skill-proposals"), api.GET("/scheduler/status")]); setSchedules(scheduleResult.data?.items ?? []); setNotifications(notificationResult.data?.items ?? []); setRequests((requestResult.data as { items?: any[] } | undefined)?.items ?? []); setRuns(runResult.data?.items ?? []); setProposals(proposalResult.data?.items ?? []); setSchedulerTopology(schedulerResult.data?.topology ?? null); setSchedulerProfiles(schedulerResult.data?.profiles ?? []); setSchedulerDecisions(schedulerResult.data?.decisions ?? []); const projectItems = projectResult.data?.items ?? []; setProjects(projectItems); setScheduleProject((value) => value || projectItems[0]?.id || ""); }, []);
  useEffect(() => { void load(); }, [load]);
  const createSchedule = async (event: FormEvent) => { event.preventDefault(); const now = new Date(Date.now() + 60000).toISOString(); const response = await api.POST("/schedules", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { project_id: scheduleProject, name: scheduleName, task_type: scheduleType, task: scheduleTask, interval_seconds: 86400, window_start_minute: 0, window_end_minute: 1439, max_wall_seconds: 3600, max_tokens: 100000, enabled: true, next_run_at: now } }); setMessage(response.data ? `Created schedule ${response.data.name}.` : "Schedule creation failed."); if (response.data) { setScheduleName(""); setScheduleTask(""); await load(); } };
  const simulateScheduler = async () => { let input: SchedulerSimulationRequest; try { const parsed = JSON.parse(schedulerInput); if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("object required"); input = parsed as SchedulerSimulationRequest; } catch { setMessage("Scheduler simulation input must be a JSON object."); return; } const response = await api.POST("/scheduler/simulations", { body: input }); setSchedulerDecision(response.data?.decision ?? null); setMessage(response.data ? `${label(response.data.decision.status)}: ${response.data.decision.reason}` : "Scheduler simulation failed."); if (response.data) await load(); };
  const readNotification = async (item: Notification) => { const response = await api.POST("/notifications/{notificationID}/actions/read", { params: { path: { notificationID: item.id }, header: { "X-CSRF-Token": getCSRFToken() } } }); if (response.data) await load(); };
  const reviewProposal = async (proposal: SkillProposal, decision: "approve" | "reject") => { const response = await api.POST("/skill-proposals/{proposalID}/actions/review", { params: { path: { proposalID: proposal.id }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { decision, rationale: `${label(decision)} through the authenticated operator console.`, expected_version: proposal.version } }); setMessage(response.data ? `${label(decision)} recorded; proposals remain inert until separately installed.` : "Proposal review failed."); await load(); };
  return <>
    <PageIntro>Automation is serial, budgeted, maintenance-window constrained, and non-authoritative. Hermes may request review but cannot approve publication.</PageIntro>
    <div className="metrics-grid"><Metric icon={<FileClock aria-hidden="true" />} name="Schedules" value={String(schedules.length)} detail={`${schedules.filter((item) => item.enabled).length} enabled`} /><Metric icon={<Bell aria-hidden="true" />} name="Unread inbox" value={String(notifications.filter((item) => item.state === "delivered").length)} detail="Durable local notifications" /><Metric icon={<ShieldCheck aria-hidden="true" />} name="Approval requests" value={String(requests.length)} detail="No authority conferred" /><Metric icon={<TimerReset aria-hidden="true" />} name="Scheduler profiles" value={String(schedulerProfiles.length)} detail={`${schedulerDecisions.length} retained decision(s)`} /></div>
    <Section title="Resource scheduler" eyebrow="Topology, profiles, and simulation" action={<button className="secondary-button" type="button" onClick={() => void load()}><RefreshCw aria-hidden="true" />Refresh</button>}>
      <div className="card-grid">
        <article className="resource-card"><div className="resource-title"><Cpu aria-hidden="true" /><div><h3>{schedulerTopology?.host_id ?? "Unknown host"}</h3><p>Advisory capacity snapshot</p></div><Badge value={schedulerTopology?.thermal_state ?? "unknown"} /></div><dl><div><dt>CPU</dt><dd>{schedulerTopology ? `${schedulerTopology.physical_cores} physical / ${schedulerTopology.logical_cores} logical` : "—"}</dd></div><div><dt>Memory</dt><dd>{gibibytes(schedulerTopology?.total_memory_bytes)} total</dd></div><div><dt>Reserved</dt><dd>{gibibytes(schedulerTopology?.reserved_memory_bytes)}</dd></div><div><dt>I/O pressure</dt><dd>{schedulerTopology?.io_pressure ?? "—"}</dd></div></dl></article>
        <article className="resource-card"><div className="resource-title"><MemoryStick aria-hidden="true" /><div><h3>Safe profiles</h3><p>Bounded runner, model, memory, CPU, and network envelopes</p></div><Badge value="advisory" /></div>{schedulerProfiles.length === 0 ? <p>No resource profiles returned.</p> : <dl>{schedulerProfiles.map((profile) => <div key={profile.id}><dt>{profile.id}</dt><dd>{profile.cpu_cores} CPU · {gibibytes(profile.memory_bytes)} · {profile.network}</dd></div>)}</dl>}</article>
      </div>
      <div className="two-column">
        <div><label>Queue simulation JSON<textarea rows={14} value={schedulerInput} onChange={(event) => setSchedulerInput(event.target.value)} /></label><button type="button" onClick={() => void simulateScheduler()}>Simulate scheduler decision</button></div>
        <div>{schedulerDecision ? <article className="resource-card"><div className="resource-title"><TimerReset aria-hidden="true" /><div><h3>{schedulerDecision.selected_job_id || "No job selected"}</h3><p>{schedulerDecision.reason}</p></div><Badge value={schedulerDecision.status} /></div><dl><div><dt>Mode</dt><dd>{label(schedulerDecision.mode)}</dd></div><div><dt>Profile</dt><dd>{schedulerDecision.selected_profile_id || "—"}</dd></div><div><dt>Co-residence</dt><dd>{schedulerDecision.co_residence_safe ? "Safe" : "Unsafe"}</dd></div><div><dt>Fairness</dt><dd>{schedulerDecision.fairness_applied ? "Applied" : "Not applied"}</dd></div></dl><details open><summary>Decision evidence</summary><pre>{JSON.stringify(schedulerDecision, null, 2)}</pre></details></article> : <Empty title="No simulation run in this session" detail="Run a simulation to retain an auditable scheduling recommendation." />}</div>
      </div>
      <details><summary>Recent scheduler decision history</summary>{schedulerDecisions.length === 0 ? <p>No retained scheduler decisions.</p> : <div className="audit-list">{schedulerDecisions.slice(0, 30).map((decision) => <div key={decision.id}><time>{date(decision.created_at)}</time><strong>{decision.selected_job_id || decision.status}</strong><span>{label(decision.mode)} · {decision.reason}</span></div>)}</div>}</details>
      <p className="expert-note">This checkpoint records advisory decisions and unsafe co-residence deferrals. It does not yet replace the live durable lease dispatcher.</p>
    </Section>
    <Section title="Create recurring work" eyebrow="Bounded automation"><form className="inline-form" onSubmit={(event) => void createSchedule(event)}><label>Project<select required value={scheduleProject} onChange={(event) => setScheduleProject(event.target.value)}>{projects.map((project) => <option key={project.id} value={project.id}>{project.repository}</option>)}</select></label><label>Name<input required maxLength={128} value={scheduleName} onChange={(event) => setScheduleName(event.target.value)} /></label><label>Task type<select value={scheduleType} onChange={(event) => setScheduleType(event.target.value as typeof scheduleType)}><option value="maintenance">Maintenance</option><option value="sync">Sync</option><option value="audit">Audit</option></select></label><label>Task<input required maxLength={65536} value={scheduleTask} onChange={(event) => setScheduleTask(event.target.value)} /></label><button type="submit" disabled={!scheduleProject}>Create daily schedule</button></form>{message && <p className="inline-message" role="status">{message}</p>}<p className="expert-note">Safe defaults: daily, UTC all-day window, one-hour wall budget, and 100,000 tokens. Versioned expert editing remains API-driven.</p></Section>
    <Section title="Recurring schedules" eyebrow="UTC maintenance windows">{schedules.length === 0 ? <Empty title="No recurring work configured" detail="Create maintenance, synchronization, or audit schedules with explicit wall-time and token budgets." /> : <div className="card-grid">{schedules.map((schedule) => <article className="resource-card" key={schedule.id}><div className="resource-title"><FileClock aria-hidden="true" /><div><h3>{schedule.name}</h3><p>{schedule.project_id}</p></div><Badge value={schedule.enabled ? "enabled" : "disabled"} /></div><p>{schedule.task}</p><dl><div><dt>Task type</dt><dd>{label(schedule.task_type)}</dd></div><div><dt>Next run</dt><dd>{date(schedule.next_run_at)}</dd></div><div><dt>Budget</dt><dd>{schedule.max_wall_seconds}s / {schedule.max_tokens} tokens</dd></div></dl></article>)}</div>}</Section>
    <Section title="Operator inbox" eyebrow="Notifications" action={<button className="secondary-button" type="button" onClick={() => void load()}><RefreshCw aria-hidden="true" />Refresh</button>}>{notifications.length === 0 ? <Empty title="Inbox is clear" detail="Schedule outcomes and jobs requiring attention appear here transactionally." /> : <div className="notification-list">{notifications.map((item) => <article key={item.id}><Bell aria-hidden="true" /><div><strong>{item.title}</strong><p>{item.message}</p><time>{date(item.created_at)}</time></div><Badge value={item.state} />{item.state === "delivered" && <button className="tertiary-button" type="button" onClick={() => void readNotification(item)}>Mark read</button>}</article>)}</div>}</Section>
    <Section title="Automation history" eyebrow="Append-only runs">{runs.length === 0 ? <Empty title="No schedule runs" detail="Dispatches and maintenance-window skips appear here." /> : <div className="audit-list">{runs.slice(0, 30).map((run) => <div key={run.sequence}><time>{date(run.created_at)}</time><strong>{run.schedule_id}</strong><span>{label(run.status)} · {run.reason}</span></div>)}</div>}</Section>
    <Section title="Hermes skill proposals" eyebrow="Always inert">{proposals.length === 0 ? <Empty title="No proposed skills" detail="Hermes proposals are secret-scanned and cannot activate themselves." /> : <div className="card-grid">{proposals.map((proposal) => <article className="resource-card" key={proposal.id}><div className="resource-title"><ShieldCheck aria-hidden="true" /><div><h3>{proposal.name}</h3><p>{proposal.description}</p></div><Badge value={proposal.status} /></div><code>{proposal.content_hash.slice(0, 12)}</code>{proposal.status === "proposed" && <div className="card-actions"><button type="button" onClick={() => void reviewProposal(proposal, "approve")}>Approve as inert</button><button className="danger-button" type="button" onClick={() => void reviewProposal(proposal, "reject")}>Reject</button></div>}</article>)}</div>}</Section>
    {expert && <Section title="Hermes authority boundary" eyebrow="Exactly ten tools"><p className="expert-note">Hermes receives opaque API access for bounded submit/list/status/cancel/report, review and publication requests, scoped memory, schedule reads, and inert skill proposals. It receives no controller session, GitHub credential, worktree, Docker socket, or approval capability.</p></Section>}
  </>;
}

function AdministrationPage({ status }: { status: SystemStatus | null }) {
  const [revisions, setRevisions] = useState<ConfigRevision[]>([]);
  const [audit, setAudit] = useState<any[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [backups, setBackups] = useState<BackupRecord[]>([]);
  const [password, setPassword] = useState("");
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [role, setRole] = useState<"viewer" | "operator" | "reviewer" | "administrator">("viewer");
  const [newPassword, setNewPassword] = useState("");
  const [message, setMessage] = useState("");
  const [preflight, setPreflight] = useState<any>(null);
  const load = useCallback(async () => { const [revisionResult, auditResult, userResult, backupResult] = await Promise.all([api.GET("/config/revisions"), api.GET("/audit"), api.GET("/admin/users"), api.GET("/admin/backups")]); setRevisions(revisionResult.data?.items ?? []); setAudit((auditResult.data as { items?: any[] } | undefined)?.items ?? []); setUsers(userResult.data?.items ?? []); setBackups(backupResult.data?.items ?? []); }, []);
  useEffect(() => { void load(); }, [load]);
  const csrfParams = () => ({ header: { "X-CSRF-Token": getCSRFToken() } });
  const reauthenticate = async () => { if (!password) { setMessage("Enter your current password before a sensitive action."); return false; } const result = await api.POST("/auth/reauthenticate", { params: csrfParams(), body: { password } }); setPassword(""); if (!result.data) { setMessage("Reauthentication failed."); return false; } return true; };
  const createUser = async (event: FormEvent) => { event.preventDefault(); if (!await reauthenticate()) return; const result = await api.POST("/admin/users", { params: csrfParams(), body: { username, display_name: displayName, password: newPassword, role } }); if (!result.data) { setMessage("The user could not be created."); return; } setUsername(""); setDisplayName(""); setNewPassword(""); setMessage(`Created ${result.data.username}.`); await load(); };
  const createBackup = async () => { if (!await reauthenticate()) return; const result = await api.POST("/admin/backups", { params: csrfParams() }); setMessage(result.data ? `Created ${result.data.id}.` : "Backup creation failed."); await load(); };
  const updateUser = async (user: User, patch: { role?: User["role"]; disabled?: boolean }) => { if (!await reauthenticate()) return; const result = await api.PUT("/admin/users/{userID}", { params: { path: { userID: user.id }, ...csrfParams() }, body: { display_name: user.display_name, role: patch.role ?? user.role, disabled: patch.disabled ?? user.disabled } }); setMessage(result.data ? `Updated ${result.data.username}.` : `Could not update ${user.username}; the last administrator and active session are protected.`); await load(); };
  const rollbackRevision = async (revision: ConfigRevision) => { if (!await reauthenticate()) return; const result = await api.POST("/config/revisions/{revisionID}/rollback", { params: { path: { revisionID: revision.id } }, body: { reason: `Rollback requested from operator console to ${revision.id}` } }); setMessage(result.data ? `Created rollback revision ${result.data.id}.` : `Rollback to ${revision.id} failed validation.`); await load(); };
  const validateBackup = async (backup: BackupRecord) => { if (!await reauthenticate()) return; const result = await api.POST("/admin/backups/{backupID}/actions/restore", { params: { path: { backupID: backup.id }, ...csrfParams() }, body: { dry_run: true } }); setMessage(result.data && "compatible" in result.data ? `${backup.id} is checksum-valid and schema-compatible: ${result.data.compatible}.` : "Restore dry run failed."); };
  const checkUpdate = async () => { const result = await api.GET("/admin/update/preflight"); setPreflight(result.data ?? null); setMessage(result.data ? `Update preflight readiness: ${result.data.ready ? "ready" : "blocked"}.` : "Update preflight failed."); };
  return <>
    <PageIntro>Security-sensitive operations are authenticated, role checked, CSRF protected, append-only audited, and reauthenticated where required.</PageIntro>
    <div className="metrics-grid"><Metric icon={<Users aria-hidden="true" />} name="Local identity" value="Enabled" detail="Administrator / operator / reviewer" /><Metric icon={<BookOpen aria-hidden="true" />} name="Config revisions" value={String(revisions.length)} detail="Validated and rollback capable" /><Metric icon={<ShieldCheck aria-hidden="true" />} name="Audit events" value={String(audit.length)} detail="Append-only retained history" /><Metric icon={<Activity aria-hidden="true" />} name="Deployment" value={status?.profile ?? "Unknown"} detail={`Controller ${status?.version ?? "unknown"}`} /></div>
    <Section title="Users and roles" eyebrow="Access control">
      <form className="inline-form user-form" onSubmit={(event) => void createUser(event)}><label>Username<input required minLength={3} value={username} onChange={(event) => setUsername(event.target.value)} /></label><label>Display name<input required value={displayName} onChange={(event) => setDisplayName(event.target.value)} /></label><label>Role<select value={role} onChange={(event) => setRole(event.target.value as typeof role)}><option value="viewer">Viewer</option><option value="operator">Operator</option><option value="reviewer">Reviewer</option><option value="administrator">Administrator</option></select></label><label>Temporary password<input required type="password" minLength={14} value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></label><button type="submit">Create user</button></form>
      <div className="table-scroll user-table" tabIndex={0}><table><thead><tr><th>User</th><th>Role</th><th>Status</th><th>Updated</th><th>Action</th></tr></thead><tbody>{users.map((user) => <tr key={user.id}><td><strong>{user.display_name}</strong><br /><code>{user.username}</code></td><td><select aria-label={`Role for ${user.username}`} value={user.role} onChange={(event) => void updateUser(user, { role: event.target.value as User["role"] })}><option value="viewer">Viewer</option><option value="operator">Operator</option><option value="reviewer">Reviewer</option><option value="administrator">Administrator</option></select></td><td><Badge value={user.disabled ? "disabled" : "enabled"} /></td><td>{date(user.updated_at)}</td><td><button className={user.disabled ? "secondary-button" : "danger-button"} type="button" onClick={() => void updateUser(user, { disabled: !user.disabled })}>{user.disabled ? "Enable" : "Disable"}</button></td></tr>)}</tbody></table></div>
    </Section>
    <Section title="Configuration revisions" eyebrow="Declarative state">{revisions.length === 0 ? <Empty title="No revisions returned" detail="The active configuration and rollback history appear here." /> : <div className="table-scroll" tabIndex={0}><table><thead><tr><th>Revision</th><th>Actor</th><th>Reason</th><th>Rollback of</th><th>Created</th><th>Action</th></tr></thead><tbody>{revisions.map((revision, index) => <tr key={revision.id}><td><code>{revision.id}</code></td><td>{revision.actor_id}</td><td>{revision.reason}</td><td>{revision.rollback_of ?? "—"}</td><td>{date(revision.created_at)}</td><td><button className="tertiary-button" type="button" disabled={index === 0} onClick={() => void rollbackRevision(revision)}>Rollback</button></td></tr>)}</tbody></table></div>}</Section>
    <Section title="Backup, restore, and updates" eyebrow="Operator lifecycle"><label className="sensitive-password">Current administrator password<span>Required for user, backup, restore, and update actions; cleared after every attempt.</span><input type="password" autoComplete="current-password" minLength={14} value={password} onChange={(event) => setPassword(event.target.value)} /></label><div className="action-strip"><button type="button" onClick={() => void createBackup()}>Create backup</button><button className="secondary-button" type="button" onClick={() => void checkUpdate()}>Update preflight</button><button className="tertiary-button" type="button" disabled title="Use scripts/update.sh rollback after selecting the recorded known-good source">Rollback update from host</button></div>{message && <p className="inline-message" role="status">{message}</p>}{preflight && <><p className="expert-note">Controller {preflight.version} · {preflight.profile} profile · {preflight.backup_count} retained backup(s)</p><div className="health-grid preflight-grid">{preflight.checks.map((check: any) => <div key={check.name}><span>{check.detail}</span><Badge value={check.status} /></div>)}</div></>}<div className="backup-list">{backups.map((backup) => <article key={backup.id}><div><strong>{backup.id}</strong><p>{(backup.bytes / 1048576).toFixed(2)} MiB · {backup.encrypted ? "Encrypted" : "Mock profile plaintext"} · <code>{backup.sha256.slice(0, 12)}</code></p></div><button className="secondary-button" type="button" onClick={() => void validateBackup(backup)}>Restore dry run</button></article>)}</div><p className="expert-note">Production bundles require a private 256-bit key. Worktrees and model weights are excluded; database, audit/config state, retained artifacts, repository mirror state, and OpenViking state are checksum-bound.</p></Section>
    <Section title="Recent audit" eyebrow="Security events">{audit.length === 0 ? <Empty title="No audit events returned" detail="Authenticated actions will appear with actor, target, correlation ID, and time." /> : <div className="audit-list">{audit.slice(0, 30).map((event) => <div key={event.id}><time>{date(event.created_at)}</time><strong>{label(event.action)}</strong><span>{event.actor_id} · {event.target_type}/{event.target_id}</span></div>)}</div>}</Section>
  </>;
}
