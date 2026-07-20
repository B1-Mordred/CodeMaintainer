import { useCallback, useEffect, useState } from "react";
import { Activity, Box, Cpu, Database, RefreshCw } from "lucide-react";

interface SystemStatus {
  status: string;
  profile: string;
  components: Record<string, string>;
}

interface Job {
  id: string;
  repository: string;
  state: string;
  updated_at: string;
}

interface JobsResponse {
  items: Job[];
}

function statusLabel(value: string): string {
  return value.replaceAll("_", " ");
}

export function App() {
  const [status, setStatus] = useState<SystemStatus | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [statusResponse, jobsResponse] = await Promise.all([
        fetch("/api/v1/system/status", { headers: { Accept: "application/json" } }),
        fetch("/api/v1/jobs", { headers: { Accept: "application/json" } }),
      ]);
      if (!statusResponse.ok || !jobsResponse.ok) {
        throw new Error("The controller returned an error.");
      }
      const nextStatus = (await statusResponse.json()) as SystemStatus;
      const nextJobs = (await jobsResponse.json()) as JobsResponse;
      setStatus(nextStatus);
      setJobs(nextJobs.items);
      setError(null);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "The controller is unavailable.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <header className="app-header">
        <span className="brand-mark" aria-hidden="true">M</span>
        <div>
          <p className="brand-name">Local Code Maintainer</p>
          <p className="brand-subtitle">Quality-first maintenance control plane</p>
        </div>
      </header>
      <main id="main-content" className="page" tabIndex={-1}>
        <p className="notice" role="status">
          Foundation build: durable workflow and audit services are active. Agent execution and publication remain disabled.
        </p>
        {error && <p className="error" role="alert">{error}</p>}
        <section className="summary-grid" aria-label="System summary">
          <SummaryCard icon={<Activity aria-hidden="true" />} label="Controller" value={error ? "Unavailable" : status?.status ?? "Checking…"} good={!error && status?.status === "healthy"} />
          <SummaryCard icon={<Box aria-hidden="true" />} label="Deployment" value={status?.profile ?? "—"} />
          <SummaryCard icon={<Database aria-hidden="true" />} label="Jobs" value={loading ? "—" : String(jobs.length)} />
          <SummaryCard icon={<Cpu aria-hidden="true" />} label="Current model" value="Unloaded" />
        </section>
        <section aria-labelledby="jobs-heading">
          <div className="section-heading">
            <div><p className="eyebrow">Queue and history</p><h1 id="jobs-heading">Maintenance jobs</h1></div>
            <button type="button" onClick={() => void load()} disabled={loading}>
              <RefreshCw aria-hidden="true" size={17} /> {loading ? "Refreshing" : "Refresh"}
            </button>
          </div>
          <div className="table-scroll" tabIndex={0} aria-label="Scrollable maintenance jobs table">
            <table>
              <thead><tr><th scope="col">Job</th><th scope="col">Repository</th><th scope="col">State</th><th scope="col">Updated</th></tr></thead>
              <tbody>
                {!loading && jobs.length === 0 && <tr><td className="empty" colSpan={4}>No jobs submitted yet.</td></tr>}
                {loading && <tr><td className="empty" colSpan={4}>Loading jobs…</td></tr>}
                {jobs.map((job) => (
                  <tr key={job.id}>
                    <td><a href={`/api/v1/jobs/${encodeURIComponent(job.id)}`}>{job.id}</a></td>
                    <td>{job.repository}</td>
                    <td><span className="status-pill"><span aria-hidden="true">●</span> {statusLabel(job.state)}</span></td>
                    <td>{new Date(job.updated_at).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </main>
    </>
  );
}

function SummaryCard({ icon, label, value, good = false }: { icon: React.ReactNode; label: string; value: string; good?: boolean }) {
  return <article className="summary-card"><div className="card-label">{icon}<span>{label}</span></div><p className={good ? "card-value good" : "card-value"}>{value}</p></article>;
}
