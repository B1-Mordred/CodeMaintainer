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
	"strings"
	"time"
)

var version = "dev"

type client struct {
	baseURL string
	http    *http.Client
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
	api := client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}}
	switch arguments[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "doctor":
		return api.printJSON(http.MethodGet, "/api/v1/system/status", nil)
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
	case "config":
		if len(arguments) == 2 && arguments[1] == "export" {
			return api.printJSON(http.MethodGet, "/api/v1/config", nil)
		}
		return errors.New("usage: maintainctl config export")
	default:
		usage()
		return fmt.Errorf("command %q is not implemented", arguments[0])
	}
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
	request.Header.Set("X-Maintainer-Actor", env("MAINTAINER_ACTOR", "maintainctl"))
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

Foundation commands:
  doctor
  run <owner/repository> --task <text> | --issue <number>
  status [job-id]
  inspect <job-id>
  logs <job-id>
  cancel <job-id>
  retry <job-id>
  config export
  version`)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
