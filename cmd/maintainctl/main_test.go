package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadJSONRejectsBytesBeyondLimit(t *testing.T) {
	payload := `"` + strings.Repeat("a", maxConfigDocumentBytes-2) + `" `
	if _, err := readJSON(strings.NewReader(payload)); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized valid-prefix JSON returned %v", err)
	}
}

func TestParseConfigScopeRejectsAmbiguousAndUnsafeScopes(t *testing.T) {
	for value, wantKind := range map[string]string{
		"system": "system", "project:owner-repo": "project", "job_template:nightly": "job_template",
	} {
		kind, _, err := parseConfigScope(value)
		if err != nil || kind != wantKind {
			t.Errorf("parse %q = %q, %v", value, kind, err)
		}
	}
	for _, value := range []string{"built_in", "system:one", "project", "unknown:value", "project:"} {
		if _, _, err := parseConfigScope(value); err == nil {
			t.Errorf("unsafe scope %q was accepted", value)
		}
	}
}

func TestConfigDraftCreateSendsTypedBodyAndExactETag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/config/drafts" || r.Header.Get("If-Match") != `"config-scope-7"` {
			t.Errorf("unexpected request %s %s If-Match=%q", r.Method, r.URL.Path, r.Header.Get("If-Match"))
		}
		payload, _ := io.ReadAll(r.Body)
		var body struct {
			Scope struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"scope"`
			Reason  string           `json:"reason"`
			Entries []map[string]any `json:"entries"`
		}
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Error(err)
		}
		if body.Scope.Kind != "project" || body.Scope.ID != "owner-repo" || body.Reason != "CLI parity" || len(body.Entries) != 1 {
			t.Errorf("unexpected draft body: %s", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draft":{"id":"configdraft_0123456789abcdef0123456789abcdef"}}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "entries.json")
	if err := os.WriteFile(path, []byte(`[{"key":"workflow.max_review_cycles","value":4,"configured":true,"reset":false,"secret":false}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.config([]string{"draft", "create", "--scope", "project:owner-repo", "--scope-version", "7", "--reason", "CLI parity", path}); err != nil {
		t.Fatal(err)
	}
}

func TestReadJSONAcceptsBoundedDocument(t *testing.T) {
	payload, err := readJSON(strings.NewReader(`{"schema_version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"schema_version":1}` {
		t.Fatalf("unexpected payload %s", payload)
	}
}

func TestIntelligenceQueryUsesSharedBoundedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/project-one/intelligence/query" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["term"] != "Target" || body["limit"] != float64(100) {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"project-one","revision":"abc","term":"Target","symbols":[],"relations":[],"partial":false,"failures":0}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.intelligence([]string{"query", "project-one", "Target"}); err != nil {
		t.Fatal(err)
	}
}

func TestIntelligenceRebuildRequiresAndSendsAuditedReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/project-one/intelligence/actions/rebuild" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["reason"] != "replace stale derived index" {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"cleared","next_action":"refresh","reason":"replace stale derived index"}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.intelligence([]string{"rebuild", "project-one"}); err == nil {
		t.Fatal("rebuild accepted without audited reason")
	}
	if err := client.intelligence([]string{"rebuild", "project-one", "--reason", "replace stale derived index"}); err != nil {
		t.Fatal(err)
	}
}

func TestIntelligenceCacheSimulationUsesTypedBoundedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/project-one/caches/actions/simulate" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["kind"] != "source-parse" || body["trust_domain"] != "trusted" || body["estimated_bytes"] != float64(4096) {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"project-one","trust_domain":"trusted","kind":"source-parse","current_bytes":0,"estimated_bytes":4096,"quota_bytes":536870912,"would_fit":true,"explanation":"fits"}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.intelligence([]string{"simulate-cache", "project-one", "--kind", "source-parse", "--trust-domain", "trusted", "--estimated-bytes", "4096"}); err != nil {
		t.Fatal(err)
	}
}

func TestContractApprovalUsesExactVersionAndReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/jobs/job-one/task-contract/actions/approve" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["expected_version"] != float64(3) || body["reason"] != "scope reviewed" {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"contract":{"job_id":"job-one","schema_version":1,"version":3,"status":"approved","source_kind":"free_form","requested_behavior":"repair","explicit_non_goals":[],"affected_users":[],"acceptance_criteria":[{"id":"AC","statement":"ok","verification_method":"test"}],"constraints":[],"likely_components":[],"likely_risks":[],"required_evidence":[],"required_documentation":[],"assumptions":[],"questions":[],"completion_checklist":[{"id":"DONE","statement":"done","verification_method":"audit"}],"contract_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","created_at":"2026-07-23T00:00:00Z","updated_at":"2026-07-23T00:00:00Z"}}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.contract([]string{"approve", "job-one", "--version", "3", "--reason", "scope reviewed"}); err != nil {
		t.Fatal(err)
	}
}

func TestRiskWaiverUsesServerSideReauthenticationContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/jobs/job-one/risk/waivers" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["assessment_id"] != "risk-one" || body["to_level"] != "medium" || body["reason"] != "temporary fixture waiver" || body["reauthenticated"] != nil {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"waiver":{"id":"waiver-one","job_id":"job-one","assessment_id":"risk-one","from_level":"high","to_level":"medium","reason":"temporary fixture waiver","actor_id":"reviewer","actor_role":"reviewer","reauthenticated":true,"expires_at":"2026-07-24T00:00:00Z","created_at":"2026-07-23T00:00:00Z"},"risk_assessment":{"id":"risk-two","job_id":"job-one","schema_version":1,"contract_version":1,"contract_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","level":"medium","score":100,"signals":[],"routing":{"required_stages":[],"manual_gates":[],"full_verification":true,"documentation_review":true,"publish_allowed":true},"policy_decision":{},"explanation":"waived","requested_by_operator":false,"created_at":"2026-07-23T00:00:00Z"}}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.risk([]string{"waive", "job-one", "--assessment", "risk-one", "--to", "medium", "--reason", "temporary fixture waiver", "--expires-at", "2026-07-24T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentContractsCLIListsSchemasAndJobValidationEvidence(t *testing.T) {
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv("MAINTAINER_URL", server.URL)
	t.Setenv("MAINTAINER_SESSION_FILE", filepath.Join(t.TempDir(), "session.json"))
	if err := run([]string{"agent-contracts"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"agent-contracts", "job-one"}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/api/v1/agent-contracts" || paths[1] != "/api/v1/jobs/job-one/agent-contract-validations" {
		t.Fatalf("unexpected paths %#v", paths)
	}
}

func TestEvidenceCLIListsJobTraceGraph(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"job-one","project_id":"project-one","nodes":[],"edges":[]}`))
	}))
	defer server.Close()
	t.Setenv("MAINTAINER_URL", server.URL)
	t.Setenv("MAINTAINER_SESSION_FILE", filepath.Join(t.TempDir(), "session.json"))
	if err := run([]string{"evidence", "job-one"}); err != nil {
		t.Fatal(err)
	}
	if seen != "GET /api/v1/jobs/job-one/evidence-graph" {
		t.Fatalf("unexpected evidence request %q", seen)
	}
}

func TestTestDesignerCLIListsJobReports(t *testing.T) {
	seen := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["disposition"] != "accepted" || body["reason"] != "implemented required boundary regression" {
				t.Fatalf("unexpected disposition body %#v", body)
			}
		}
		if (r.Method == http.MethodGet && r.URL.Path != "/api/v1/jobs/job-one/test-designer") ||
			(r.Method == http.MethodPost && r.URL.Path != "/api/v1/test-designer-reports/report-one/proposals/TD-1/actions/dispose") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"reports":[],"dispositions":[]}`))
	}))
	defer server.Close()
	t.Setenv("MAINTAINER_URL", server.URL)
	t.Setenv("MAINTAINER_SESSION_FILE", filepath.Join(t.TempDir(), "session.json"))
	if err := run([]string{"test-designer", "job-one"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"test-designer", "dispose", "report-one", "TD-1", "--disposition", "accepted", "--reason", "implemented required boundary regression"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("unexpected requests %#v", seen)
	}
}

func TestDocumentationCLIListsJobManifests(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if got := body["risk_level"]; got != "medium" {
				t.Errorf("risk level body = %#v", body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/jobs/job-one/documentation":
			_, _ = w.Write([]byte(`{"manifests":[]}`))
		case "/api/v1/documentation/policy/profile":
			_, _ = w.Write([]byte(`{"profile":{"id":"documentation-policy-default","version":"documentation-policy-v1","summary":"test","rules":[],"tool_profile":{"id":"tool","render_targets":[],"checks":[],"preview_modes":[]}}}`))
		case "/api/v1/documentation/policy/simulations":
			_, _ = w.Write([]byte(`{"simulation":{"profile_id":"documentation-policy-default","profile_version":"documentation-policy-v1","matched_rules":[],"requirements":[],"checks":[],"render_targets":[],"reviewer_roles":[],"publication_gates":[],"source_mappings":[],"policy_summary":"none","publication_ready":true,"no_documentation_required":true}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("MAINTAINER_URL", server.URL)
	t.Setenv("MAINTAINER_SESSION_FILE", filepath.Join(t.TempDir(), "session.json"))
	if err := run([]string{"documentation", "job-one"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"documentation", "policy"}); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "doc-policy.json")
	if err := os.WriteFile(input, []byte(`{"paths":["internal/api/openapi.yaml"],"change_classes":["public_api"],"risk_level":"medium","languages":["go"],"capability_packs":[],"labels":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"documentation", "simulate", "--input", input}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/v1/jobs/job-one/documentation",
		"GET /api/v1/documentation/policy/profile",
		"POST /api/v1/documentation/policy/simulations",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestSchedulerCLIExposesStatusDecisionsAndSimulation(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["mode"] != "quality_latency" || body["maintenance_window"] != true {
				t.Errorf("unexpected simulation body %#v", body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/scheduler/status":
			_, _ = w.Write([]byte(`{"topology":{"host_id":"local","physical_cores":1,"logical_cores":1,"total_memory_bytes":1073741824,"reserved_memory_bytes":0,"io_pressure":"normal","thermal_state":"unknown"},"profiles":[],"modes":["quality_latency"],"decisions":[]}`))
		case "/api/v1/scheduler/decisions":
			_, _ = w.Write([]byte(`{"decisions":[]}`))
		case "/api/v1/scheduler/simulations":
			_, _ = w.Write([]byte(`{"decision":{"id":"scheduler_one","mode":"quality_latency","status":"scheduled","reason":"test","rejected_job_ids":[],"deferred_job_ids":[],"co_residence_safe":true,"fairness_applied":false,"resource_summary":"test","created_at":"2026-07-24T00:00:00Z"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("MAINTAINER_URL", server.URL)
	t.Setenv("MAINTAINER_SESSION_FILE", filepath.Join(t.TempDir(), "session.json"))
	if err := run([]string{"scheduler", "status"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"scheduler", "decisions"}); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "scheduler.json")
	if err := os.WriteFile(input, []byte(`{"mode":"quality_latency","active":[],"queued":[],"maintenance_window":true,"fairness_window":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"scheduler", "simulate", "--input", input}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/v1/scheduler/status",
		"GET /api/v1/scheduler/decisions",
		"POST /api/v1/scheduler/simulations",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestGoldenCLIListsReportsAndApprovesUpdates(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if strings.Contains(r.URL.Path, "configure-profile") {
				if body["reason"] != "reviewed tolerance profile" || body["tolerance_absolute"] != 0.02 ||
					body["tolerance_relative"] != 0.001 || body["mask_dynamic_regions"] != true {
					t.Errorf("unexpected profile body %#v", body)
				}
				selectors, _ := body["mask_selectors"].([]any)
				if len(selectors) != 1 || selectors[0] != ".timestamp" {
					t.Errorf("unexpected mask selectors %#v", body["mask_selectors"])
				}
			} else if body["approved"] != true || body["reason"] != "reviewed golden update" {
				t.Errorf("unexpected body %#v", body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv("MAINTAINER_URL", server.URL)
	t.Setenv("MAINTAINER_SESSION_FILE", filepath.Join(t.TempDir(), "session.json"))
	if err := run([]string{"golden", "reports", "job-one"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"golden", "approve", "report-one", "cmp-one", "--reason", "reviewed golden update"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"golden", "configure-profile", "report-one", "cmp-one", "--reason", "reviewed tolerance profile", "--absolute", "0.02", "--relative", "0.001", "--mask-dynamic", "--mask-selector", ".timestamp"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/v1/jobs/job-one/golden-rehearsals",
		"POST /api/v1/golden-rehearsals/report-one/comparisons/cmp-one/actions/approve",
		"POST /api/v1/golden-rehearsals/report-one/comparisons/cmp-one/actions/configure-profile",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestIntelligenceCorrectionUsesTypedAuditedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/project-one/differentials/differential-one/actions/correct" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["observation_kind"] != "test" || body["observation_key"] != "unit" || body["after_classification"] != "indeterminate" || body["reason"] != "evidence incomplete" {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"correction_one"}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.intelligence([]string{"correct-differential", "project-one", "differential-one", "--kind", "test", "--key", "unit", "--classification", "indeterminate", "--reason", "evidence incomplete"}); err != nil {
		t.Fatal(err)
	}
}

func TestPackInstallUsesVersionRevisionAndAuditedReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/capability-packs/php83-intranet/actions/install" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["target_version"] != "1.0.0" || body["expected_revision"] != float64(0) || body["reason"] != "reviewed trusted catalog" {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"installation":{"pack_id":"php83-intranet","pack_version":"1.0.0"},"event":{"action":"install"}}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.pack([]string{"install", "--version", "1.0.0", "--revision", "0", "--reason", "reviewed trusted catalog", "php83-intranet"}); err != nil {
		t.Fatal(err)
	}
}

func TestPackConfigurationUsesTypedSharedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/projects/project-one/capability-packs/r-statistical-validation/configuration" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		config, _ := body["config"].(map[string]any)
		tolerance, _ := config["tolerance"].(map[string]any)
		if body["expected_revision"] != float64(3) || body["reason"] != "reviewed statistical policy" || tolerance["absolute"] != 0.02 {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"project-one","pack_id":"r-statistical-validation","pack_version":"1.0.0","enabled":true,"config":{},"revision":4,"updated_at":"2026-07-22T00:00:00Z"}`))
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "r-config.json")
	if err := os.WriteFile(configPath, []byte(`{"tolerance":{"absolute":0.02}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.pack([]string{"configure", "--revision", "3", "--config", configPath, "--reason", "reviewed statistical policy", "project-one", "r-statistical-validation"}); err != nil {
		t.Fatal(err)
	}
}

func TestPackConfigurationPreservesDuplicateKeysForControllerRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if strings.Count(string(body), `"absolute"`) != 2 {
			t.Errorf("CLI rewrote duplicate configuration keys before controller validation: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"duplicate configuration key"}}`))
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "duplicate-config.json")
	if err := os.WriteFile(configPath, []byte(`{"tolerance":{"absolute":0.01,"absolute":0.02}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	err := client.pack([]string{"configure-preview", "--revision", "1", "--config", configPath, "--reason", "validate exact bytes", "project-one", "r-statistical-validation"})
	if err == nil || !strings.Contains(err.Error(), "duplicate configuration key") {
		t.Fatalf("controller rejection = %v", err)
	}
}
