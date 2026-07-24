import { ChangeEvent, FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import {
  CheckCircle2, ChevronDown, CircleAlert, Download, FileCheck2, FileUp,
  History, RefreshCw, RotateCcw, Save, Search, ShieldCheck, SlidersHorizontal,
} from "lucide-react";
import { api, getCSRFToken } from "./api/client";
import type { components } from "./api/schema";

type Descriptor = components["schemas"]["ConfigDescriptor"];
type ScopeKind = components["schemas"]["ConfigScopeKind"];
type ScopeRef = components["schemas"]["ConfigScopeRef"];
type ScopeState = components["schemas"]["ConfigScopeState"];
type EffectiveConfiguration = components["schemas"]["EffectiveConfiguration"];
type Draft = components["schemas"]["ConfigDraft"];
type DraftEntry = components["schemas"]["ConfigDraftEntry"];
type Check = components["schemas"]["ConfigCheckResult"];
type Revision = components["schemas"]["ConfigRegistryRevision"];
type Validation = components["schemas"]["ConfigValidationReport"];
type ImportDocument = components["schemas"]["DeclarativeConfig"];
type ImportPreview = components["schemas"]["ConfigImportPreview"];
type ConfigDependency = components["schemas"]["ConfigDependency"];

type PendingChange = { mode: "set"; value: unknown } | { mode: "reset" };
type RelationIssue = { code: "dependency" | "incompatibility"; message: string };
type ConfigConsumer = { page: string; label: string; detail: string };

const scopeKinds: Array<{ value: Exclude<ScopeKind, "built_in">; label: string; hint: string }> = [
  { value: "system", label: "System", hint: "Applies appliance-wide" },
  { value: "capability_pack", label: "Capability pack", hint: "Pack assignment identifier" },
  { value: "project", label: "Project", hint: "Registered project identifier" },
  { value: "environment", label: "Environment / runner", hint: "Named execution environment" },
  { value: "job_template", label: "Job template", hint: "Reusable job template" },
  { value: "job_override", label: "Job override", hint: "One accepted job only" },
];

const pageLabels: Record<string, string> = {
  "setup-health": "Setup and health",
  repositories: "Repositories",
  "capability-packs": "Capability packs",
  jobs: "Jobs",
  quality: "Quality",
  documentation: "Documentation",
  "code-intelligence": "Code intelligence",
  forges: "Forges",
  "runners-windows": "Runners and Windows",
  "models-agents": "Models and agents",
  "scheduling-resources": "Scheduling and resources",
  "policy-risk": "Policy and risk",
  "security-sbom": "Security and SBOM",
  evaluation: "Evaluation",
  "memory-evidence": "Memory and evidence",
  observability: "Observability",
};

const consumerDefaults: Record<string, ConfigConsumer[]> = {
  deployment: [
    { page: "setup-health", label: "Setup and health", detail: "Bootstrap, controller health, backup, restore, and update status." },
    { page: "observability", label: "Observability", detail: "Deployment profile and data-root diagnostics appear in support bundles." },
  ],
  workflow: [
    { page: "jobs", label: "Jobs", detail: "Accepted jobs consume review-cycle, wall-time, and log-retention limits." },
    { page: "scheduling-resources", label: "Scheduling and resources", detail: "Recurring work inherits workflow budgets when creating jobs." },
  ],
  qc: [
    { page: "quality", label: "Quality", detail: "Findings, waivers, and publication blockers consume QC policy." },
    { page: "policy-risk", label: "Policy and risk", detail: "Risk routing and waiver policy use the same deterministic settings." },
  ],
  protected_paths: [
    { page: "policy-risk", label: "Policy and risk", detail: "Protected-path matches affect risk and required approval routing." },
    { page: "repositories", label: "Repositories", detail: "Repository diagnostics display protected-path scope effects." },
  ],
  notifications: [
    { page: "scheduling-resources", label: "Scheduling and resources", detail: "The local inbox receives schedule and workflow attention events." },
    { page: "setup-health", label: "Setup and health", detail: "Operator lifecycle checks include durable notification readiness." },
  ],
  intelligence: [
    { page: "code-intelligence", label: "Code intelligence", detail: "Indexing, cache retention, quota, and context budgets are consumed here." },
    { page: "memory-evidence", label: "Memory and evidence", detail: "Evidence and memory views rely on project-scoped context controls." },
  ],
  verification: [
    { page: "quality", label: "Quality", detail: "Final verification gates and fresh-cache requirements are enforced here." },
    { page: "security-sbom", label: "Security and SBOM", detail: "Release evidence and SBOM gates depend on final verification policy." },
  ],
};

const consumerOverrides: Record<string, ConfigConsumer[]> = {
  "workflow.max_review_cycles": [
    { page: "jobs", label: "Jobs", detail: "Controls implementation/QC repair loop limits for each accepted job." },
    { page: "quality", label: "Quality", detail: "Determines how many finding repair cycles can run before escalation." },
  ],
  "workflow.max_wall_seconds": [
    { page: "jobs", label: "Jobs", detail: "Bounds one maintenance job's accepted wall-time budget." },
    { page: "runners-windows", label: "Runners and Windows", detail: "Limits remote and simulated worker lease duration." },
    { page: "scheduling-resources", label: "Scheduling and resources", detail: "Constrains recurring-work execution windows." },
  ],
  "workflow.max_log_bytes": [
    { page: "jobs", label: "Jobs", detail: "Controls retained job log evidence size." },
    { page: "observability", label: "Observability", detail: "Determines bounded diagnostic payload size before support export." },
  ],
  "intelligence.context_input_tokens": [
    { page: "code-intelligence", label: "Code intelligence", detail: "Caps selectable repository context for agent stages." },
    { page: "models-agents", label: "Models and agents", detail: "Feeds model route and context-window suitability checks." },
    { page: "evaluation", label: "Evaluation", detail: "Provides reproducible historical profile comparison limits." },
  ],
  "intelligence.context_output_reserve_tokens": [
    { page: "code-intelligence", label: "Code intelligence", detail: "Reserves output capacity outside selectable source context." },
    { page: "models-agents", label: "Models and agents", detail: "Prevents model routes from overcommitting context windows." },
  ],
  "verification.clean_final_cache_required": [
    { page: "quality", label: "Quality", detail: "Requires publishable final verification to run against fresh caches." },
    { page: "evaluation", label: "Evaluation", detail: "Keeps historical comparison cache effects explicit and isolated." },
    { page: "security-sbom", label: "Security and SBOM", detail: "Prevents stale verification artifacts from backing release evidence." },
  ],
};

function configRouteState(): { search: string; from: string } {
  const raw = window.location.hash.slice(1);
  const [page, query = ""] = raw.split("?", 2);
  if (page !== "configuration") return { search: "", from: "" };
  const params = new URLSearchParams(query);
  return { search: params.get("search") ?? "", from: params.get("from") ?? "" };
}

function consumersFor(descriptor: Descriptor): ConfigConsumer[] {
  return consumerOverrides[descriptor.key] ?? consumerDefaults[descriptor.namespace] ?? [
    { page: "configuration", label: "Configuration", detail: "Registry-owned setting with no specific consumer mapping yet." },
  ];
}

function scopeLabel(scope?: ScopeRef): string {
  if (!scope) return "Built-in default";
  const name = scope.kind.replaceAll("_", " ").replace(/\b\w/g, (character) => character.toUpperCase());
  return scope.id ? `${name} · ${scope.id}` : name;
}

function displayValue(value: unknown, secret = false): string {
  if (secret) return "Configured (write-only)";
  if (value === undefined || value === null) return "Not set";
  if (Array.isArray(value)) return value.length ? value.join(", ") : "Empty list";
  if (typeof value === "boolean") return value ? "Enabled" : "Disabled";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function scopeETag(version: number): string { return `"config-scope-${version}"`; }
function draftETag(version: number): string { return `"config-draft-${version}"`; }
function requestError(error: unknown, fallback: string): string {
  if (error && typeof error === "object" && "error" in error) {
    const nested = (error as { error?: { message?: string } }).error;
    if (nested?.message) return nested.message;
  }
  return fallback;
}

function valuesEqual(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function relationMatches(relation: ConfigDependency, current: unknown): boolean {
  const equal = valuesEqual(current, relation.value);
  if (relation.operator === "equals") return equal;
  if (relation.operator === "not_equals") return !equal;
  const contains = Array.isArray(current) && current.some((item) => valuesEqual(item, relation.value));
  if (relation.operator === "contains") return contains;
  if (relation.operator === "not_contains") return !contains;
  return false;
}

function relationActive(relation: ConfigDependency, ownerValue: unknown): boolean {
  return relation.when_value === undefined || valuesEqual(ownerValue, relation.when_value);
}

function inheritedValue(effective: components["schemas"]["ConfigEffectiveValue"] | undefined, scope: ScopeRef, fallback: unknown): unknown {
  if (!effective) return fallback;
  const contributions = effective.contributions.filter((item) => item.configured && (item.scope.kind !== scope.kind || item.scope.id !== scope.id));
  return contributions.at(-1)?.value ?? fallback;
}

function ApplyBadge({ mode }: { mode: Descriptor["apply"] }) {
  const labels: Record<Descriptor["apply"], string> = {
    live: "Applies live",
    new_jobs: "New jobs",
    service_reload: "Service reload",
    operator_restart: "Operator restart",
  };
  return <span className={`apply-badge apply-${mode}`}>{labels[mode]}</span>;
}

function ValidationSummary({ report }: { report: Validation }) {
  return <div className={report.valid ? "validation-summary valid" : "validation-summary invalid"} role="status">
    {report.valid ? <CheckCircle2 aria-hidden="true" /> : <CircleAlert aria-hidden="true" />}
    <div>
      <strong>{report.valid ? "Draft is valid" : "Draft needs attention"}</strong>
      <p>{report.issues.length === 0 ? "All typed values and dependencies passed." : `${report.issues.length} validation message(s).`}</p>
      {report.issues.length > 0 && <ul>{report.issues.map((issue, index) => <li key={`${issue.key}-${issue.code}-${index}`}><strong>{issue.key ?? "Draft"}:</strong> {issue.message}</li>)}</ul>}
    </div>
  </div>;
}

function SettingField({ descriptor, state, effective, pending, relationIssues, disabled, onChange, onReset, onDefault }: {
  descriptor: Descriptor;
  state: ScopeState;
  effective?: components["schemas"]["ConfigEffectiveValue"];
  pending?: PendingChange;
  relationIssues: RelationIssue[];
  disabled: boolean;
  onChange: (value: unknown) => void;
  onReset: () => void;
  onDefault: () => void;
}) {
  const consumers = consumersFor(descriptor);
  const stored = state.values.find((item) => item.key === descriptor.key);
  const baseValue = stored?.configured ? stored.value : effective?.value ?? descriptor.default;
  const value = pending?.mode === "set" ? pending.value : pending?.mode === "reset" ? inheritedValue(effective, state.scope, descriptor.default) : baseValue;
  const fieldID = `config-${descriptor.key.replaceAll(".", "-")}`;
  const permitted = descriptor.permitted_scopes.includes(state.scope.kind);
  const fieldDisabled = disabled || descriptor.bootstrap_controlled || !permitted;
  const helpID = `${fieldID}-help`;
  const relationID = `${fieldID}-relations`;
  const describedBy = relationIssues.length > 0 ? `${helpID} ${relationID}` : helpID;
  let control;
  if (descriptor.ui.widget === "checkbox") {
    control = <label className="config-checkbox" htmlFor={fieldID}>
      <input id={fieldID} type="checkbox" checked={Boolean(value)} disabled={fieldDisabled} aria-label={descriptor.ui.label} aria-describedby={describedBy} onChange={(event) => onChange(event.target.checked)} />
      <span>{Boolean(value) ? "Enabled" : "Disabled"}</span>
    </label>;
  } else if (descriptor.ui.widget === "number") {
    const schema = descriptor.json_schema as { minimum?: number; maximum?: number };
    control = <input id={fieldID} type="number" value={typeof value === "number" ? value : ""} min={schema.minimum} max={schema.maximum} disabled={fieldDisabled} aria-describedby={describedBy} onChange={(event) => onChange(Number(event.target.value))} />;
  } else if (descriptor.ui.widget === "string_list" && Array.isArray((descriptor.json_schema as { items?: { enum?: unknown[] } }).items?.enum)) {
    const options = (descriptor.json_schema as { items: { enum: unknown[] } }).items.enum.map(String);
    const selected = Array.isArray(value) ? value.map(String) : [];
    control = <fieldset className="config-multiselect" id={fieldID} aria-describedby={describedBy} disabled={fieldDisabled}><legend>Configured values</legend>{options.map((option) => <label key={option}><input type="checkbox" checked={selected.includes(option)} onChange={(event) => onChange(event.target.checked ? [...selected, option] : selected.filter((item) => item !== option))} /><span>{option.replaceAll("_", " ")}</span></label>)}</fieldset>;
  } else if (descriptor.ui.widget === "string_list" || descriptor.ui.widget === "path_pattern_list") {
    control = <textarea id={fieldID} rows={4} value={Array.isArray(value) ? value.join("\n") : ""} disabled={fieldDisabled} aria-describedby={describedBy} placeholder="One value per line" onChange={(event) => onChange(event.target.value.split("\n").map((item) => item.trim()).filter(Boolean))} />;
  } else {
    control = <input id={fieldID} type={descriptor.secret ? "password" : "text"} value={typeof value === "string" ? value : ""} disabled={fieldDisabled} aria-describedby={describedBy} autoComplete={descriptor.secret ? "new-password" : undefined} onChange={(event) => onChange(event.target.value)} />;
  }
  return <article className={pending ? "setting-card changed" : "setting-card"}>
    <header>
      <div><p className="setting-key">{descriptor.key}</p><h3>{descriptor.ui.label}</h3></div>
      <ApplyBadge mode={descriptor.apply} />
    </header>
    <p className="setting-help" id={helpID}>{descriptor.ui.help}</p>
    <div className="setting-consumers" aria-label={`${descriptor.key} consumer links`}>
      <span>Consumed by</span>
      <ul>{consumers.map((consumer) => <li key={`${descriptor.key}-${consumer.page}`}><a href={`#${consumer.page}`}>{consumer.label}</a><small>{consumer.detail}</small></li>)}</ul>
    </div>
    {relationIssues.length > 0 && <div className="relation-warning" id={relationID} role="status"><CircleAlert aria-hidden="true" /><ul>{relationIssues.map((issue, index) => <li key={`${issue.code}-${index}`}>{issue.message}</li>)}</ul></div>}
    <div className="setting-control">
      {descriptor.ui.widget !== "checkbox" && !(descriptor.ui.widget === "string_list" && Array.isArray((descriptor.json_schema as { items?: { enum?: unknown[] } }).items?.enum)) && <label htmlFor={fieldID}>Configured value</label>}
      {control}
    </div>
    <div className="setting-provenance">
      <div><span>Effective</span><strong>{displayValue(effective?.value, descriptor.secret)}</strong></div>
      <div><span>Source</span><strong>{scopeLabel(effective?.source_scope)}</strong></div>
      <div><span>This scope</span><strong>{stored?.configured ? "Override" : "Inherited"}</strong></div>
    </div>
    <footer>
      {descriptor.bootstrap_controlled ? <span className="field-lock"><ShieldCheck aria-hidden="true" /> Bootstrap controlled</span> : !permitted ? <span className="field-lock">Unavailable at this scope</span> : <div className="setting-reset-actions"><button className="tertiary-button" type="button" disabled={disabled || (!stored?.configured && !pending)} onClick={onReset}><RotateCcw aria-hidden="true" />Inherited</button><button className="tertiary-button" type="button" disabled={disabled || valuesEqual(value, descriptor.default)} onClick={onDefault}>Safe default</button></div>}
      {pending && <span className="pending-label">{pending.mode === "reset" ? "Will inherit" : "Draft change"}</span>}
    </footer>
  </article>;
}

export function ConfigurationPage({ expert }: { expert: boolean }) {
  const initialRoute = configRouteState();
  const [scopeKind, setScopeKind] = useState<Exclude<ScopeKind, "built_in">>("system");
  const [scopeID, setScopeID] = useState("");
  const [descriptors, setDescriptors] = useState<Descriptor[]>([]);
  const [state, setState] = useState<ScopeState | null>(null);
  const [effective, setEffective] = useState<EffectiveConfiguration | null>(null);
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const [activeDraft, setActiveDraft] = useState<Draft | null>(null);
  const [checks, setChecks] = useState<Check[]>([]);
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [prerequisites, setPrerequisites] = useState<Array<{ key: string; prerequisite: string; status: "passed" | "failed" | "unavailable"; message: string }>>([]);
  const [pending, setPending] = useState<Record<string, PendingChange>>({});
  const [search, setSearch] = useState(initialRoute.search);
  const [routeFrom, setRouteFrom] = useState(initialRoute.from);
  const [reason, setReason] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [validation, setValidation] = useState<Validation | null>(null);
  const [importDocument, setImportDocument] = useState<ImportDocument | null>(null);
  const [importMode, setImportMode] = useState<"strict" | "forward_compatible">("strict");
  const [importPreview, setImportPreview] = useState<ImportPreview | null>(null);
  const [password, setPassword] = useState("");

  useEffect(() => {
    const updateRouteFilter = () => {
      const next = configRouteState();
      if (next.search) setSearch(next.search);
      setRouteFrom(next.from);
    };
    window.addEventListener("hashchange", updateRouteFilter);
    window.addEventListener("popstate", updateRouteFilter);
    return () => {
      window.removeEventListener("hashchange", updateRouteFilter);
      window.removeEventListener("popstate", updateRouteFilter);
    };
  }, []);

  const scope = useMemo<ScopeRef>(() => scopeKind === "system" ? { kind: "system" } : { kind: scopeKind, id: scopeID.trim() }, [scopeID, scopeKind]);
  const scopeReady = scope.kind === "system" || Boolean(scope.id);
  const query = { scope_kind: scope.kind, ...(scope.id ? { scope_id: scope.id } : {}) };

  const load = useCallback(async () => {
    if (!scopeReady) { setState(null); setEffective(null); setDrafts([]); setRevisions([]); return; }
    setLoading(true); setError("");
    const scopeQuery = { scope_kind: scope.kind, ...(scope.id ? { scope_id: scope.id } : {}) };
    const [descriptorResult, stateResult, effectiveResult, draftResult, revisionResult, prerequisiteResult] = await Promise.all([
      api.GET("/config/descriptors", { params: { query: { advanced: true } } }),
      api.GET("/config/values", { params: { query: scopeQuery } }),
      api.POST("/config/effective", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { scopes: scope.kind === "system" ? [scope] : [{ kind: "system" }, scope] } }),
      api.GET("/config/drafts", { params: { query: scopeQuery } }),
      api.GET("/config/registry-revisions", { params: { query: scopeQuery } }),
      api.GET("/config/prerequisites"),
    ]);
    if (!stateResult.data || !effectiveResult.data) {
      setError(requestError(stateResult.error ?? effectiveResult.error, "Configuration state could not be loaded."));
    }
    setDescriptors(descriptorResult.data?.items ?? []);
    setState(stateResult.data ?? null);
    setEffective(effectiveResult.data ?? null);
    setDrafts(draftResult.data?.items ?? []);
    setRevisions(revisionResult.data?.items ?? []);
    setPrerequisites(prerequisiteResult.data?.items ?? []);
    setLoading(false);
  }, [scope.kind, scope.id, scopeReady]);

  useEffect(() => { setPending({}); setActiveDraft(null); setChecks([]); setValidation(null); setImportDocument(null); setImportPreview(null); void load(); }, [load]);

  const visibleDescriptors = useMemo(() => descriptors.filter((descriptor) => {
    if (!expert && descriptor.ui.advanced) return false;
    const haystack = `${descriptor.key} ${descriptor.namespace} ${descriptor.ui.label} ${descriptor.ui.help} ${descriptor.ui.group}`.toLowerCase();
    const tokens = search.trim().toLowerCase().split(/\s+/).filter(Boolean);
    return tokens.length === 0 || tokens.some((token) => haystack.includes(token));
  }), [descriptors, expert, search]);
  const groupedDescriptors = useMemo(() => {
    const groups = new Map<string, Descriptor[]>();
    visibleDescriptors.forEach((descriptor) => groups.set(descriptor.ui.group, [...(groups.get(descriptor.ui.group) ?? []), descriptor]));
    return [...groups.entries()].sort(([left], [right]) => left.localeCompare(right));
  }, [visibleDescriptors]);

  const prospectiveValues = useMemo(() => {
    const values = new Map<string, unknown>();
    descriptors.forEach((descriptor) => {
      const change = pending[descriptor.key];
      if (change?.mode === "set") values.set(descriptor.key, change.value);
      else if (change?.mode === "reset") values.set(descriptor.key, inheritedValue(effective?.values[descriptor.key], scope, descriptor.default));
      else values.set(descriptor.key, effective?.values[descriptor.key]?.value ?? descriptor.default);
    });
    return values;
  }, [descriptors, effective, pending, scope]);
  const relationIssues = useMemo(() => {
    const issues = new Map<string, RelationIssue[]>();
    descriptors.forEach((descriptor) => {
      const ownerValue = prospectiveValues.get(descriptor.key);
      const current: RelationIssue[] = [];
      descriptor.dependencies?.forEach((relation) => {
        if (relationActive(relation, ownerValue) && !relationMatches(relation, prospectiveValues.get(relation.key))) current.push({ code: "dependency", message: relation.message });
      });
      descriptor.incompatibilities?.forEach((relation) => {
        if (relationActive(relation, ownerValue) && relationMatches(relation, prospectiveValues.get(relation.key))) current.push({ code: "incompatibility", message: relation.message });
      });
      if (current.length > 0) issues.set(descriptor.key, current);
    });
    return issues;
  }, [descriptors, prospectiveValues]);

  const selectDraft = async (draft: Draft) => {
    setActiveDraft(draft); setValidation(null); setMessage(""); setError("");
    const result = await api.GET("/config/drafts/{draftID}/checks", { params: { path: { draftID: draft.id } } });
    setChecks(result.data?.items ?? []);
  };
  const editDraft = (draft: Draft) => {
    const changes: Record<string, PendingChange> = {};
    draft.entries.forEach((entry) => { changes[entry.key] = entry.reset ? { mode: "reset" } : { mode: "set", value: entry.value }; });
    setPending(changes); setReason(draft.reason ?? ""); void selectDraft(draft);
  };
  const startNewDraft = () => { setActiveDraft(null); setChecks([]); setValidation(null); setPending({}); setReason(""); setMessage("Started a new local draft."); };

  const draftEntries = (): DraftEntry[] => Object.entries(pending).sort(([left], [right]) => left.localeCompare(right)).map(([key, change]) => {
    const descriptor = descriptors.find((item) => item.key === key)!;
    return change.mode === "reset"
      ? { key, reset: true, secret: descriptor.secret, configured: false }
      : { key, value: change.value, reset: false, secret: descriptor.secret, configured: true };
  });

  const saveDraft = async (event: FormEvent) => {
    event.preventDefault(); setError(""); setMessage("");
    if (!state || !reason.trim() || Object.keys(pending).length === 0) { setError("Add at least one change and an audited reason before saving a draft."); return; }
    const entries = draftEntries();
    if (activeDraft?.state === "draft") {
      const result = await api.PUT("/config/drafts/{draftID}", { params: { path: { draftID: activeDraft.id }, header: { "X-CSRF-Token": getCSRFToken(), "If-Match": draftETag(activeDraft.version) } }, body: { reason, entries } });
      if (!result.data) { setError(requestError(result.error, "The draft could not be updated; refresh after a version conflict.")); return; }
      setActiveDraft(result.data.draft); setValidation(result.data.validation); setMessage(`Updated ${result.data.draft.id}.`);
    } else {
      const result = await api.POST("/config/drafts", { params: { header: { "X-CSRF-Token": getCSRFToken(), "If-Match": scopeETag(state.version) } }, body: { scope, reason, entries } });
      if (!result.data) { setError(requestError(result.error, "The draft could not be created; refresh after a version conflict.")); return; }
      setActiveDraft(result.data.draft); setValidation(result.data.validation); setMessage(`Created ${result.data.draft.id}.`);
    }
    await load();
  };

  const runCheck = async (kind: "validate" | "dry-run") => {
    if (!activeDraft) return;
    setError("");
    if (kind === "validate") {
      const result = await api.POST("/config/drafts/{draftID}/actions/validate", { params: { path: { draftID: activeDraft.id }, header: { "X-CSRF-Token": getCSRFToken() } } });
      if (!result.data) { setError(requestError(result.error, "Validation failed.")); return; }
      setValidation(result.data.validation); setChecks((items) => [result.data!.check, ...items]); setMessage("Recorded a version-bound validation check.");
    } else {
      const result = await api.POST("/config/drafts/{draftID}/actions/dry-run", { params: { path: { draftID: activeDraft.id }, header: { "X-CSRF-Token": getCSRFToken() } } });
      if (!result.data) { setError(requestError(result.error, "Dry run failed.")); return; }
      setChecks((items) => [...result.data!.items, ...items]); setMessage("Recorded dry-run and prerequisite results.");
    }
  };

  const transitionDraft = async (action: "review" | "apply" | "discard") => {
    if (!activeDraft || !reason.trim()) { setError("An audited reason is required for this action."); return; }
    setError("");
    const params = { path: { draftID: activeDraft.id }, header: { "X-CSRF-Token": getCSRFToken(), "If-Match": draftETag(activeDraft.version) } };
    if (action === "review") {
      const result = await api.POST("/config/drafts/{draftID}/actions/review", { params, body: { reason } });
      if (!result.data) { setError(requestError(result.error, "Review failed; rerun validation or refresh the draft.")); return; }
      setActiveDraft(result.data.draft); setValidation(result.data.validation); setMessage("Draft content is now review-bound.");
    } else if (action === "apply") {
      const result = await api.POST("/config/drafts/{draftID}/actions/apply", { params, body: { reason } });
      if (!result.data) { setError(requestError(result.error, "Apply failed; reauthenticate if the draft contains secrets, or refresh after a conflict.")); return; }
      setMessage(`Applied revision ${result.data.revision.id}.`); setActiveDraft(null); setPending({}); setValidation(result.data.validation);
    } else {
      const result = await api.POST("/config/drafts/{draftID}/actions/discard", { params, body: { reason } });
      if (!result.data) { setError(requestError(result.error, "Discard failed after a version conflict.")); return; }
      setMessage(`Discarded ${result.data.id}.`); setActiveDraft(result.data); setPending({});
    }
    await load();
  };

  const exportScope = async () => {
    if (!scopeReady) return;
    const result = await api.GET("/config/export", { params: { query } });
    if (!result.data) { setError(requestError(result.error, "Export failed.")); return; }
    const payload = JSON.stringify(result.data, null, 2);
    const href = URL.createObjectURL(new Blob([payload], { type: "application/json" }));
    const anchor = document.createElement("a"); anchor.href = href; anchor.download = `configuration-${scope.kind}-${scope.id ?? "default"}.json`; anchor.click(); URL.revokeObjectURL(href);
    setMessage(`Exported redacted scope version ${result.data.scope_version}; document hash ${result.data.document_hash.slice(0, 12)}…`);
  };
  const readImport = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]; if (!file) return;
    try { setImportDocument(JSON.parse(await file.text()) as ImportDocument); setImportPreview(null); setMessage(`Loaded ${file.name} for local preview.`); setError(""); }
    catch { setImportDocument(null); setError("The selected import is not valid JSON."); }
  };
  const previewImport = async (create: boolean) => {
    if (!state || !importDocument) { setError("Select a declarative configuration export first."); return; }
    const headers = { "X-CSRF-Token": getCSRFToken(), "If-Match": scopeETag(state.version) };
    const body = { mode: importMode, target: scope, ...(create ? { reason } : {}), document: importDocument };
    if (create) {
      if (!reason.trim()) { setError("An audited reason is required to create an import draft."); return; }
      const result = await api.POST("/config/import", { params: { header: headers }, body });
      if (!result.data) { setError(requestError(result.error, "The import could not create a draft.")); return; }
      setImportPreview(result.data.preview); setActiveDraft(result.data.draft); setMessage(`Created import candidate ${result.data.draft.id}; it still requires review and apply.`); await load();
    } else {
      const result = await api.POST("/config/import/preview", { params: { header: headers }, body });
      if (!result.data) { setError(requestError(result.error, "Import preview failed.")); return; }
      setImportPreview(result.data); setMessage(result.data.valid ? "Import is valid for a draft." : "Import preview found blocking issues.");
    }
  };

  const reauthenticate = async (): Promise<boolean> => {
    if (!password) { setError("Enter the current administrator password before rollback."); return false; }
    const result = await api.POST("/auth/reauthenticate", { params: { header: { "X-CSRF-Token": getCSRFToken() } }, body: { password } });
    setPassword("");
    if (!result.data) { setError("Reauthentication failed."); return false; }
    return true;
  };
  const rollback = async (revision: Revision) => {
    if (!state || !reason.trim() || !await reauthenticate()) return;
    if (!window.confirm(`Create a new rollback revision from ${revision.id}? Current history remains immutable.`)) return;
    const result = await api.POST("/config/registry-revisions/{revisionID}/actions/rollback", { params: { path: { revisionID: revision.id }, header: { "X-CSRF-Token": getCSRFToken(), "If-Match": scopeETag(state.version) } }, body: { reason } });
    if (!result.data) { setError(requestError(result.error, "Rollback failed after a version conflict.")); return; }
    setMessage(`Created rollback revision ${result.data.revision.id}.`); setPending({}); await load();
  };

  const changedKeys = Object.keys(pending).sort();
  const routeFromLabel = routeFrom ? pageLabels[routeFrom] : "";
  return <>
    <p className="page-intro">Edit trusted typed settings by scope, inspect their provenance, and move every change through a version-bound draft before it can become effective.</p>
    {routeFromLabel && <p className="config-route-hint" role="status">Filtered from {routeFromLabel}. <a href={`#${routeFrom}`}>Return to {routeFromLabel}</a>.</p>}
    <section className="configuration-toolbar" aria-label="Configuration scope and search">
      <label>Scope<select value={scopeKind} onChange={(event) => setScopeKind(event.target.value as typeof scopeKind)}>{scopeKinds.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select><span>{scopeKinds.find((item) => item.value === scopeKind)?.hint}</span></label>
      <label>Scope identifier<input value={scopeID} disabled={scopeKind === "system"} required={scopeKind !== "system"} placeholder="Exact registered identifier" onChange={(event) => setScopeID(event.target.value)} /></label>
      <label>Search registry<div className="input-with-icon"><Search aria-hidden="true" /><input type="search" value={search} placeholder="Key, label, group…" onChange={(event) => setSearch(event.target.value)} /></div></label>
      <button className="secondary-button" type="button" disabled={loading || !scopeReady} onClick={() => void load()}><RefreshCw aria-hidden="true" />Refresh</button>
    </section>
    <div className="configuration-summary">
      <div><span>Scope version</span><strong>{state?.version ?? "—"}</strong></div>
      <div><span>Visible settings</span><strong>{visibleDescriptors.length}</strong></div>
      <div><span>Draft changes</span><strong>{changedKeys.length}</strong></div>
      <div><span>Open drafts</span><strong>{drafts.filter((draft) => draft.state === "draft" || draft.state === "reviewed").length}</strong></div>
    </div>
    {error && <p className="error" role="alert">{error}</p>}
    {message && <p className="inline-message" role="status">{message}</p>}
    {!scopeReady && <div className="empty-state"><SlidersHorizontal aria-hidden="true" /><div><strong>Choose an exact scope</strong><p>Non-system values require a registered pack, project, environment, template, or job identifier.</p></div></div>}
    {scopeReady && <div className="configuration-layout">
      <div className="configuration-main">
        {loading ? <p className="loading-line">Loading typed descriptors and effective values…</p> : groupedDescriptors.length === 0 ? <div className="empty-state"><Search aria-hidden="true" /><div><strong>No settings match</strong><p>Clear the search or enable Expert mode to include advanced fields.</p></div></div> : groupedDescriptors.map(([group, items]) => <section className="setting-group" key={group}>
          <div className="section-heading"><div><p className="eyebrow">Registry group</p><h2>{group}</h2></div><span>{items.length} setting{items.length === 1 ? "" : "s"}</span></div>
          <div className="setting-grid">{items.map((descriptor) => <SettingField key={descriptor.key} descriptor={descriptor} state={state!} effective={effective?.values[descriptor.key]} pending={pending[descriptor.key]} relationIssues={relationIssues.get(descriptor.key) ?? []} disabled={Boolean(activeDraft && activeDraft.state !== "draft")} onChange={(value) => setPending((current) => ({ ...current, [descriptor.key]: { mode: "set", value } }))} onDefault={() => setPending((current) => ({ ...current, [descriptor.key]: { mode: "set", value: descriptor.default } }))} onReset={() => setPending((current) => {
            if (state!.values.some((value) => value.key === descriptor.key && value.configured)) return { ...current, [descriptor.key]: { mode: "reset" } };
            const next = { ...current }; delete next[descriptor.key]; return next;
          })} />)}</div>
        </section>)}
      </div>
      <aside className="configuration-rail" aria-label="Draft lifecycle">
        <section className="rail-card draft-composer">
          <div className="rail-heading"><div><p className="eyebrow">Version-bound change</p><h2>{activeDraft?.state === "draft" ? "Edit draft" : "New draft"}</h2></div><button className="tertiary-button" type="button" onClick={startNewDraft}>New</button></div>
          {activeDraft && <div className="selected-draft"><code>{activeDraft.id}</code><span className={`status-pill ${activeDraft.state === "applied" ? "good" : "neutral"}`}><span aria-hidden="true">●</span>{activeDraft.state}</span></div>}
          <form onSubmit={(event) => void saveDraft(event)}>
            <label>Audited reason<textarea required rows={3} maxLength={1000} value={reason} onChange={(event) => setReason(event.target.value)} placeholder="Why this exact change is needed" /></label>
            <div className="diff-preview"><h3>Before / after and impact</h3>{changedKeys.length === 0 ? <p>No local changes yet.</p> : <ul>{changedKeys.map((key) => { const descriptor = descriptors.find((item) => item.key === key); const change = pending[key]; const after = change.mode === "reset" ? inheritedValue(effective?.values[key], scope, descriptor?.default) : change.value; return <li key={key}><code>{key}</code><span><del>{displayValue(effective?.values[key]?.value, descriptor?.secret)}</del> → <ins>{displayValue(after, descriptor?.secret)}</ins></span>{descriptor && <ApplyBadge mode={descriptor.apply} />}</li>; })}</ul>}</div>
            <button type="submit" disabled={changedKeys.length === 0 || Boolean(activeDraft && activeDraft.state !== "draft")}><Save aria-hidden="true" />{activeDraft?.state === "draft" ? "Save draft" : "Create draft"}</button>
          </form>
          {validation && <ValidationSummary report={validation} />}
          {activeDraft && <div className="lifecycle-actions">
            <button className="secondary-button" type="button" onClick={() => void runCheck("validate")}><FileCheck2 aria-hidden="true" />Validate</button>
            <button className="secondary-button" type="button" onClick={() => void runCheck("dry-run")}><SlidersHorizontal aria-hidden="true" />Dry run</button>
            {activeDraft.state === "draft" && <button type="button" onClick={() => void transitionDraft("review")}>Review exact draft</button>}
            {activeDraft.state === "reviewed" && <button type="button" onClick={() => void transitionDraft("apply")}>Apply reviewed draft</button>}
            {(activeDraft.state === "draft" || activeDraft.state === "reviewed") && <button className="danger-button" type="button" onClick={() => void transitionDraft("discard")}>Discard</button>}
          </div>}
        </section>
        <section className="rail-card"><div className="rail-heading"><div><p className="eyebrow">Saved work</p><h2>Drafts</h2></div><span>{drafts.length}</span></div>{drafts.length === 0 ? <p className="rail-empty">No drafts at this scope.</p> : <div className="draft-list">{drafts.map((draft) => <button type="button" className={activeDraft?.id === draft.id ? "draft-row active" : "draft-row"} key={draft.id} onClick={() => editDraft(draft)}><div><code>{draft.id}</code><span>{draft.operation} · v{draft.version}</span></div><span>{draft.state}</span></button>)}</div>}</section>
        <section className="rail-card"><div className="rail-heading"><div><p className="eyebrow">Recorded evidence</p><h2>Checks</h2></div><span>{checks.length}</span></div>{checks.length === 0 ? <p className="rail-empty">Select a draft and run validation or a dry run.</p> : <div className="check-list">{checks.map((check) => <article key={check.id}><strong>{check.kind.replaceAll("_", " ")}</strong><span className={`check-status ${check.status}`}>{check.status}</span><p>{check.handler} · draft v{check.draft_version}</p></article>)}</div>}</section>
      </aside>
    </div>}
    {scopeReady && <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Portable, redacted state</p><h2>Export and import</h2></div><button className="secondary-button" type="button" onClick={() => void exportScope()}><Download aria-hidden="true" />Export scope</button></div>
      <div className="import-panel"><label>Declarative JSON export<input type="file" accept="application/json,.json" onChange={(event) => void readImport(event)} /><span>Known secrets remain write-only. The document hash and source provenance are checked before import.</span></label><label>Compatibility mode<select value={importMode} onChange={(event) => setImportMode(event.target.value as typeof importMode)}><option value="strict">Strict — reject unknown keys</option><option value="forward_compatible">Forward compatible — preserve, never apply unknown keys</option></select></label><div className="action-strip"><button className="secondary-button" type="button" disabled={!importDocument} onClick={() => void previewImport(false)}><FileCheck2 aria-hidden="true" />Preview import</button><button type="button" disabled={!importDocument || !importPreview?.valid} onClick={() => void previewImport(true)}><FileUp aria-hidden="true" />Create import draft</button></div></div>
      {importPreview && <div className={importPreview.valid ? "import-result valid" : "import-result invalid"}><strong>{importPreview.valid ? "Import can become a draft" : "Import is blocked"}</strong><p>{importPreview.entries.length} known value(s); {importPreview.preserved_unknown.length} unknown value(s) preserved and never applied.</p>{importPreview.issues.length > 0 && <ul>{importPreview.issues.map((issue, index) => <li key={`${issue.code}-${index}`}>{issue.key ? `${issue.key}: ` : ""}{issue.message}</li>)}</ul>}</div>}
    </section>}
    {scopeReady && <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Dependency visibility</p><h2>Prerequisites</h2></div><span>{prerequisites.length} registered</span></div>{prerequisites.length === 0 ? <div className="empty-state"><ShieldCheck aria-hidden="true" /><div><strong>No external prerequisites</strong><p>Current descriptors validate entirely inside the trusted controller.</p></div></div> : <div className="check-list prerequisite-list">{prerequisites.map((item) => <article key={`${item.key}-${item.prerequisite}`}><strong>{item.prerequisite}</strong><span className={`check-status ${item.status}`}>{item.status}</span><p><code>{item.key}</code> · {item.message}</p></article>)}</div>}</section>}
    {scopeReady && <section className="content-section"><div className="section-heading"><div><p className="eyebrow">Append-only history</p><h2>Scoped revisions and rollback</h2></div><History aria-hidden="true" /></div>
      <label className="rollback-password">Current administrator password<span>Required only for rollback and cleared after every attempt.</span><input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} /></label>
      {revisions.length === 0 ? <div className="empty-state"><History aria-hidden="true" /><div><strong>No scoped revisions</strong><p>The first applied draft creates a versioned, auditable revision.</p></div></div> : <div className="table-scroll configuration-history" tabIndex={0} aria-label="Scrollable configuration revision history"><table><thead><tr><th>Revision</th><th>Operation</th><th>Reason</th><th>Actor</th><th>Created</th><th>Action</th></tr></thead><tbody>{revisions.map((revision, index) => <tr key={revision.id}><td><code>{revision.id}</code><br /><small>scope v{revision.scope_version}</small></td><td>{revision.operation}</td><td>{revision.reason}</td><td>{revision.actor_id}</td><td>{new Date(revision.created_at).toLocaleString()}</td><td><button className="tertiary-button" type="button" disabled={index === 0} onClick={() => void rollback(revision)}><RotateCcw aria-hidden="true" />Rollback</button></td></tr>)}</tbody></table></div>}
    </section>}
  </>;
}
