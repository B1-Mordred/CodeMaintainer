package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var version = "dev"

const maxConfigDocumentBytes = 2 << 20

type client struct {
	baseURL     string
	http        *http.Client
	sessionFile string
	session     cliSession
}

type cliSession struct {
	Token     string    `json:"token"`
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "maintainctl:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		usage()
		return errors.New("a command is required")
	}
	baseURL := env("MAINTAINER_URL", "http://127.0.0.1:8080")
	api := client{
		baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second},
		sessionFile: env("MAINTAINER_SESSION_FILE", "/var/lib/maintainctl/session.json"),
	}
	_ = api.loadSession()
	switch arguments[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "doctor":
		return api.printJSON(http.MethodGet, "/api/v1/system/status", nil)
	case "health":
		return api.printJSON(http.MethodGet, "/healthz", nil)
	case "bootstrap":
		return api.bootstrap(arguments[1:])
	case "login":
		return api.login(arguments[1:])
	case "reauthenticate":
		return api.reauthenticate(arguments[1:])
	case "logout":
		return api.logout()
	case "status":
		if len(arguments) > 1 {
			return api.printJSON(http.MethodGet, "/api/v1/jobs/"+url.PathEscape(arguments[1]), nil)
		}
		return api.printJSON(http.MethodGet, "/api/v1/jobs", nil)
	case "inspect":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl inspect <job-id>")
		}
		return api.printJSON(http.MethodGet, "/api/v1/jobs/"+url.PathEscape(arguments[1]), nil)
	case "logs":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl logs <job-id>")
		}
		return api.streamEvents(arguments[1])
	case "cancel", "retry":
		if len(arguments) != 2 {
			return fmt.Errorf("usage: maintainctl %s <job-id>", arguments[0])
		}
		path := "/api/v1/jobs/" + url.PathEscape(arguments[1]) + "/actions/" + arguments[0]
		return api.printJSON(http.MethodPost, path, map[string]any{})
	case "run":
		return api.runJob(arguments[1:])
	case "repo":
		return api.repo(arguments[1:])
	case "verify", "review":
		if len(arguments) != 2 {
			return fmt.Errorf("usage: maintainctl %s <job-id>", arguments[0])
		}
		return api.printJSON(http.MethodPost, "/api/v1/jobs/"+url.PathEscape(arguments[1])+"/actions/"+arguments[0], map[string]any{})
	case "publish":
		return api.publish(arguments[1:])
	case "open":
		return api.open(arguments[1:])
	case "backup":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl backup")
		}
		return api.printJSON(http.MethodPost, "/api/v1/admin/backups", map[string]any{})
	case "restore":
		return api.restore(arguments[1:])
	case "model":
		return api.model(arguments[1:])
	case "model-provider":
		return api.modelProvider(arguments[1:])
	case "config":
		return api.config(arguments[1:])
	case "intelligence":
		return api.intelligence(arguments[1:])
	case "pack":
		return api.pack(arguments[1:])
	case "forge":
		return api.forge(arguments[1:])
	case "windows-worker":
		return api.windowsWorker(arguments[1:])
	case "contract":
		return api.contract(arguments[1:])
	case "risk":
		return api.risk(arguments[1:])
	case "agent-contracts":
		if len(arguments) > 2 {
			return errors.New("usage: maintainctl agent-contracts [job-id]")
		}
		if len(arguments) == 2 {
			return api.printJSON(http.MethodGet, "/api/v1/jobs/"+url.PathEscape(arguments[1])+"/agent-contract-validations", nil)
		}
		return api.printJSON(http.MethodGet, "/api/v1/agent-contracts", nil)
	case "test-designer":
		if len(arguments) < 2 {
			return errors.New("usage: maintainctl test-designer <job-id>|dispose ...")
		}
		if arguments[1] == "dispose" {
			if len(arguments) < 4 {
				return errors.New("usage: maintainctl test-designer dispose <report-id> <proposal-id> --disposition accepted|rejected|not_applicable --reason text")
			}
			flags := flag.NewFlagSet("test-designer dispose", flag.ContinueOnError)
			disposition := flags.String("disposition", "", "accepted, rejected, or not_applicable")
			reason := flags.String("reason", "", "audited disposition reason")
			if err := flags.Parse(arguments[4:]); err != nil {
				return err
			}
			if *disposition == "" || *reason == "" {
				return errors.New("usage: maintainctl test-designer dispose <report-id> <proposal-id> --disposition accepted|rejected|not_applicable --reason text")
			}
			return api.printJSON(http.MethodPost, "/api/v1/test-designer-reports/"+url.PathEscape(arguments[2])+"/proposals/"+url.PathEscape(arguments[3])+"/actions/dispose", map[string]any{
				"disposition": *disposition, "reason": *reason,
			})
		}
		if len(arguments) != 2 || arguments[1] == "" {
			return errors.New("usage: maintainctl test-designer <job-id>")
		}
		return api.printJSON(http.MethodGet, "/api/v1/jobs/"+url.PathEscape(arguments[1])+"/test-designer", nil)
	case "documentation":
		return api.documentation(arguments[1:])
	case "scheduler":
		return api.scheduler(arguments[1:])
	case "policy":
		return api.policy(arguments[1:])
	case "golden":
		return api.golden(arguments[1:])
	default:
		usage()
		return fmt.Errorf("command %q is not implemented", arguments[0])
	}
}

func (c client) scheduler(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl scheduler <status|decisions|simulate> ...")
	}
	switch arguments[0] {
	case "status":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl scheduler status")
		}
		return c.printJSON(http.MethodGet, "/api/v1/scheduler/status", nil)
	case "decisions":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl scheduler decisions")
		}
		return c.printJSON(http.MethodGet, "/api/v1/scheduler/decisions", nil)
	case "simulate":
		flags := flag.NewFlagSet("scheduler simulate", flag.ContinueOnError)
		inputFile := flags.String("input", "", "scheduler simulation JSON file or '-' for stdin")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *inputFile == "" {
			return errors.New("usage: maintainctl scheduler simulate --input <json-file|->")
		}
		raw, err := readJSONDocument(*inputFile)
		if err != nil {
			return err
		}
		var input any
		if err := json.Unmarshal(raw, &input); err != nil {
			return err
		}
		return c.printJSON(http.MethodPost, "/api/v1/scheduler/simulations", input)
	default:
		return fmt.Errorf("scheduler action %q is not implemented", arguments[0])
	}
}

func (c client) modelProvider(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl model-provider <status|simulate|egress> ...")
	}
	switch arguments[0] {
	case "status":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl model-provider status")
		}
		return c.printJSON(http.MethodGet, "/api/v1/model-providers/status", nil)
	case "simulate":
		flags := flag.NewFlagSet("model-provider simulate", flag.ContinueOnError)
		inputFile := flags.String("input", "", "provider route simulation JSON file or '-' for stdin")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *inputFile == "" {
			return errors.New("usage: maintainctl model-provider simulate --input <json-file|->")
		}
		raw, err := readJSONDocument(*inputFile)
		if err != nil {
			return err
		}
		var input any
		if err := json.Unmarshal(raw, &input); err != nil {
			return err
		}
		return c.printJSON(http.MethodPost, "/api/v1/model-providers/routes/simulations", input)
	case "egress":
		flags := flag.NewFlagSet("model-provider egress", flag.ContinueOnError)
		projectID := flags.String("project", "", "optional project id")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("usage: maintainctl model-provider egress [--project <project-id>]")
		}
		path := "/api/v1/model-providers/egress-manifests"
		if *projectID != "" {
			path += "?project_id=" + url.QueryEscape(*projectID)
		}
		return c.printJSON(http.MethodGet, path, nil)
	default:
		return fmt.Errorf("model-provider action %q is not implemented", arguments[0])
	}
}

func (c client) documentation(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl documentation <job-id>|policy|simulate --input <json-file|->")
	}
	switch arguments[0] {
	case "policy":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl documentation policy")
		}
		return c.printJSON(http.MethodGet, "/api/v1/documentation/policy/profile", nil)
	case "simulate":
		flags := flag.NewFlagSet("documentation simulate", flag.ContinueOnError)
		inputFile := flags.String("input", "", "documentation policy simulation JSON file or '-' for stdin")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *inputFile == "" {
			return errors.New("usage: maintainctl documentation simulate --input <json-file|->")
		}
		raw, err := readJSONDocument(*inputFile)
		if err != nil {
			return err
		}
		var input any
		if err := json.Unmarshal(raw, &input); err != nil {
			return err
		}
		return c.printJSON(http.MethodPost, "/api/v1/documentation/policy/simulations", input)
	default:
		if len(arguments) != 1 || arguments[0] == "" {
			return errors.New("usage: maintainctl documentation <job-id>|policy|simulate --input <json-file|->")
		}
		return c.printJSON(http.MethodGet, "/api/v1/jobs/"+url.PathEscape(arguments[0])+"/documentation", nil)
	}
}

func (c client) policy(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl policy <bundles|create-advanced|test|test-runs|activations|simulate|activate|decisions> ...")
	}
	switch arguments[0] {
	case "bundles":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl policy bundles")
		}
		return c.printJSON(http.MethodGet, "/api/v1/policies/bundles", nil)
	case "activations":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl policy activations")
		}
		return c.printJSON(http.MethodGet, "/api/v1/policies/activations", nil)
	case "test-runs":
		flags := flag.NewFlagSet("policy test-runs", flag.ContinueOnError)
		bundleID := flags.String("bundle", "", "optional policy bundle ID")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
			return errors.New("usage: maintainctl policy test-runs [--bundle <id>]")
		}
		query := url.Values{}
		if *bundleID != "" {
			query.Set("bundle_id", *bundleID)
		}
		path := "/api/v1/policies/test-runs"
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return c.printJSON(http.MethodGet, path, nil)
	case "decisions":
		if len(arguments) != 2 || arguments[1] == "" {
			return errors.New("usage: maintainctl policy decisions <job-id>")
		}
		return c.printJSON(http.MethodGet, "/api/v1/jobs/"+url.PathEscape(arguments[1])+"/policy-decisions", nil)
	case "simulate":
		flags := flag.NewFlagSet("policy simulate", flag.ContinueOnError)
		bundleID := flags.String("bundle", "", "policy bundle ID, defaults to active")
		decisionPoint := flags.String("decision", "", "policy decision point")
		inputFile := flags.String("input", "", "JSON input file or '-' for stdin")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *decisionPoint == "" || *inputFile == "" {
			return errors.New("usage: maintainctl policy simulate --decision <point> --input <json-file|-> [--bundle <id>]")
		}
		raw, err := readJSONDocument(*inputFile)
		if err != nil {
			return err
		}
		var input any
		if err := json.Unmarshal(raw, &input); err != nil {
			return err
		}
		return c.printJSON(http.MethodPost, "/api/v1/policies/simulations", map[string]any{
			"bundle_id": *bundleID, "decision_point": *decisionPoint, "input": input,
		})
	case "create-advanced":
		flags := flag.NewFlagSet("policy create-advanced", flag.ContinueOnError)
		version := flags.String("version", "", "policy bundle version")
		reason := flags.String("reason", "", "audited creation reason")
		regoFile := flags.String("rego", "", "Rego source file")
		testsFile := flags.String("tests", "", "policy tests JSON file or '-'")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *version == "" || strings.TrimSpace(*reason) == "" || *regoFile == "" || *testsFile == "" {
			return errors.New("usage: maintainctl policy create-advanced --version <v> --reason <text> --rego <file> --tests <json-file|->; reauthenticate first")
		}
		regoSource, err := readTextDocument(*regoFile, 256*1024)
		if err != nil {
			return err
		}
		tests, err := readJSONDocument(*testsFile)
		if err != nil {
			return err
		}
		var decodedTests any
		if err := json.Unmarshal(tests, &decodedTests); err != nil {
			return err
		}
		return c.printJSON(http.MethodPost, "/api/v1/policies/bundles", map[string]any{
			"version": *version, "reason": *reason, "rego_source": regoSource, "tests": decodedTests,
		})
	case "test":
		if len(arguments) < 2 || arguments[1] == "" {
			return errors.New("usage: maintainctl policy test <bundle-id> --tests <json-file|->")
		}
		flags := flag.NewFlagSet("policy test", flag.ContinueOnError)
		testsFile := flags.String("tests", "", "policy tests JSON file or '-'")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *testsFile == "" {
			return errors.New("usage: maintainctl policy test <bundle-id> --tests <json-file|->")
		}
		tests, err := readJSONDocument(*testsFile)
		if err != nil {
			return err
		}
		var decodedTests any
		if err := json.Unmarshal(tests, &decodedTests); err != nil {
			return err
		}
		return c.printJSON(http.MethodPost, "/api/v1/policies/bundles/"+url.PathEscape(arguments[1])+"/actions/test", map[string]any{"tests": decodedTests})
	case "activate":
		if len(arguments) < 2 || arguments[1] == "" {
			return errors.New("usage: maintainctl policy activate <bundle-id> --reason <text> [--staged-rollout-percent <0-100>]; reauthenticate first")
		}
		flags := flag.NewFlagSet("policy activate", flag.ContinueOnError)
		reason := flags.String("reason", "", "audited activation reason")
		staged := flags.Int("staged-rollout-percent", 100, "staged rollout percent")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*reason) == "" || *staged < 0 || *staged > 100 {
			return errors.New("usage: maintainctl policy activate <bundle-id> --reason <text> [--staged-rollout-percent <0-100>]; reauthenticate first")
		}
		return c.printJSON(http.MethodPost, "/api/v1/policies/bundles/"+url.PathEscape(arguments[1])+"/actions/activate", map[string]any{
			"action": "activate", "reason": *reason, "staged_rollout_percent": *staged,
		})
	default:
		return errors.New("usage: maintainctl policy <bundles|create-advanced|test|test-runs|activations|simulate|activate|decisions> ...")
	}
}

func (c client) golden(arguments []string) error {
	if len(arguments) < 2 {
		return errors.New("usage: maintainctl golden <reports|approve|reject|configure-profile> ...")
	}
	switch arguments[0] {
	case "reports":
		if len(arguments) != 2 || arguments[1] == "" {
			return errors.New("usage: maintainctl golden reports <job-id>")
		}
		return c.printJSON(http.MethodGet, "/api/v1/jobs/"+url.PathEscape(arguments[1])+"/golden-rehearsals", nil)
	case "approve", "reject":
		if len(arguments) < 3 {
			return errors.New("usage: maintainctl golden approve|reject <report-id> <comparison-id> --reason text")
		}
		flags := flag.NewFlagSet("golden "+arguments[0], flag.ContinueOnError)
		reason := flags.String("reason", "", "audited reason")
		if err := flags.Parse(arguments[3:]); err != nil {
			return err
		}
		if strings.TrimSpace(*reason) == "" {
			return errors.New("usage: maintainctl golden approve|reject <report-id> <comparison-id> --reason text")
		}
		return c.printJSON(http.MethodPost, "/api/v1/golden-rehearsals/"+url.PathEscape(arguments[1])+"/comparisons/"+url.PathEscape(arguments[2])+"/actions/approve", map[string]any{
			"approved": arguments[0] == "approve",
			"reason":   *reason,
		})
	case "configure-profile":
		if len(arguments) < 3 {
			return errors.New("usage: maintainctl golden configure-profile <report-id> <comparison-id> --reason text --absolute <n> --relative <n> [--mask-dynamic] [--mask-selector <selector> ...]")
		}
		flags := flag.NewFlagSet("golden configure-profile", flag.ContinueOnError)
		reason := flags.String("reason", "", "audited profile reason")
		absolute := flags.Float64("absolute", 0, "absolute tolerance")
		relative := flags.Float64("relative", 0, "relative tolerance")
		maskDynamic := flags.Bool("mask-dynamic", false, "mask declared dynamic regions")
		var selectors repeatedFlag
		flags.Var(&selectors, "mask-selector", "bounded selector to mask; repeatable")
		if err := flags.Parse(arguments[3:]); err != nil {
			return err
		}
		if strings.TrimSpace(*reason) == "" || *absolute < 0 || *relative < 0 || *relative > 1 {
			return errors.New("usage: maintainctl golden configure-profile <report-id> <comparison-id> --reason text --absolute <n> --relative <n> [--mask-dynamic] [--mask-selector <selector> ...]")
		}
		return c.printJSON(http.MethodPost, "/api/v1/golden-rehearsals/"+url.PathEscape(arguments[1])+"/comparisons/"+url.PathEscape(arguments[2])+"/actions/configure-profile", map[string]any{
			"reason": *reason, "tolerance_absolute": *absolute, "tolerance_relative": *relative,
			"mask_dynamic_regions": *maskDynamic, "mask_selectors": []string(selectors),
		})
	default:
		return errors.New("usage: maintainctl golden <reports|approve|reject|configure-profile> ...")
	}
}

func (c client) contract(arguments []string) error {
	if len(arguments) < 2 {
		return errors.New("usage: maintainctl contract <get|update|approve> <job-id> ...")
	}
	jobID := url.PathEscape(arguments[1])
	base := "/api/v1/jobs/" + jobID + "/task-contract"
	switch arguments[0] {
	case "get":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl contract get <job-id>")
		}
		return c.printJSON(http.MethodGet, base, nil)
	case "update":
		flags := flag.NewFlagSet("contract update", flag.ContinueOnError)
		version := flags.Int64("version", 0, "current task-contract version")
		reason := flags.String("reason", "", "audited contract edit reason")
		file := flags.String("file", "", "task-contract JSON document")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *version < 1 || strings.TrimSpace(*reason) == "" || *file == "" {
			return errors.New("usage: maintainctl contract update <job-id> --version <n> --reason <text> --file <json>")
		}
		raw, err := readJSONDocument(*file)
		if err != nil {
			return err
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			return err
		}
		body["expected_version"] = *version
		body["reason"] = *reason
		return c.printJSON(http.MethodPut, base, body)
	case "approve":
		flags := flag.NewFlagSet("contract approve", flag.ContinueOnError)
		version := flags.Int64("version", 0, "current task-contract version")
		reason := flags.String("reason", "", "audited approval reason")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *version < 1 || strings.TrimSpace(*reason) == "" {
			return errors.New("usage: maintainctl contract approve <job-id> --version <n> --reason <text>")
		}
		return c.printJSON(http.MethodPost, base+"/actions/approve", map[string]any{"expected_version": *version, "reason": *reason})
	default:
		return fmt.Errorf("contract action %q is not implemented", arguments[0])
	}
}

func (c client) risk(arguments []string) error {
	if len(arguments) < 2 {
		return errors.New("usage: maintainctl risk <get|waive> <job-id> ...")
	}
	jobID := url.PathEscape(arguments[1])
	base := "/api/v1/jobs/" + jobID + "/risk"
	switch arguments[0] {
	case "get":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl risk get <job-id>")
		}
		return c.printJSON(http.MethodGet, base, nil)
	case "waive":
		flags := flag.NewFlagSet("risk waive", flag.ContinueOnError)
		assessmentID := flags.String("assessment", "", "current risk assessment ID")
		toLevel := flags.String("to", "", "lower target level: low or medium")
		reason := flags.String("reason", "", "audited risk-waiver reason")
		expiresAt := flags.String("expires-at", "", "RFC3339 waiver expiry")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *assessmentID == "" || (*toLevel != "low" && *toLevel != "medium") || strings.TrimSpace(*reason) == "" || *expiresAt == "" {
			return errors.New("usage: maintainctl risk waive <job-id> --assessment <id> --to <low|medium> --reason <text> --expires-at <RFC3339>; reauthenticate first")
		}
		if _, err := time.Parse(time.RFC3339, *expiresAt); err != nil {
			return fmt.Errorf("expires-at must be RFC3339: %w", err)
		}
		return c.printJSON(http.MethodPost, base+"/waivers", map[string]any{"assessment_id": *assessmentID, "to_level": *toLevel, "reason": *reason, "expires_at": *expiresAt})
	default:
		return fmt.Errorf("risk action %q is not implemented", arguments[0])
	}
}

func (c client) windowsWorker(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl windows-worker <profiles|get|save|probe|runs|run> ...")
	}
	switch arguments[0] {
	case "profiles":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl windows-worker profiles")
		}
		return c.printJSON(http.MethodGet, "/api/v1/windows-workers/profiles", nil)
	case "get", "probe", "runs":
		if len(arguments) != 2 {
			return fmt.Errorf("usage: maintainctl windows-worker %s <profile-id>", arguments[0])
		}
		base := "/api/v1/windows-workers/profiles/" + url.PathEscape(arguments[1])
		switch arguments[0] {
		case "get":
			return c.printJSON(http.MethodGet, base, nil)
		case "probe":
			return c.printJSON(http.MethodPost, base+"/actions/probe", nil)
		default:
			return c.printJSON(http.MethodGet, base+"/runs", nil)
		}
	case "save", "run":
		if len(arguments) != 3 {
			return fmt.Errorf("usage: maintainctl windows-worker %s <profile-id> <closed-json-file|->", arguments[0])
		}
		raw, err := readJSONDocument(arguments[2])
		if err != nil {
			return err
		}
		var body any
		if err := json.Unmarshal(raw, &body); err != nil {
			return err
		}
		base := "/api/v1/windows-workers/profiles/" + url.PathEscape(arguments[1])
		if arguments[0] == "save" {
			return c.printJSON(http.MethodPut, base, body)
		}
		return c.printJSON(http.MethodPost, base+"/runs", body)
	default:
		return fmt.Errorf("windows-worker action %q is not implemented", arguments[0])
	}
}

func (c client) forge(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl forge <profiles|get|save|probe|sync|runs|objects> ...")
	}
	switch arguments[0] {
	case "profiles":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl forge profiles")
		}
		return c.printJSON(http.MethodGet, "/api/v1/forges/profiles", nil)
	case "get", "probe", "runs", "objects":
		if len(arguments) < 2 {
			return fmt.Errorf("usage: maintainctl forge %s <project-id>", arguments[0])
		}
		base := "/api/v1/projects/" + url.PathEscape(arguments[1])
		switch arguments[0] {
		case "get":
			return c.printJSON(http.MethodGet, base+"/forge-profile", nil)
		case "probe":
			return c.printJSON(http.MethodPost, base+"/forge-profile/actions/probe", nil)
		case "runs":
			return c.printJSON(http.MethodGet, base+"/forge-sync-runs", nil)
		default:
			kind := ""
			if len(arguments) == 4 && arguments[2] == "--kind" {
				kind = "?kind=" + url.QueryEscape(arguments[3])
			} else if len(arguments) != 2 {
				return errors.New("usage: maintainctl forge objects <project-id> [--kind kind]")
			}
			return c.printJSON(http.MethodGet, base+"/forge-objects"+kind, nil)
		}
	case "save":
		if len(arguments) != 3 {
			return errors.New("usage: maintainctl forge save <project-id> <profile-json-file|->")
		}
		raw, err := readJSONDocument(arguments[2])
		if err != nil {
			return err
		}
		var body any
		if err := json.Unmarshal(raw, &body); err != nil {
			return err
		}
		return c.printJSON(http.MethodPut, "/api/v1/projects/"+url.PathEscape(arguments[1])+"/forge-profile", body)
	case "sync":
		if len(arguments) < 2 {
			return errors.New("usage: maintainctl forge sync <project-id> --key <idempotency-key> [--cursor cursor]")
		}
		projectID := arguments[1]
		flags := flag.NewFlagSet("forge sync", flag.ContinueOnError)
		key := flags.String("key", "", "safe idempotency key")
		cursor := flags.String("cursor", "", "last durable provider cursor")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*key) == "" {
			return errors.New("usage: maintainctl forge sync <project-id> --key <idempotency-key> [--cursor cursor]")
		}
		return c.printJSON(http.MethodPost, "/api/v1/projects/"+url.PathEscape(projectID)+"/forge-profile/actions/sync", map[string]any{"cursor": *cursor, "idempotency_key": *key})
	default:
		return fmt.Errorf("forge action %q is not implemented", arguments[0])
	}
}

func (c client) intelligence(arguments []string) error {
	if len(arguments) < 2 {
		return errors.New("usage: maintainctl intelligence <status|query|refresh|rebuild|contexts|compare-contexts|baselines|differentials|test-impacts|caches|verify-cache|simulate-cache|warm-cache|purge-cache> <project-id> [arguments]")
	}
	action, projectID := arguments[0], arguments[1]
	if projectID == "" {
		return errors.New("project ID is required")
	}
	base := "/api/v1/projects/" + url.PathEscape(projectID)
	switch action {
	case "status":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl intelligence status <project-id>")
		}
		return c.printJSON(http.MethodGet, base+"/intelligence/status", nil)
	case "query":
		if len(arguments) != 3 {
			return errors.New("usage: maintainctl intelligence query <project-id> <term>")
		}
		return c.printJSON(http.MethodPost, base+"/intelligence/query", map[string]any{"term": arguments[2], "limit": 100})
	case "refresh":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl intelligence refresh <project-id>")
		}
		return c.printJSON(http.MethodPost, base+"/intelligence/actions/refresh", nil)
	case "rebuild":
		flags := flag.NewFlagSet("intelligence rebuild", flag.ContinueOnError)
		reason := flags.String("reason", "", "audited rebuild reason")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*reason) == "" {
			return errors.New("usage: maintainctl intelligence rebuild <project-id> --reason <text>; reauthenticate first")
		}
		return c.printJSON(http.MethodPost, base+"/intelligence/actions/rebuild", map[string]any{"reason": *reason})
	case "contexts":
		return c.printJSON(http.MethodGet, base+"/context-manifests", nil)
	case "compare-contexts":
		if len(arguments) != 4 {
			return errors.New("usage: maintainctl intelligence compare-contexts <project-id> <left-manifest-id> <right-manifest-id>")
		}
		return c.printJSON(http.MethodPost, base+"/context-manifests/actions/compare", map[string]any{"left_id": arguments[2], "right_id": arguments[3]})
	case "baselines":
		return c.printJSON(http.MethodGet, base+"/baselines", nil)
	case "baseline-updates":
		return c.printJSON(http.MethodGet, base+"/baseline-supersessions", nil)
	case "supersede-baseline":
		if len(arguments) < 3 {
			return errors.New("usage: maintainctl intelligence supersede-baseline <project-id> <baseline-id> --differential id --reason text; reauthenticate first")
		}
		baselineID := arguments[2]
		flags := flag.NewFlagSet("intelligence supersede-baseline", flag.ContinueOnError)
		differentialID := flags.String("differential", "", "candidate differential identity")
		reason := flags.String("reason", "", "audited intentional update reason")
		if err := flags.Parse(arguments[3:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *differentialID == "" || strings.TrimSpace(*reason) == "" {
			return errors.New("usage: maintainctl intelligence supersede-baseline <project-id> <baseline-id> --differential id --reason text; reauthenticate first")
		}
		return c.printJSON(http.MethodPost, base+"/baselines/"+url.PathEscape(baselineID)+"/actions/supersede", map[string]any{"differential_id": *differentialID, "reason": *reason})
	case "differentials":
		return c.printJSON(http.MethodGet, base+"/differentials", nil)
	case "corrections":
		return c.printJSON(http.MethodGet, base+"/differential-corrections", nil)
	case "correct-differential":
		if len(arguments) < 3 {
			return errors.New("usage: maintainctl intelligence correct-differential <project-id> <differential-id> --kind kind --key key --classification class --reason text; reauthenticate first")
		}
		differentialID := arguments[2]
		flags := flag.NewFlagSet("intelligence correct-differential", flag.ContinueOnError)
		kind := flags.String("kind", "", "observation kind")
		key := flags.String("key", "", "observation key")
		classification := flags.String("classification", "", "corrected classification")
		reason := flags.String("reason", "", "audited correction reason")
		if err := flags.Parse(arguments[3:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *kind == "" || *key == "" || *classification == "" || strings.TrimSpace(*reason) == "" {
			return errors.New("usage: maintainctl intelligence correct-differential <project-id> <differential-id> --kind kind --key key --classification class --reason text; reauthenticate first")
		}
		return c.printJSON(http.MethodPost, base+"/differentials/"+url.PathEscape(differentialID)+"/actions/correct", map[string]any{"observation_kind": *kind, "observation_key": *key, "after_classification": *classification, "reason": *reason})
	case "test-impacts":
		return c.printJSON(http.MethodGet, base+"/test-impacts", nil)
	case "impact-overrides":
		return c.printJSON(http.MethodGet, base+"/test-impact-overrides", nil)
	case "override-impact":
		if len(arguments) < 3 {
			return errors.New("usage: maintainctl intelligence override-impact <project-id> <impact-id> --test id [--selected=false] --reason text [--expires-at RFC3339]; reauthenticate first")
		}
		impactID := arguments[2]
		flags := flag.NewFlagSet("intelligence override-impact", flag.ContinueOnError)
		testID := flags.String("test", "", "target test identity")
		selected := flags.Bool("selected", true, "include or omit the targeted test")
		reason := flags.String("reason", "", "audited override reason")
		expiresAt := flags.String("expires-at", "", "optional RFC 3339 expiry")
		if err := flags.Parse(arguments[3:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *testID == "" || strings.TrimSpace(*reason) == "" {
			return errors.New("usage: maintainctl intelligence override-impact <project-id> <impact-id> --test id [--selected=false] --reason text [--expires-at RFC3339]; reauthenticate first")
		}
		return c.printJSON(http.MethodPost, base+"/test-impacts/"+url.PathEscape(impactID)+"/actions/override", map[string]any{"test_id": *testID, "selected": *selected, "reason": *reason, "expires_at": *expiresAt})
	case "caches":
		return c.printJSON(http.MethodGet, base+"/caches", nil)
	case "verify-cache":
		flags := flag.NewFlagSet("intelligence verify-cache", flag.ContinueOnError)
		kind := flags.String("kind", "", "exact cache kind, or all project caches")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("usage: maintainctl intelligence verify-cache <project-id> [--kind kind]")
		}
		return c.printJSON(http.MethodPost, base+"/caches/actions/verify", map[string]any{"kind": *kind})
	case "simulate-cache":
		flags := flag.NewFlagSet("intelligence simulate-cache", flag.ContinueOnError)
		kind := flags.String("kind", "", "cache kind")
		trustDomain := flags.String("trust-domain", "", "cache trust domain")
		estimatedBytes := flags.Int64("estimated-bytes", -1, "estimated verified object bytes")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *kind == "" || *trustDomain == "" || *estimatedBytes < 0 {
			return errors.New("usage: maintainctl intelligence simulate-cache <project-id> --kind kind --trust-domain domain --estimated-bytes bytes")
		}
		return c.printJSON(http.MethodPost, base+"/caches/actions/simulate", map[string]any{"kind": *kind, "trust_domain": *trustDomain, "estimated_bytes": *estimatedBytes})
	case "warm-cache":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl intelligence warm-cache <project-id>")
		}
		return c.printJSON(http.MethodPost, base+"/caches/actions/warm", nil)
	case "purge-cache":
		flags := flag.NewFlagSet("intelligence purge-cache", flag.ContinueOnError)
		kind := flags.String("kind", "", "exact cache kind, or all project caches")
		reason := flags.String("reason", "", "audited purge reason")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*reason) == "" {
			return errors.New("usage: maintainctl intelligence purge-cache <project-id> [--kind kind] --reason <text>; reauthenticate first")
		}
		return c.printJSON(http.MethodPost, base+"/caches/actions/purge", map[string]any{"kind": *kind, "reason": *reason})
	default:
		return fmt.Errorf("intelligence action %q is not implemented", action)
	}
}

func (c client) repo(arguments []string) error {
	if len(arguments) < 2 {
		return errors.New("usage: maintainctl repo <add|sync> <owner/repository>")
	}
	repository := arguments[1]
	if !validRepositoryName(repository) {
		return errors.New("repository must be owner/repository using letters, numbers, dot, underscore, or hyphen")
	}
	switch arguments[0] {
	case "add":
		flags := flag.NewFlagSet("repo add", flag.ContinueOnError)
		provider := flags.String("provider", "local", "repository provider: local, github, or gitlab")
		branch := flags.String("default-branch", "main", "exact default branch")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || (*provider != "local" && *provider != "github" && *provider != "gitlab") {
			return errors.New("usage: maintainctl repo add <owner/repository> [--provider local|github|gitlab] [--default-branch branch]")
		}
		id := strings.ReplaceAll(repository, "/", "-")
		body := map[string]any{"id": id, "provider": *provider, "repository": repository, "default_branch": *branch}
		if *provider == "local" {
			body["local_remote_name"] = id + ".git"
		}
		return c.printJSON(http.MethodPost, "/api/v1/projects", body)
	case "sync":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl repo sync <owner/repository>")
		}
		return c.printJSON(http.MethodPost, "/api/v1/projects/"+url.PathEscape(strings.ReplaceAll(repository, "/", "-"))+"/actions/sync", map[string]any{})
	case "doctor":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl repo doctor <owner/repository>")
		}
		return c.printJSON(http.MethodPost, "/api/v1/projects/"+url.PathEscape(strings.ReplaceAll(repository, "/", "-"))+"/repo-doctor/actions/scan", nil)
	case "doctor-scans":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl repo doctor-scans <owner/repository>")
		}
		return c.printJSON(http.MethodGet, "/api/v1/projects/"+url.PathEscape(strings.ReplaceAll(repository, "/", "-"))+"/repo-doctor/scans", nil)
	default:
		return fmt.Errorf("unknown repo command %q", arguments[0])
	}
}

func (c client) pack(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl pack <catalog|installed|show|events|preview|install|enable|disable|upgrade|rollback|pin|unpin|assignments|configure-preview|configure|proposal> ...")
	}
	switch arguments[0] {
	case "catalog":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl pack catalog")
		}
		return c.printJSON(http.MethodGet, "/api/v1/capability-packs", nil)
	case "installed":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl pack installed")
		}
		return c.printJSON(http.MethodGet, "/api/v1/capability-packs/installations", nil)
	case "show":
		if len(arguments) != 3 {
			return errors.New("usage: maintainctl pack show <pack-id> <version>")
		}
		return c.printJSON(http.MethodGet, "/api/v1/capability-packs/"+url.PathEscape(arguments[1])+"/versions/"+url.PathEscape(arguments[2]), nil)
	case "events":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl pack events <pack-id>")
		}
		return c.printJSON(http.MethodGet, "/api/v1/capability-packs/"+url.PathEscape(arguments[1])+"/events", nil)
	case "assignments":
		if len(arguments) != 2 {
			return errors.New("usage: maintainctl pack assignments <project-id>")
		}
		return c.printJSON(http.MethodGet, "/api/v1/projects/"+url.PathEscape(arguments[1])+"/capability-packs", nil)
	case "configure-preview", "configure":
		action := arguments[0]
		flags := flag.NewFlagSet("pack "+action, flag.ContinueOnError)
		revision := flags.Int64("revision", 0, "current assignment revision")
		reason := flags.String("reason", "preview typed pack configuration", "audited configuration reason")
		configPath := flags.String("config", "", "typed JSON configuration file")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 2 || *revision < 1 || *configPath == "" || (action == "configure" && strings.TrimSpace(*reason) == "") {
			return fmt.Errorf("usage: maintainctl pack %s --revision <n> --config <file> --reason <text> <project-id> <pack-id>", action)
		}
		raw, err := readJSONDocument(*configPath)
		if err != nil {
			return err
		}
		if !json.Valid(raw) {
			return errors.New("configuration file must contain valid JSON")
		}
		projectID, packID := url.PathEscape(flags.Arg(0)), url.PathEscape(flags.Arg(1))
		body := map[string]any{"expected_revision": *revision, "reason": *reason, "config": json.RawMessage(raw)}
		if action == "configure-preview" {
			return c.printJSON(http.MethodPost, "/api/v1/projects/"+projectID+"/capability-packs/"+packID+"/actions/preview-configuration", body)
		}
		return c.printJSON(http.MethodPut, "/api/v1/projects/"+projectID+"/capability-packs/"+packID+"/configuration", body)
	case "preview", "install", "enable", "disable", "upgrade", "rollback", "pin", "unpin":
		action := arguments[0]
		flags := flag.NewFlagSet("pack "+action, flag.ContinueOnError)
		version := flags.String("version", "", "trusted target version")
		previewAction := flags.String("action", "install", "preview action: install, upgrade, or rollback")
		revision := flags.Int64("revision", 0, "current installation revision")
		reason := flags.String("reason", "", "audited reason")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 1 || strings.TrimSpace(*reason) == "" {
			return fmt.Errorf("usage: maintainctl pack %s <pack-id> --version <version> --revision <n> --reason <text>", action)
		}
		body := map[string]any{"target_version": *version, "expected_revision": *revision, "reason": *reason}
		packID := url.PathEscape(flags.Arg(0))
		if action == "preview" {
			return c.printJSON(http.MethodPost, "/api/v1/capability-packs/"+packID+"/actions/preview?action="+url.QueryEscape(*previewAction), body)
		}
		return c.printJSON(http.MethodPost, "/api/v1/capability-packs/"+packID+"/actions/"+action, body)
	case "proposal":
		if len(arguments) < 5 {
			return errors.New("usage: maintainctl pack proposal <dry-run|accept|reject> <project-id> <scan-id> <proposal-id> [flags]")
		}
		action := arguments[1]
		if action != "dry-run" && action != "accept" && action != "reject" {
			return errors.New("proposal action must be dry-run, accept, or reject")
		}
		flags := flag.NewFlagSet("pack proposal "+action, flag.ContinueOnError)
		revision := flags.Int64("version", 0, "proposal version")
		reason := flags.String("reason", "preview proposal", "review reason")
		configPath := flags.String("config", "", "optional JSON configuration file")
		if err := flags.Parse(arguments[5:]); err != nil {
			return err
		}
		body := map[string]any{"expected_version": *revision, "reason": *reason}
		if *configPath != "" {
			raw, err := readJSONDocument(*configPath)
			if err != nil {
				return err
			}
			if !json.Valid(raw) {
				return errors.New("configuration file must contain valid JSON")
			}
			body["config"] = json.RawMessage(raw)
		}
		path := "/api/v1/projects/" + url.PathEscape(arguments[2]) + "/repo-doctor/scans/" + url.PathEscape(arguments[3]) + "/proposals/" + url.PathEscape(arguments[4]) + "/actions/" + action
		return c.printJSON(http.MethodPost, path, body)
	default:
		return fmt.Errorf("pack action %q is not implemented", arguments[0])
	}
}

func validRepositoryName(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(value) > 200 {
		return false
	}
	for _, part := range parts {
		for _, character := range part {
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || strings.ContainsRune("._-", character)) {
				return false
			}
		}
	}
	return true
}

func (c client) publish(arguments []string) error {
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	draft := flags.Bool("draft-pr", false, "approve exact result for draft pull request publication")
	rationale := flags.String("rationale", "approved for draft pull request publication", "audited reviewer rationale")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 1 || !*draft {
		return errors.New("usage: maintainctl publish <job-id> --draft-pr [--rationale text]")
	}
	return c.printJSON(http.MethodPost, "/api/v1/jobs/"+url.PathEscape(flags.Arg(0))+"/actions/approve-publication", map[string]any{"rationale": *rationale})
}

func (c client) open(arguments []string) error {
	if len(arguments) > 1 {
		return errors.New("usage: maintainctl open [job-id]")
	}
	target := c.baseURL
	if len(arguments) == 1 {
		target += "/?job=" + url.QueryEscape(arguments[0])
	}
	fmt.Println(target)
	return nil
}

func (c client) restore(arguments []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	dryRun := flags.Bool("dry-run", false, "validate archive compatibility and checksums without changing state")
	apply := flags.Bool("apply", false, "stage a validated restore for the next full appliance restart")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 1 || *dryRun == *apply {
		return errors.New("usage: maintainctl restore (--dry-run|--apply) <backup-id>")
	}
	return c.printJSON(http.MethodPost, "/api/v1/admin/backups/"+url.PathEscape(flags.Arg(0))+"/actions/restore", map[string]any{"dry_run": *dryRun})
}

func (c client) model(arguments []string) error {
	if len(arguments) == 1 && arguments[0] == "list" {
		return c.printJSON(http.MethodGet, "/api/v1/models", nil)
	}
	if len(arguments) == 2 && arguments[0] == "benchmark" {
		return c.printJSON(http.MethodPost, "/api/v1/models/"+url.PathEscape(arguments[1])+"/actions/benchmark", map[string]any{})
	}
	return errors.New("usage: maintainctl model list | maintainctl model benchmark <profile>")
}

func (c client) config(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl config <descriptors|values|effective|registry-export|import-preview|import|draft|history|registry-rollback|export|validate|apply|rollback>")
	}
	switch arguments[0] {
	case "descriptors":
		flags := flag.NewFlagSet("config descriptors", flag.ContinueOnError)
		search := flags.String("search", "", "search keys, labels, help, and groups")
		basic := flags.Bool("basic", false, "hide bootstrap and advanced descriptors")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
			return errors.New("usage: maintainctl config descriptors [--search text] [--basic]")
		}
		query := url.Values{}
		if *search != "" {
			query.Set("q", *search)
		}
		query.Set("advanced", strconv.FormatBool(!*basic))
		return c.printJSON(http.MethodGet, "/api/v1/config/descriptors?"+query.Encode(), nil)
	case "values":
		flags := flag.NewFlagSet("config values", flag.ContinueOnError)
		scope := flags.String("scope", "", "scope as system or kind:id")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 || *scope == "" {
			return errors.New("usage: maintainctl config values --scope <system|kind:id>")
		}
		query, err := configScopeQuery(*scope)
		if err != nil {
			return err
		}
		return c.printJSON(http.MethodGet, "/api/v1/config/values?"+query.Encode(), nil)
	case "effective":
		flags := flag.NewFlagSet("config effective", flag.ContinueOnError)
		var scopes repeatedFlag
		flags.Var(&scopes, "scope", "scope as system or kind:id; repeat in precedence order")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
			return errors.New("usage: maintainctl config effective [--scope <system|kind:id>]...")
		}
		bodyScopes := make([]map[string]string, 0, len(scopes))
		for _, value := range scopes {
			kind, id, err := parseConfigScope(value)
			if err != nil {
				return err
			}
			scope := map[string]string{"kind": kind}
			if id != "" {
				scope["id"] = id
			}
			bodyScopes = append(bodyScopes, scope)
		}
		return c.printJSON(http.MethodPost, "/api/v1/config/effective", map[string]any{"scopes": bodyScopes})
	case "registry-export":
		flags := flag.NewFlagSet("config registry-export", flag.ContinueOnError)
		scope := flags.String("scope", "", "scope as system or kind:id")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 || *scope == "" {
			return errors.New("usage: maintainctl config registry-export --scope <system|kind:id>")
		}
		query, err := configScopeQuery(*scope)
		if err != nil {
			return err
		}
		return c.printJSON(http.MethodGet, "/api/v1/config/export?"+query.Encode(), nil)
	case "import-preview", "import":
		return c.configImport(arguments[0], arguments[1:])
	case "draft":
		return c.configDraft(arguments[1:])
	case "history":
		flags := flag.NewFlagSet("config history", flag.ContinueOnError)
		scope := flags.String("scope", "", "scope as system or kind:id")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 || *scope == "" {
			return errors.New("usage: maintainctl config history --scope <system|kind:id>")
		}
		query, err := configScopeQuery(*scope)
		if err != nil {
			return err
		}
		return c.printJSON(http.MethodGet, "/api/v1/config/registry-revisions?"+query.Encode(), nil)
	case "registry-rollback":
		flags := flag.NewFlagSet("config registry-rollback", flag.ContinueOnError)
		version := flags.Int64("scope-version", -1, "current scope version from the ETag")
		reason := flags.String("reason", "", "audited rollback reason")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 1 || *version < 0 || *reason == "" {
			return errors.New("usage: maintainctl config registry-rollback --scope-version <n> --reason <text> <revision-id>")
		}
		path := "/api/v1/config/registry-revisions/" + url.PathEscape(flags.Arg(0)) + "/actions/rollback"
		return c.printJSONWithHeaders(http.MethodPost, path, map[string]any{"reason": *reason}, map[string]string{"If-Match": configCLIETag("scope", *version)})
	case "export":
		if len(arguments) != 1 {
			return errors.New("usage: maintainctl config export")
		}
		return c.printJSON(http.MethodGet, "/api/v1/config", nil)
	case "validate":
		if len(arguments) > 2 {
			return errors.New("usage: maintainctl config validate [file|-]")
		}
		path := "-"
		if len(arguments) == 2 {
			path = arguments[1]
		}
		document, err := readJSONDocument(path)
		if err != nil {
			return err
		}
		return c.printJSON(http.MethodPost, "/api/v1/config/validate", map[string]any{"document": document})
	case "apply":
		flags := flag.NewFlagSet("config apply", flag.ContinueOnError)
		reason := flags.String("reason", "", "audited reason for the configuration change")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if *reason == "" || flags.NArg() != 1 {
			return errors.New("usage: maintainctl config apply --reason <text> <file|->")
		}
		document, err := readJSONDocument(flags.Arg(0))
		if err != nil {
			return err
		}
		return c.printJSON(http.MethodPost, "/api/v1/config/revisions", map[string]any{"document": document, "reason": *reason})
	case "rollback":
		flags := flag.NewFlagSet("config rollback", flag.ContinueOnError)
		reason := flags.String("reason", "", "audited reason for rollback")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if *reason == "" || flags.NArg() != 1 {
			return errors.New("usage: maintainctl config rollback --reason <text> <revision-id>")
		}
		path := "/api/v1/config/revisions/" + url.PathEscape(flags.Arg(0)) + "/rollback"
		return c.printJSON(http.MethodPost, path, map[string]any{"reason": *reason})
	default:
		return fmt.Errorf("unknown config command %q", arguments[0])
	}
}

func (c client) configImport(command string, arguments []string) error {
	flags := flag.NewFlagSet("config "+command, flag.ContinueOnError)
	scope := flags.String("scope", "", "target scope as system or kind:id")
	version := flags.Int64("scope-version", -1, "current target scope version from the ETag")
	mode := flags.String("mode", "strict", "strict or forward_compatible")
	reason := flags.String("reason", "", "audited import reason")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 1 || *scope == "" || *version < 0 || (command == "import" && *reason == "") {
		reasonUsage := ""
		if command == "import" {
			reasonUsage = "--reason <text> "
		}
		return fmt.Errorf("usage: maintainctl config %s --scope <system|kind:id> --scope-version <n> --mode <strict|forward_compatible> %s<file|->", command, reasonUsage)
	}
	kind, id, err := parseConfigScope(*scope)
	if err != nil {
		return err
	}
	document, err := readJSONDocument(flags.Arg(0))
	if err != nil {
		return err
	}
	var decoded any
	if err := json.Unmarshal(document, &decoded); err != nil {
		return err
	}
	target := map[string]string{"kind": kind}
	if id != "" {
		target["id"] = id
	}
	body := map[string]any{"mode": *mode, "target": target, "document": decoded}
	path := "/api/v1/config/import/preview"
	if command == "import" {
		path = "/api/v1/config/import"
		body["reason"] = *reason
	}
	return c.printJSONWithHeaders(http.MethodPost, path, body, map[string]string{"If-Match": configCLIETag("scope", *version)})
}

func (c client) configDraft(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl config draft <list|create|get|update|checks|validate|dry-run|review|apply|discard>")
	}
	switch arguments[0] {
	case "list":
		flags := flag.NewFlagSet("config draft list", flag.ContinueOnError)
		scope := flags.String("scope", "", "scope as system or kind:id")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 || *scope == "" {
			return errors.New("usage: maintainctl config draft list --scope <system|kind:id>")
		}
		query, err := configScopeQuery(*scope)
		if err != nil {
			return err
		}
		return c.printJSON(http.MethodGet, "/api/v1/config/drafts?"+query.Encode(), nil)
	case "get", "checks":
		if len(arguments) != 2 {
			return fmt.Errorf("usage: maintainctl config draft %s <draft-id>", arguments[0])
		}
		path := "/api/v1/config/drafts/" + url.PathEscape(arguments[1])
		if arguments[0] == "checks" {
			path += "/checks"
		}
		return c.printJSON(http.MethodGet, path, nil)
	case "create":
		flags := flag.NewFlagSet("config draft create", flag.ContinueOnError)
		scope := flags.String("scope", "", "scope as system or kind:id")
		version := flags.Int64("scope-version", -1, "current scope version from the ETag")
		reason := flags.String("reason", "", "audited draft reason")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 1 || *scope == "" || *version < 0 || *reason == "" {
			return errors.New("usage: maintainctl config draft create --scope <system|kind:id> --scope-version <n> --reason <text> <entries-file|->")
		}
		kind, id, err := parseConfigScope(*scope)
		if err != nil {
			return err
		}
		entries, err := readJSONDocument(flags.Arg(0))
		if err != nil {
			return err
		}
		var entryList []any
		if err := json.Unmarshal(entries, &entryList); err != nil {
			return errors.New("configuration draft entries must be a JSON array")
		}
		bodyScope := map[string]string{"kind": kind}
		if id != "" {
			bodyScope["id"] = id
		}
		body := map[string]any{"scope": bodyScope, "reason": *reason, "entries": entryList}
		return c.printJSONWithHeaders(http.MethodPost, "/api/v1/config/drafts", body, map[string]string{"If-Match": configCLIETag("scope", *version)})
	case "update":
		flags := flag.NewFlagSet("config draft update", flag.ContinueOnError)
		version := flags.Int64("version", 0, "current draft version from the ETag")
		reason := flags.String("reason", "", "audited draft reason")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 2 || *version < 1 || *reason == "" {
			return errors.New("usage: maintainctl config draft update --version <n> --reason <text> <draft-id> <entries-file|->")
		}
		entries, err := readJSONDocument(flags.Arg(1))
		if err != nil {
			return err
		}
		var entryList []any
		if err := json.Unmarshal(entries, &entryList); err != nil {
			return errors.New("configuration draft entries must be a JSON array")
		}
		path := "/api/v1/config/drafts/" + url.PathEscape(flags.Arg(0))
		return c.printJSONWithHeaders(http.MethodPut, path, map[string]any{"reason": *reason, "entries": entryList}, map[string]string{"If-Match": configCLIETag("draft", *version)})
	case "validate", "dry-run":
		if len(arguments) != 2 {
			return fmt.Errorf("usage: maintainctl config draft %s <draft-id>", arguments[0])
		}
		return c.printJSON(http.MethodPost, "/api/v1/config/drafts/"+url.PathEscape(arguments[1])+"/actions/"+arguments[0], map[string]any{})
	case "review", "apply", "discard":
		flags := flag.NewFlagSet("config draft "+arguments[0], flag.ContinueOnError)
		version := flags.Int64("version", 0, "current draft version from the ETag")
		reason := flags.String("reason", "", "audited action reason")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 1 || *version < 1 || *reason == "" {
			return fmt.Errorf("usage: maintainctl config draft %s --version <n> --reason <text> <draft-id>", arguments[0])
		}
		path := "/api/v1/config/drafts/" + url.PathEscape(flags.Arg(0)) + "/actions/" + arguments[0]
		return c.printJSONWithHeaders(http.MethodPost, path, map[string]any{"reason": *reason}, map[string]string{"If-Match": configCLIETag("draft", *version)})
	default:
		return fmt.Errorf("unknown config draft command %q", arguments[0])
	}
}

type repeatedFlag []string

func (values *repeatedFlag) String() string { return strings.Join(*values, ",") }
func (values *repeatedFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func parseConfigScope(value string) (string, string, error) {
	kind, id, hasID := strings.Cut(strings.TrimSpace(value), ":")
	switch kind {
	case "system":
		if hasID || id != "" {
			return "", "", errors.New("system scope must not have an id")
		}
	case "capability_pack", "project", "environment", "job_template", "job_override":
		if !hasID || strings.TrimSpace(id) == "" || len(id) > 256 {
			return "", "", fmt.Errorf("scope %s requires a bounded id", kind)
		}
	default:
		return "", "", fmt.Errorf("unsupported configuration scope %q", kind)
	}
	return kind, id, nil
}

func configScopeQuery(value string) (url.Values, error) {
	kind, id, err := parseConfigScope(value)
	if err != nil {
		return nil, err
	}
	query := url.Values{"scope_kind": []string{kind}}
	if id != "" {
		query.Set("scope_id", id)
	}
	return query, nil
}

func configCLIETag(kind string, version int64) string {
	return fmt.Sprintf(`"config-%s-%d"`, kind, version)
}

func readJSONDocument(path string) (json.RawMessage, error) {
	var source io.Reader = os.Stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open configuration %s: %w", path, err)
		}
		defer file.Close()
		source = file
	}
	return readJSON(source)
}

func readTextDocument(path string, limit int64) (string, error) {
	var source io.Reader = os.Stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open document %s: %w", path, err)
		}
		defer file.Close()
		source = file
	}
	payload, err := io.ReadAll(io.LimitReader(source, limit+1))
	if err != nil {
		return "", fmt.Errorf("read document: %w", err)
	}
	if int64(len(payload)) > limit {
		return "", fmt.Errorf("document exceeds %d bytes", limit)
	}
	return string(payload), nil
}

func readJSON(source io.Reader) (json.RawMessage, error) {
	payload, err := io.ReadAll(io.LimitReader(source, maxConfigDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	if len(payload) > maxConfigDocumentBytes {
		return nil, fmt.Errorf("configuration exceeds %d bytes", maxConfigDocumentBytes)
	}
	if !json.Valid(payload) {
		return nil, errors.New("configuration must be valid JSON")
	}
	return json.RawMessage(payload), nil
}

func (c client) runJob(arguments []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	task := flags.String("task", "", "maintenance task text")
	issue := flags.Int64("issue", 0, "Git provider issue number")
	project := flags.String("project", "", "controller project ID")
	if len(arguments) == 0 {
		return errors.New("usage: maintainctl run <owner/repository> --task <text> | --issue <number>")
	}
	repository := arguments[0]
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if (*task == "" && *issue == 0) || (*task != "" && *issue != 0) {
		return errors.New("exactly one of --task or --issue is required")
	}
	if *task != "" {
		if info, err := os.Stat(*task); err == nil {
			if !info.Mode().IsRegular() || info.Size() > maxConfigDocumentBytes {
				return errors.New("task file must be a regular file no larger than 2 MiB")
			}
			payload, readErr := os.ReadFile(*task)
			if readErr != nil {
				return fmt.Errorf("read task file: %w", readErr)
			}
			*task = strings.TrimSpace(string(payload))
			if *task == "" {
				return errors.New("task file is empty")
			}
		}
	}
	if *project == "" {
		*project = strings.ReplaceAll(repository, "/", "-")
	}
	body := map[string]any{"project_id": *project, "repository": repository, "task": *task}
	if *issue != 0 {
		body["issue_number"] = *issue
		body["task"] = fmt.Sprintf("Resolve issue #%d", *issue)
	}
	return c.printJSON(http.MethodPost, "/api/v1/jobs", body)
}

func (c client) printJSON(method, path string, body any) error {
	return c.printJSONWithHeaders(method, path, body, nil)
}

func (c client) printJSONWithHeaders(method, path string, body any, headers map[string]string) error {
	response, err := c.requestWithHeaders(method, path, body, headers)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("controller returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return fmt.Errorf("decode controller response: %w", err)
	}
	formatted, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(formatted))
	return nil
}

func (c client) request(method, path string, body any) (*http.Response, error) {
	return c.requestWithHeaders(method, path, body, nil)
}

func (c client) requestWithHeaders(method, path string, body any, headers map[string]string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if c.session.Token != "" {
		request.AddCookie(&http.Cookie{Name: "maintainer_session", Value: c.session.Token})
	}
	if c.session.CSRFToken != "" && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		request.Header.Set("X-CSRF-Token", c.session.CSRFToken)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", c.baseURL, err)
	}
	return response, nil
}

func (c client) streamEvents(jobID string) error {
	request, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/v1/jobs/"+url.PathEscape(jobID)+"/events", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	if c.session.Token != "" {
		request.AddCookie(&http.Cookie{Name: "maintainer_session", Value: c.session.Token})
	}
	streamClient := *c.http
	streamClient.Timeout = 0
	response, err := streamClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("controller returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	_, err = io.Copy(os.Stdout, response.Body)
	return err
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: maintainctl <command>

Commands:
  bootstrap --username <name> --display-name <name> --password-file <file|->
  login --username <name> --password-file <file|->
  logout
  doctor
  up (repository wrapper)
  down (repository wrapper)
  repo add <owner/repository> [--provider local|github|gitlab] [--default-branch branch]
  repo sync <owner/repository>
  repo doctor|doctor-scans <owner/repository>
  run <owner/repository> --task <text> | --issue <number>
  status [job-id]
  inspect <job-id>
  logs <job-id>
  cancel <job-id>
  retry <job-id>
  verify <job-id>
  review <job-id>
  publish <job-id> --draft-pr [--rationale text]
  contract get <job-id>
  contract update <job-id> --version <n> --reason <text> --file <json>
  contract approve <job-id> --version <n> --reason <text>
  risk get <job-id>
  risk waive <job-id> --assessment <id> --to <low|medium> --reason <text> --expires-at <RFC3339>
  agent-contracts [job-id]
  test-designer <job-id>
  test-designer dispose <report-id> <proposal-id> --disposition accepted|rejected|not_applicable --reason text
  documentation <job-id>
  documentation policy
  documentation simulate --input <json-file|->
  model-provider status
  model-provider simulate --input <json-file|->
  model-provider egress [--project <project-id>]
  scheduler status
  scheduler decisions
  scheduler simulate --input <json-file|->
  golden reports <job-id>
  golden approve|reject <report-id> <comparison-id> --reason text
  golden configure-profile <report-id> <comparison-id> --reason text --absolute <n> --relative <n> [--mask-dynamic] [--mask-selector selector]
  open [job-id]
  backup
  restore (--dry-run|--apply) <backup-id>
  config export
  config validate [file|-]
  config apply --reason <text> <file|->
  config rollback --reason <text> <revision-id>
  config descriptors [--search text] [--basic]
  config values --scope <system|kind:id>
  config effective [--scope <system|kind:id>]...
  config registry-export --scope <system|kind:id>
  config import-preview --scope <scope> --scope-version <n> --mode <mode> <file|->
  config import --scope <scope> --scope-version <n> --mode <mode> --reason <text> <file|->
  config draft list --scope <system|kind:id>
  config draft create --scope <scope> --scope-version <n> --reason <text> <entries-file|->
  config draft get <draft-id>
  config draft update --version <n> --reason <text> <draft-id> <entries-file|->
  config draft checks|validate|dry-run <draft-id>
  config draft review|apply|discard --version <n> --reason <text> <draft-id>
  config history --scope <system|kind:id>
  config registry-rollback --scope-version <n> --reason <text> <revision-id>
  intelligence status|refresh|rebuild <project-id>
  intelligence query <project-id> <term>
  intelligence contexts|baselines|differentials|test-impacts|caches <project-id>
  intelligence baseline-updates|corrections|impact-overrides <project-id>
  intelligence supersede-baseline <project-id> <baseline-id> --differential id --reason text
  intelligence correct-differential <project-id> <differential-id> --kind kind --key key --classification class --reason text
  intelligence override-impact <project-id> <impact-id> --test id [--selected=false] --reason text [--expires-at RFC3339]
  intelligence compare-contexts <project-id> <left-manifest-id> <right-manifest-id>
  intelligence verify-cache <project-id> [--kind kind]
  intelligence simulate-cache <project-id> --kind kind --trust-domain domain --estimated-bytes bytes
  intelligence warm-cache <project-id>
	pack catalog|installed
	pack show|events <pack-id> [version]
	pack preview|install|enable|disable|upgrade|rollback|pin|unpin <pack-id> --version <version> --revision <n> --reason <text>
	pack assignments <project-id>
	pack proposal <dry-run|accept|reject> <project-id> <scan-id> <proposal-id> --version <n> --reason <text> [--config file]
  forge profiles|get|save|probe|sync|runs|objects
  windows-worker profiles|get|save|probe|runs|run
  intelligence purge-cache <project-id> [--kind kind] --reason <text>
  model list
  model benchmark <profile>
  version`)
}

func (c *client) bootstrap(arguments []string) error {
	flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	username := flags.String("username", "", "local administrator username")
	displayName := flags.String("display-name", "", "local administrator display name")
	passwordFile := flags.String("password-file", "", "protected password file, or - for stdin")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *username == "" || *displayName == "" || *passwordFile == "" {
		return errors.New("usage: maintainctl bootstrap --username <name> --display-name <name> --password-file <file|->")
	}
	password, err := readPassword(*passwordFile)
	if err != nil {
		return err
	}
	return c.authenticate("/api/v1/auth/bootstrap", map[string]string{
		"username": *username, "display_name": *displayName, "password": password,
	})
}

func (c *client) login(arguments []string) error {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	username := flags.String("username", "", "local username")
	passwordFile := flags.String("password-file", "", "protected password file, or - for stdin")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *username == "" || *passwordFile == "" {
		return errors.New("usage: maintainctl login --username <name> --password-file <file|->")
	}
	password, err := readPassword(*passwordFile)
	if err != nil {
		return err
	}
	return c.authenticate("/api/v1/auth/login", map[string]string{"username": *username, "password": password})
}

func (c *client) authenticate(path string, body any) error {
	response, err := c.request(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("controller returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	var result struct {
		CSRFToken string `json:"csrf_token"`
		Principal struct {
			ExpiresAt time.Time `json:"expires_at"`
			User      struct {
				Username string `json:"username"`
				Role     string `json:"role"`
			} `json:"user"`
		} `json:"principal"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return fmt.Errorf("decode authentication response: %w", err)
	}
	var token string
	for _, cookie := range response.Cookies() {
		if cookie.Name == "maintainer_session" {
			token = cookie.Value
		}
	}
	if token == "" || result.CSRFToken == "" || result.Principal.ExpiresAt.IsZero() {
		return errors.New("controller returned an incomplete authentication session")
	}
	c.session = cliSession{Token: token, CSRFToken: result.CSRFToken, ExpiresAt: result.Principal.ExpiresAt}
	if err := c.saveSession(); err != nil {
		return err
	}
	fmt.Printf("Authenticated as %s (%s); session expires %s\n", result.Principal.User.Username, result.Principal.User.Role, result.Principal.ExpiresAt.Format(time.RFC3339))
	return nil
}

func (c *client) reauthenticate(arguments []string) error {
	flags := flag.NewFlagSet("reauthenticate", flag.ContinueOnError)
	passwordFile := flags.String("password-file", "", "protected password file, or - for stdin")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *passwordFile == "" {
		return errors.New("usage: maintainctl reauthenticate --password-file <file|->")
	}
	password, err := readPassword(*passwordFile)
	if err != nil {
		return err
	}
	response, err := c.request(http.MethodPost, "/api/v1/auth/reauthenticate", map[string]string{"password": password})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("controller returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	fmt.Println("Recent reauthentication recorded for five minutes.")
	return nil
}

func (c *client) logout() error {
	if c.session.Token == "" {
		return errors.New("no maintainctl session is stored")
	}
	response, err := c.request(http.MethodPost, "/api/v1/auth/logout", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("controller returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	if err := os.Remove(c.sessionFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove session file: %w", err)
	}
	c.session = cliSession{}
	fmt.Println("Session revoked.")
	return nil
}

func (c *client) loadSession() error {
	file, err := os.Open(c.sessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 16<<10 {
		return errors.New("maintainctl session file must be a private regular file")
	}
	if err := json.NewDecoder(io.LimitReader(file, 16<<10)).Decode(&c.session); err != nil {
		return err
	}
	if !time.Now().Before(c.session.ExpiresAt) {
		c.session = cliSession{}
	}
	return nil
}

func (c *client) saveSession() error {
	directory := filepath.Dir(c.sessionFile)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect session directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".session-*")
	if err != nil {
		return fmt.Errorf("create temporary session: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	if err := encoder.Encode(c.session); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, c.sessionFile); err != nil {
		return fmt.Errorf("publish session file: %w", err)
	}
	return nil
}

func readPassword(path string) (string, error) {
	var reader io.Reader = os.Stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open password file: %w", err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return "", errors.New("password file must be a private regular file")
		}
		reader = file
	}
	payload, err := io.ReadAll(io.LimitReader(reader, 1026))
	if err != nil {
		return "", err
	}
	if len(payload) > 1025 {
		return "", errors.New("password exceeds 1024 bytes")
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(payload), "\n"), "\r")
	if len(password) < 14 {
		return "", errors.New("password must contain at least 14 characters")
	}
	return password, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
