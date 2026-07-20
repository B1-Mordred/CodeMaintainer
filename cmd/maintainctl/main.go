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
	case "config":
		return api.config(arguments[1:])
	default:
		usage()
		return fmt.Errorf("command %q is not implemented", arguments[0])
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
		provider := flags.String("provider", "local", "repository provider: local or github")
		branch := flags.String("default-branch", "main", "exact default branch")
		if err := flags.Parse(arguments[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || (*provider != "local" && *provider != "github") {
			return errors.New("usage: maintainctl repo add <owner/repository> [--provider local|github] [--default-branch branch]")
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
	default:
		return fmt.Errorf("unknown repo command %q", arguments[0])
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
		return errors.New("usage: maintainctl config <export|validate|apply|rollback>")
	}
	switch arguments[0] {
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
	response, err := c.request(method, path, body)
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
  repo add <owner/repository> [--provider local|github] [--default-branch branch]
  repo sync <owner/repository>
  run <owner/repository> --task <text> | --issue <number>
  status [job-id]
  inspect <job-id>
  logs <job-id>
  cancel <job-id>
  retry <job-id>
  verify <job-id>
  review <job-id>
  publish <job-id> --draft-pr [--rationale text]
  open [job-id]
  backup
  restore (--dry-run|--apply) <backup-id>
  config export
  config validate [file|-]
  config apply --reason <text> <file|->
  config rollback --reason <text> <revision-id>
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
