import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import {
  Activity, Bell, BookOpen, Boxes, BrainCircuit, CheckCircle2, ChevronRight,
  CircleDot, ClipboardCheck, CloudCog, Cpu, Database, FileClock, FolderGit2,
  Gauge, GitBranch, HardDrive, ListChecks, MemoryStick, RefreshCw, Settings,
  ShieldCheck, SlidersHorizontal, TerminalSquare, TimerReset, Users,
} from "lucide-react";
import { api, getCSRFToken } from "./api/client";
import type { components } from "./api/schema";

type SystemStatus = components["schemas"]["SystemStatus"];
type Job = components["schemas"]["Job"];
type Project = components["schemas"]["Project"];
type MemoryRecord = components["schemas"]["MemoryRecord"];
type Schedule = components["schemas"]["Schedule"];
type Notification = components["schemas"]["Notification"];
type ConfigRevision = components["schemas"]["ConfigRevision"];
type Finding = components["schemas"]["Finding"];
type User = components["schemas"]["User"];
type BackupRecord = components["schemas"]["BackupRecord"];

type PageID = "first-run" | "overview" | "projects" | "jobs" | "quality" | "models" | "memory" | "github" | "automation" | "administration";

const navigation: Array<{ id: PageID; label: string; icon: ReactNode; group: "operate" | "integrate" | "manage" }> = [
  { id: "first-run", label: "First run", icon: <ListChecks aria-hidden="true" />, group: "operate" },
  { id: "overview", label: "Overview", icon: <Gauge aria-hidden="true" />, group: "operate" },
  { id: "projects", label: "Projects", icon: <FolderGit2 aria-hidden="true" />, group: "operate" },
  { id: "jobs", label: "Jobs", icon: <TerminalSquare aria-hidden="true" />, group: "operate" },
  { id: "quality", label: "QC / QA", icon: <ClipboardCheck aria-hidden="true" />, group: "operate" },
  { id: "models", label: "Models", icon: <BrainCircuit aria-hidden="true" />, group: "integrate" },
  { id: "memory", label: "Memory", icon: <Database aria-hidden="true" />, group: "integrate" },
  { id: "github", label: "GitHub", icon: <GitBranch aria-hidden="true" />, group: "integrate" },
  { id: "automation", label: "Scheduling / Hermes", icon: <FileClock aria-hidden="true" />, group: "integrate" },
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

function statusTone(value: string): "good" | "warn" | "bad" | "neutral" {
  if (["healthy", "completed", "canonical", "verified", "closed", "read", "draft_pr_created"].includes(value)) return "good";
  if (["failed", "cancelled", "open", "blocker", "must_fix", "stale", "rejected"].includes(value)) return "bad";
  if (["awaiting_operator", "awaiting_repair", "quarantine", "disputed", "delivered"].includes(value)) return "warn";
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
  const [page, setPage] = useState<PageID>("overview");
  const [expert, setExpert] = useState(false);
  const current = navigation.find((item) => item.id === page)!;

  useEffect(() => {
    document.title = `${current.label} | Local Code Maintainer`;
    document.querySelector<HTMLElement>("#page-heading")?.focus();
  }, [current.label]);

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
            onClick={() => setPage(item.id)}
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
      {page === "first-run" && <FirstRunPage status={initialStatus} jobs={initialJobs} navigate={setPage} />}
      {page === "overview" && <OverviewPage status={initialStatus} jobs={initialJobs} loading={initialLoading} reload={reloadOverview} />}
      {page === "projects" && <ProjectsPage expert={expert} />}
      {page === "jobs" && <JobsPage initialJobs={initialJobs} expert={expert} />}
      {page === "quality" && <QualityPage jobs={initialJobs} />}
      {page === "models" && <ModelsPage status={initialStatus} expert={expert} />}
      {page === "memory" && <MemoryPage />}
      {page === "github" && <GitHubPage jobs={initialJobs} />}
      {page === "automation" && <AutomationPage expert={expert} />}
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
    ["Git provider", "Review", "Use a local bare remote or configure the narrow GitHub App boundary.", "github"],
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
  return <>
    <PageIntro>Live operating state across the controller, queue, inference boundary, memory index, and publication workflow.</PageIntro>
    <div className="metrics-grid">
      <Metric icon={<Activity aria-hidden="true" />} name="Controller" value={status?.status ?? "Checking"} detail={`Uptime ${status ? Math.floor(status.uptime_seconds / 60) : 0} min`} />
      <Metric icon={<TimerReset aria-hidden="true" />} name="Active jobs" value={String(active.length)} detail={`${jobs.length} total retained`} />
      <Metric icon={<Cpu aria-hidden="true" />} name="Model" value={status?.components.model ?? "Unknown"} detail="Sequential supervisor" />
      <Metric icon={<Database aria-hidden="true" />} name="Memory" value={status?.components.memory ?? "Unknown"} detail="Project-scoped retrieval" />
      <Metric icon={<HardDrive aria-hidden="true" />} name="Runner" value={status?.components.runner ?? "Unknown"} detail="Policy-bound execution" />
      <Metric icon={<ShieldCheck aria-hidden="true" />} name="Recent failures" value={String(failed.length)} detail="Within retained jobs" />
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
  const [provider, setProvider] = useState<"local" | "github">("local");
  const load = useCallback(async () => { setLoading(true); const result = await api.GET("/projects"); setProjects(result.data?.items ?? []); setLoading(false); }, []);
  useEffect(() => { void load(); }, [load]);
  const submit = async (event: FormEvent) => {
    event.preventDefault(); const id = repository.replace("/", "-");
    const response = await api.POST("/projects", { body: { id, provider, repository, default_branch: branch, ...(provider === "local" ? { local_remote_name: `${id}.git` } : {}) } });
    setMessage(response.data ? `Registered ${response.data.repository}.` : "The project could not be registered.");
    if (response.data) { setRepository(""); await load(); }
  };
  return <>
    <PageIntro>Registered repositories are the authorization boundary for worktrees, memory namespaces, schedules, and publication.</PageIntro>
    <Section title="Add repository" eyebrow="Registration">
      <form className="inline-form" onSubmit={(event) => void submit(event)}>
        <label>Repository<span>owner/repository</span><input required pattern="[A-Za-z0-9._-]+/[A-Za-z0-9._-]+" value={repository} onChange={(event) => setRepository(event.target.value)} placeholder="owner/repository" /></label>
        <label>Provider<select value={provider} onChange={(event) => setProvider(event.target.value as "local" | "github")}><option value="local">Local bare remote</option><option value="github">GitHub App</option></select></label>
        <label>Default branch<input required value={branch} onChange={(event) => setBranch(event.target.value)} /></label>
        <button type="submit">Register project</button>
      </form>
      {message && <p className="inline-message" role="status">{message}</p>}
    </Section>
    <Section title="Projects" eyebrow={`${projects.length} registered`} action={<button className="secondary-button" type="button" onClick={() => void load()} disabled={loading}><RefreshCw aria-hidden="true" />Refresh</button>}>
      {projects.length === 0 && !loading ? <Empty title="No repositories registered" detail="Register a local fixture or a repository authorized through the GitHub App." /> : <div className="card-grid">{projects.map((project) => <article className="resource-card" key={project.id}>
        <div className="resource-title"><FolderGit2 aria-hidden="true" /><div><h3>{project.repository}</h3><p>{project.id}</p></div><Badge value={project.enabled ? "enabled" : "disabled"} /></div>
        <dl><div><dt>Provider</dt><dd>{label(project.provider)}</dd></div><div><dt>Default branch</dt><dd><code>{project.default_branch}</code></dd></div><div><dt>Memory namespace</dt><dd><code>viking://resources/projects/{project.repository}/</code></dd></div><div><dt>Updated</dt><dd>{date(project.updated_at)}</dd></div></dl>
        <div className="card-actions"><button className="secondary-button" type="button" disabled title="Repository synchronization endpoint is being configured">Sync</button><button className="tertiary-button" type="button" disabled>Inspect AGENTS.md</button></div>
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
  const [task, setTask] = useState("");
  const [projectID, setProjectID] = useState("");
  const load = useCallback(async () => { const [jobsResult, projectsResult] = await Promise.all([api.GET("/jobs"), api.GET("/projects")]); setJobs(jobsResult.data?.items ?? []); setProjects(projectsResult.data?.items ?? []); }, []);
  useEffect(() => { void load(); }, [load]);
  const inspect = async (job: Job) => { setSelected(job); const response = await api.GET("/jobs/{jobID}", { params: { path: { jobID: job.id } } }); setDetail(response.data ?? null); };
  const submit = async (event: FormEvent) => { event.preventDefault(); const project = projects.find((item) => item.id === projectID); if (!project) return; const response = await api.POST("/jobs", { body: { project_id: project.id, repository: project.repository, task } }); if (response.data) { setTask(""); await load(); await inspect(response.data); } };
  const action = async (name: "cancel" | "retry") => { if (!selected) return; await (name === "cancel" ? api.POST("/jobs/{jobID}/actions/cancel", { params: { path: { jobID: selected.id } } }) : api.POST("/jobs/{jobID}/actions/retry", { params: { path: { jobID: selected.id } } })); await load(); };
  return <>
    <PageIntro>Submit work, follow every durable phase, inspect exact commits and bounded outputs, and intervene through audited state-machine actions.</PageIntro>
    <Section title="Submit maintenance task" eyebrow="New job">
      <form className="job-form" onSubmit={(event) => void submit(event)}><label>Project<select required value={projectID} onChange={(event) => setProjectID(event.target.value)}><option value="">Select a registered project</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.repository}</option>)}</select></label><label>Task<textarea required maxLength={1048576} rows={3} value={task} onChange={(event) => setTask(event.target.value)} placeholder="Describe the bounded maintenance task and expected outcome." /></label><button type="submit">Queue job</button></form>
    </Section>
    <Section title="Job history" eyebrow="Durable queue" action={<button className="secondary-button" type="button" onClick={() => void load()}><RefreshCw aria-hidden="true" />Refresh</button>}><JobTable jobs={jobs} onSelect={(job) => void inspect(job)} /></Section>
    {selected && <Section title={`Job ${selected.id}`} eyebrow="Inspection">
      <div className="detail-grid"><article className="detail-panel"><h3>Task and commits</h3><p>{selected.task}</p><dl><div><dt>State</dt><dd><Badge value={selected.state} /></dd></div><div><dt>Base SHA</dt><dd><code>{selected.base_sha || "Pending"}</code></dd></div><div><dt>Result SHA</dt><dd><code>{selected.result_sha || "Pending"}</code></dd></div><div><dt>Token budget</dt><dd>{selected.reserved_tokens.toLocaleString()} / {selected.max_tokens.toLocaleString()}</dd></div></dl><div className="card-actions"><button className="secondary-button" type="button" onClick={() => void action("retry")}>Retry / resume</button><button className="danger-button" type="button" onClick={() => void action("cancel")}>Cancel</button></div></article>
      <article className="detail-panel"><h3>Acceptance criteria</h3><pre>{JSON.stringify(selected.acceptance_criteria, null, 2)}</pre></article></div>
      <div className="timeline"><h3>State timeline</h3>{detail?.transitions?.map((transition: any) => <div className="timeline-row" key={transition.sequence}><span /><div><strong>{label(transition.to)}</strong><p>{transition.reason}</p></div><time>{date(transition.created_at)}</time></div>)}</div>
      {expert && <details><summary>Phase outcomes</summary><pre>{JSON.stringify(detail?.phases ?? [], null, 2)}</pre></details>}
    </Section>}
  </>;
}

function QualityPage({ jobs }: { jobs: Job[] }) {
  const [findings, setFindings] = useState<Finding[]>([]);
  const [loading, setLoading] = useState(true);
  useEffect(() => { void (async () => { const results = await Promise.all(jobs.slice(0, 50).map((job) => api.GET("/jobs/{jobID}", { params: { path: { jobID: job.id } } }))); setFindings(results.flatMap((result) => result.data?.findings ?? [])); setLoading(false); })(); }, [jobs]);
  const counts = useMemo(() => ["blocker", "must_fix", "should_fix", "note"].map((severity) => [severity, findings.filter((finding) => finding.severity === severity).length] as const), [findings]);
  return <>
    <PageIntro>Quality findings retain evidence and history. Required open findings keep publication visibly blocked until deterministic verification or an authorized waiver.</PageIntro>
    <div className="metrics-grid compact">{counts.map(([severity, count]) => <Metric key={severity} icon={<ShieldCheck aria-hidden="true" />} name={label(severity)} value={String(count)} />)}</div>
    <Section title="Findings" eyebrow="Independent QC">
      {loading ? <p className="loading-line" role="status">Loading retained findings…</p> : findings.length === 0 ? <Empty title="No findings retained" detail="Independent QC findings will appear here with their affected code, evidence, and required resolution." /> : <div className="finding-list">{findings.map((finding) => <article className="finding-card" key={`${finding.job_id}-${finding.id}`}><header><Badge value={finding.severity} /><Badge value={finding.status} /><code>{finding.job_id}</code></header><h3>{finding.claim}</h3><p>{finding.required_resolution}</p><dl><div><dt>Category</dt><dd>{finding.category}</dd></div><div><dt>Verification</dt><dd>{finding.verification_method}</dd></div><div><dt>Cycle</dt><dd>{finding.last_seen_cycle}</dd></div></dl><details><summary>Evidence and location</summary><pre>{JSON.stringify(finding.location, null, 2)}</pre></details><div className="card-actions"><button className="secondary-button" type="button" disabled>Dispute</button><button className="secondary-button" type="button" disabled>Accept</button><button className="tertiary-button" type="button" disabled>Waive with reauthentication</button></div></article>)}</div>}
    </Section>
  </>;
}

function ModelsPage({ status, expert }: { status: SystemStatus | null; expert: boolean }) {
  return <>
    <PageIntro>Models are imported only through checksum-bound manifests. The supervisor keeps at most one allow-listed llama-server child loaded.</PageIntro>
    <div className="metrics-grid"><Metric icon={<Cpu aria-hidden="true" />} name="Load state" value={status?.components.model ?? "Unknown"} detail="No weights required for CI" /><Metric icon={<MemoryStick aria-hidden="true" />} name="Role isolation" value="Sequential" detail="Implementation and QC stay distinct" /><Metric icon={<Gauge aria-hidden="true" />} name="Inference timing" value="No samples" detail="Prompt / decode shown after a run" /><Metric icon={<HardDrive aria-hidden="true" />} name="Disk usage" value="Manifest-only" detail="Weights remain operator supplied" /></div>
    <Section title="Installed manifests" eyebrow="Allow list"><Empty title="No production model manifest installed" detail="The deterministic fake inference profile remains active. Import requires a local file, declared SHA-256, compatible role, context, and bounded runtime settings." /></Section>
    <Section title="Safe model actions" eyebrow="Supervisor"><div className="action-strip"><button type="button" disabled>Import manifest</button><button className="secondary-button" type="button" disabled>Benchmark profile</button><button className="secondary-button" type="button" disabled>Load</button><button className="tertiary-button" type="button" disabled>Unload</button></div>{expert && <p className="expert-note">Expert mode may expose validated threads, batch, NUMA, context, and sampling values after the model API is configured. Arbitrary llama.cpp arguments are never accepted.</p>}</Section>
  </>;
}

function MemoryPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState("");
  const [query, setQuery] = useState("");
  const [records, setRecords] = useState<MemoryRecord[]>([]);
  const [message, setMessage] = useState("");
  useEffect(() => { void (async () => { const response = await api.GET("/projects"); const items = response.data?.items ?? []; setProjects(items); setProjectID(items[0]?.id ?? ""); })(); }, []);
  const search = useCallback(async () => { if (!projectID) return; const response = await api.GET("/projects/{projectID}/memory", { params: { path: { projectID }, query: { ...(query ? { q: query } : {}), limit: 100 } } }); setRecords(response.data?.items ?? []); setMessage(response.data ? `${response.data.items.length} scoped record(s).` : "Memory search failed."); }, [projectID, query]);
  useEffect(() => { void search(); }, [search]);
  const reindex = async () => { if (!projectID) return; const response = await api.POST("/projects/{projectID}/memory/actions/reindex", { params: { path: { projectID }, header: { "X-CSRF-Token": getCSRFToken() } }, body: { mode: "vectors_only" } }); setMessage(response.data ? "Reindex operations were queued." : "Reindex request failed."); };
  return <>
    <PageIntro>Memory is filtered by exact repository namespace before ranking. Quarantined candidates never promote themselves.</PageIntro>
    <Section title="Browse project memory" eyebrow="Scoped retrieval" action={<button className="secondary-button" type="button" onClick={() => void reindex()} disabled={!projectID}><RefreshCw aria-hidden="true" />Reindex</button>}>
      <div className="filter-bar"><label>Project<select value={projectID} onChange={(event) => setProjectID(event.target.value)}>{projects.map((project) => <option key={project.id} value={project.id}>{project.repository}</option>)}</select></label><label>Search<input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search canonical project knowledge" /></label><button type="button" onClick={() => void search()} disabled={!projectID}>Search</button></div>
      {message && <p className="inline-message" role="status">{message}</p>}
      {records.length === 0 ? <Empty title="No matching memory" detail="Verified cases, failed cases, patterns, and issue history will appear only within the selected project namespace." /> : <div className="memory-list">{records.map((record) => <article className="memory-card" key={record.id}><header><Badge value={record.status} /><span>{label(record.kind)}</span><code>v{record.version}</code></header><p>{record.content}</p><dl><div><dt>Provenance</dt><dd>{record.source_uri || "Controller candidate"}</dd></div><div><dt>Base commit</dt><dd><code>{shortSHA(record.base_commit)}</code></dd></div><div><dt>Hash</dt><dd><code>{record.content_hash.slice(0, 12)}</code></dd></div></dl></article>)}</div>}
    </Section>
  </>;
}

function GitHubPage({ jobs }: { jobs: Job[] }) {
  const [projects, setProjects] = useState<Project[]>([]);
  useEffect(() => { void (async () => { const response = await api.GET("/projects"); setProjects(response.data?.items ?? []); })(); }, []);
  const github = projects.filter((project) => project.provider === "github");
  const drafts = jobs.filter((job) => ["draft_pr_created", "completed"].includes(job.state) && job.result_sha);
  return <>
    <PageIntro>The GitHub App boundary owns installation tokens, webhook authentication, synchronization, and idempotent draft publication. Secrets are never returned here.</PageIntro>
    <div className="metrics-grid"><Metric icon={<GitBranch aria-hidden="true" />} name="Repositories" value={String(github.length)} detail="GitHub App registrations" /><Metric icon={<CloudCog aria-hidden="true" />} name="Webhook" value={github.length ? "Configured" : "Disabled"} detail="HMAC and replay protected" /><Metric icon={<ShieldCheck aria-hidden="true" />} name="Permissions" value="Narrow" detail="Contents, metadata, pull requests" /><Metric icon={<Boxes aria-hidden="true" />} name="Draft PRs" value={String(drafts.length)} detail="Exact-result publications" /></div>
    <Section title="GitHub repositories" eyebrow="Installation scope">{github.length === 0 ? <Empty title="GitHub provider is disabled" detail="Register a GitHub project only after configuring an App installation. Local bare remotes remain available without credentials." /> : <div className="card-grid">{github.map((project) => <article className="resource-card" key={project.id}><h3>{project.repository}</h3><dl><div><dt>Default branch</dt><dd>{project.default_branch}</dd></div><div><dt>Sync status</dt><dd><Badge value="configured" /></dd></div></dl></article>)}</div>}</Section>
    <Section title="Publication audit" eyebrow="Draft-only boundary"><JobTable jobs={drafts} /></Section>
  </>;
}

function AutomationPage({ expert }: { expert: boolean }) {
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [requests, setRequests] = useState<any[]>([]);
  const load = useCallback(async () => { const [scheduleResult, notificationResult, requestResult] = await Promise.all([api.GET("/schedules"), api.GET("/notifications"), api.GET("/automation-requests")]); setSchedules(scheduleResult.data?.items ?? []); setNotifications(notificationResult.data?.items ?? []); setRequests((requestResult.data as { items?: any[] } | undefined)?.items ?? []); }, []);
  useEffect(() => { void load(); }, [load]);
  return <>
    <PageIntro>Automation is serial, budgeted, maintenance-window constrained, and non-authoritative. Hermes may request review but cannot approve publication.</PageIntro>
    <div className="metrics-grid"><Metric icon={<FileClock aria-hidden="true" />} name="Schedules" value={String(schedules.length)} detail={`${schedules.filter((item) => item.enabled).length} enabled`} /><Metric icon={<Bell aria-hidden="true" />} name="Unread inbox" value={String(notifications.filter((item) => item.state === "delivered").length)} detail="Durable local notifications" /><Metric icon={<ShieldCheck aria-hidden="true" />} name="Approval requests" value={String(requests.length)} detail="No authority conferred" /><Metric icon={<TimerReset aria-hidden="true" />} name="Queue policy" value="Serial" detail="Per-project recurring work" /></div>
    <Section title="Recurring schedules" eyebrow="UTC maintenance windows">{schedules.length === 0 ? <Empty title="No recurring work configured" detail="Create maintenance, synchronization, or audit schedules with explicit wall-time and token budgets." /> : <div className="card-grid">{schedules.map((schedule) => <article className="resource-card" key={schedule.id}><div className="resource-title"><FileClock aria-hidden="true" /><div><h3>{schedule.name}</h3><p>{schedule.project_id}</p></div><Badge value={schedule.enabled ? "enabled" : "disabled"} /></div><p>{schedule.task}</p><dl><div><dt>Task type</dt><dd>{label(schedule.task_type)}</dd></div><div><dt>Next run</dt><dd>{date(schedule.next_run_at)}</dd></div><div><dt>Budget</dt><dd>{schedule.max_wall_seconds}s / {schedule.max_tokens} tokens</dd></div></dl></article>)}</div>}</Section>
    <Section title="Operator inbox" eyebrow="Notifications" action={<button className="secondary-button" type="button" onClick={() => void load()}><RefreshCw aria-hidden="true" />Refresh</button>}>{notifications.length === 0 ? <Empty title="Inbox is clear" detail="Schedule outcomes and jobs requiring attention appear here transactionally." /> : <div className="notification-list">{notifications.map((item) => <article key={item.id}><Bell aria-hidden="true" /><div><strong>{item.title}</strong><p>{item.message}</p><time>{date(item.created_at)}</time></div><Badge value={item.state} /></article>)}</div>}</Section>
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
  const load = useCallback(async () => { const [revisionResult, auditResult, userResult, backupResult] = await Promise.all([api.GET("/config/revisions"), api.GET("/audit"), api.GET("/admin/users"), api.GET("/admin/backups")]); setRevisions(revisionResult.data?.items ?? []); setAudit((auditResult.data as { items?: any[] } | undefined)?.items ?? []); setUsers(userResult.data?.items ?? []); setBackups(backupResult.data?.items ?? []); }, []);
  useEffect(() => { void load(); }, [load]);
  const csrfParams = () => ({ header: { "X-CSRF-Token": getCSRFToken() } });
  const reauthenticate = async () => { if (!password) { setMessage("Enter your current password before a sensitive action."); return false; } const result = await api.POST("/auth/reauthenticate", { params: csrfParams(), body: { password } }); setPassword(""); if (!result.data) { setMessage("Reauthentication failed."); return false; } return true; };
  const createUser = async (event: FormEvent) => { event.preventDefault(); if (!await reauthenticate()) return; const result = await api.POST("/admin/users", { params: csrfParams(), body: { username, display_name: displayName, password: newPassword, role } }); if (!result.data) { setMessage("The user could not be created."); return; } setUsername(""); setDisplayName(""); setNewPassword(""); setMessage(`Created ${result.data.username}.`); await load(); };
  const createBackup = async () => { if (!await reauthenticate()) return; const result = await api.POST("/admin/backups", { params: csrfParams() }); setMessage(result.data ? `Created ${result.data.id}.` : "Backup creation failed."); await load(); };
  const validateBackup = async (backup: BackupRecord) => { if (!await reauthenticate()) return; const result = await api.POST("/admin/backups/{backupID}/actions/restore", { params: { path: { backupID: backup.id }, ...csrfParams() }, body: { dry_run: true } }); setMessage(result.data ? `${backup.id} is checksum-valid and schema-compatible: ${result.data.compatible}.` : "Restore dry run failed."); };
  return <>
    <PageIntro>Security-sensitive operations are authenticated, role checked, CSRF protected, append-only audited, and reauthenticated where required.</PageIntro>
    <div className="metrics-grid"><Metric icon={<Users aria-hidden="true" />} name="Local identity" value="Enabled" detail="Administrator / operator / reviewer" /><Metric icon={<BookOpen aria-hidden="true" />} name="Config revisions" value={String(revisions.length)} detail="Validated and rollback capable" /><Metric icon={<ShieldCheck aria-hidden="true" />} name="Audit events" value={String(audit.length)} detail="Append-only retained history" /><Metric icon={<Activity aria-hidden="true" />} name="Deployment" value={status?.profile ?? "Unknown"} detail="Component health below" /></div>
    <Section title="Users and roles" eyebrow="Access control">
      <form className="inline-form user-form" onSubmit={(event) => void createUser(event)}><label>Username<input required minLength={3} value={username} onChange={(event) => setUsername(event.target.value)} /></label><label>Display name<input required value={displayName} onChange={(event) => setDisplayName(event.target.value)} /></label><label>Role<select value={role} onChange={(event) => setRole(event.target.value as typeof role)}><option value="viewer">Viewer</option><option value="operator">Operator</option><option value="reviewer">Reviewer</option><option value="administrator">Administrator</option></select></label><label>Temporary password<input required type="password" minLength={14} value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></label><button type="submit">Create user</button></form>
      <div className="table-scroll user-table" tabIndex={0}><table><thead><tr><th>User</th><th>Role</th><th>Status</th><th>Updated</th></tr></thead><tbody>{users.map((user) => <tr key={user.id}><td><strong>{user.display_name}</strong><br /><code>{user.username}</code></td><td>{label(user.role)}</td><td><Badge value={user.disabled ? "disabled" : "enabled"} /></td><td>{date(user.updated_at)}</td></tr>)}</tbody></table></div>
    </Section>
    <Section title="Configuration revisions" eyebrow="Declarative state">{revisions.length === 0 ? <Empty title="No revisions returned" detail="The active configuration and rollback history appear here." /> : <div className="table-scroll" tabIndex={0}><table><thead><tr><th>Revision</th><th>Actor</th><th>Reason</th><th>Rollback of</th><th>Created</th></tr></thead><tbody>{revisions.map((revision) => <tr key={revision.id}><td><code>{revision.id}</code></td><td>{revision.actor_id}</td><td>{revision.reason}</td><td>{revision.rollback_of ?? "—"}</td><td>{date(revision.created_at)}</td></tr>)}</tbody></table></div>}</Section>
    <Section title="Backup, restore, and updates" eyebrow="Operator lifecycle"><label className="sensitive-password">Current administrator password<span>Required for user, backup, restore, and update actions; cleared after every attempt.</span><input type="password" autoComplete="current-password" minLength={14} value={password} onChange={(event) => setPassword(event.target.value)} /></label><div className="action-strip"><button type="button" onClick={() => void createBackup()}>Create backup</button><button className="secondary-button" type="button" disabled>Update preflight</button><button className="tertiary-button" type="button" disabled>Rollback update</button></div>{message && <p className="inline-message" role="status">{message}</p>}<div className="backup-list">{backups.map((backup) => <article key={backup.id}><div><strong>{backup.id}</strong><p>{(backup.bytes / 1048576).toFixed(2)} MiB · {backup.encrypted ? "Encrypted" : "Mock profile plaintext"} · <code>{backup.sha256.slice(0, 12)}</code></p></div><button className="secondary-button" type="button" onClick={() => void validateBackup(backup)}>Restore dry run</button></article>)}</div><p className="expert-note">Production bundles require a private 256-bit key. Worktrees and model weights are excluded; database, audit/config state, retained artifacts, and OpenViking state are checksum-bound.</p></Section>
    <Section title="Recent audit" eyebrow="Security events">{audit.length === 0 ? <Empty title="No audit events returned" detail="Authenticated actions will appear with actor, target, correlation ID, and time." /> : <div className="audit-list">{audit.slice(0, 30).map((event) => <div key={event.id}><time>{date(event.created_at)}</time><strong>{label(event.action)}</strong><span>{event.actor_id} · {event.target_type}/{event.target_id}</span></div>)}</div>}</Section>
  </>;
}
