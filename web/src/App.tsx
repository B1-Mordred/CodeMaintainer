import { FormEvent, useCallback, useEffect, useState } from "react";
import { Activity, Box, Cpu, Database, LogOut, RefreshCw } from "lucide-react";
import { api, getCSRFToken, setCSRFToken } from "./api/client";
import type { components } from "./api/schema";

type SystemStatus = components["schemas"]["SystemStatus"];
type Job = components["schemas"]["Job"];
type Principal = components["schemas"]["Principal"];
type AuthenticationResult = components["schemas"]["AuthenticationResult"];

type AuthScreen = "checking" | "bootstrap" | "login" | "authenticated";

function statusLabel(value: string): string {
  return value.replaceAll("_", " ");
}

export function App() {
  const [authScreen, setAuthScreen] = useState<AuthScreen>("checking");
  const [principal, setPrincipal] = useState<Principal | null>(null);
  const [status, setStatus] = useState<SystemStatus | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const establishSession = useCallback((result: AuthenticationResult) => {
    setCSRFToken(result.csrf_token);
    setPrincipal(result.principal);
    setAuthScreen("authenticated");
    setError(null);
  }, []);

  const initializeAuthentication = useCallback(async () => {
    setLoading(true);
    try {
      const statusResult = await api.GET("/auth/status");
      if (!statusResult.data) throw new Error("Authentication status is unavailable.");
      if (!statusResult.data.bootstrapped) {
        setAuthScreen("bootstrap");
        return;
      }
      const sessionResult = await api.GET("/auth/session");
      if (sessionResult.data) establishSession(sessionResult.data);
      else setAuthScreen("login");
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "The controller is unavailable.");
      setAuthScreen("login");
    } finally {
      setLoading(false);
    }
  }, [establishSession]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [statusResult, jobsResult] = await Promise.all([api.GET("/system/status"), api.GET("/jobs")]);
      if (statusResult.error || jobsResult.error || !statusResult.data || !jobsResult.data) {
        throw new Error("The controller returned an error.");
      }
      setStatus(statusResult.data);
      setJobs(jobsResult.data.items);
      setError(null);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "The controller is unavailable.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void initializeAuthentication(); }, [initializeAuthentication]);
  useEffect(() => { if (authScreen === "authenticated") void load(); }, [authScreen, load]);

  const logout = async () => {
    await api.POST("/auth/logout", { params: { header: { "X-CSRF-Token": getCSRFToken() } } });
    setCSRFToken("");
    setPrincipal(null);
    setAuthScreen("login");
  };

  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <header className="app-header">
        <span className="brand-mark" aria-hidden="true">M</span>
        <div className="brand-copy">
          <p className="brand-name">Local Code Maintainer</p>
          <p className="brand-subtitle">Quality-first maintenance control plane</p>
        </div>
        {principal && <div className="session-summary"><span>{principal.user.display_name} · {principal.user.role}</span><button className="quiet-button" type="button" onClick={() => void logout()}><LogOut aria-hidden="true" size={16} /> Sign out</button></div>}
      </header>
      <main id="main-content" className="page" tabIndex={-1}>
        {error && <p className="error" role="alert">{error}</p>}
        {authScreen === "checking" && <p className="notice" role="status">Checking administrator bootstrap and session…</p>}
        {authScreen === "bootstrap" && <AuthenticationForm mode="bootstrap" onAuthenticated={establishSession} />}
        {authScreen === "login" && <AuthenticationForm mode="login" onAuthenticated={establishSession} />}
        {authScreen === "authenticated" && <Dashboard status={status} jobs={jobs} loading={loading} error={error} reload={load} />}
      </main>
    </>
  );
}

function AuthenticationForm({ mode, onAuthenticated }: { mode: "bootstrap" | "login"; onAuthenticated: (result: AuthenticationResult) => void }) {
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const bootstrap = mode === "bootstrap";

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    const response = bootstrap
      ? await api.POST("/auth/bootstrap", { body: { username, display_name: displayName, password } })
      : await api.POST("/auth/login", { body: { username, password } });
    setPassword("");
    setSubmitting(false);
    if (!response.data) {
      setError(bootstrap ? "Bootstrap could not be completed. Check the password policy and try again." : "The username or password was not accepted.");
      return;
    }
    onAuthenticated(response.data);
  };

  return <section className="auth-card" aria-labelledby="auth-heading">
    <p className="eyebrow">{bootstrap ? "First-run step 1 of 10" : "Authenticated access"}</p>
    <h1 id="auth-heading">{bootstrap ? "Create the local administrator" : "Sign in"}</h1>
    <p className="form-help">{bootstrap ? "This one-time account controls repositories, policies, backups, and upgrades. The password is Argon2id-hashed and never returned to the browser." : "Use a local account. Sessions expire after 12 hours."}</p>
    {error && <p className="error" role="alert">{error}</p>}
    <form onSubmit={(event) => void submit(event)}>
      <label htmlFor="username">Username</label>
      <input id="username" name="username" autoComplete="username" required minLength={3} maxLength={64} pattern="[A-Za-z0-9._-]+" value={username} onChange={(event) => setUsername(event.target.value)} />
      {bootstrap && <><label htmlFor="display-name">Display name</label><input id="display-name" name="display-name" autoComplete="name" required maxLength={128} value={displayName} onChange={(event) => setDisplayName(event.target.value)} /></>}
      <label htmlFor="password">Password</label>
      <input id="password" name="password" type="password" autoComplete={bootstrap ? "new-password" : "current-password"} required minLength={14} maxLength={1024} value={password} onChange={(event) => setPassword(event.target.value)} aria-describedby="password-help" />
      <p id="password-help" className="field-help">Use at least 14 characters. Long passphrases are supported.</p>
      <button type="submit" disabled={submitting}>{submitting ? "Working…" : bootstrap ? "Create administrator" : "Sign in"}</button>
    </form>
  </section>;
}

function Dashboard({ status, jobs, loading, error, reload }: { status: SystemStatus | null; jobs: Job[]; loading: boolean; error: string | null; reload: () => Promise<void> }) {
  return <>
    <p className="notice" role="status">The durable maintenance workflow and local draft publication are active. External publication still requires an exact-commit reviewer approval.</p>
    <section className="summary-grid" aria-label="System summary">
      <SummaryCard icon={<Activity aria-hidden="true" />} label="Controller" value={error ? "Unavailable" : status?.status ?? "Checking…"} good={!error && status?.status === "healthy"} />
      <SummaryCard icon={<Box aria-hidden="true" />} label="Deployment" value={status?.profile ?? "—"} />
      <SummaryCard icon={<Database aria-hidden="true" />} label="Jobs" value={loading ? "—" : String(jobs.length)} />
      <SummaryCard icon={<Cpu aria-hidden="true" />} label="Current model" value="Unloaded" />
    </section>
    <section aria-labelledby="jobs-heading">
      <div className="section-heading"><div><p className="eyebrow">Queue and history</p><h1 id="jobs-heading">Maintenance jobs</h1></div><button type="button" onClick={() => void reload()} disabled={loading}><RefreshCw aria-hidden="true" size={17} /> {loading ? "Refreshing" : "Refresh"}</button></div>
      <div className="table-scroll" tabIndex={0} aria-label="Scrollable maintenance jobs table">
        <table><thead><tr><th scope="col">Job</th><th scope="col">Repository</th><th scope="col">State</th><th scope="col">Updated</th></tr></thead><tbody>
          {!loading && jobs.length === 0 && <tr><td className="empty" colSpan={4}>No jobs submitted yet.</td></tr>}
          {loading && <tr><td className="empty" colSpan={4}>Loading jobs…</td></tr>}
          {jobs.map((job) => <tr key={job.id}><td><a href={`/api/v1/jobs/${encodeURIComponent(job.id)}`}>{job.id}</a></td><td>{job.repository}</td><td><span className="status-pill"><span aria-hidden="true">●</span> {statusLabel(job.state)}</span></td><td>{new Date(job.updated_at).toLocaleString()}</td></tr>)}
        </tbody></table>
      </div>
    </section>
  </>;
}

function SummaryCard({ icon, label, value, good = false }: { icon: React.ReactNode; label: string; value: string; good?: boolean }) {
  return <article className="summary-card"><div className="card-label">{icon}<span>{label}</span></div><p className={good ? "card-value good" : "card-value"}>{value}</p></article>;
}
