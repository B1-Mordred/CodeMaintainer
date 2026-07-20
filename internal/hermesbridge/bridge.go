package hermesbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxPayloadBytes = int64(4 << 20)

type Bridge struct {
	controller string
	token      string
	client     *http.Client
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func New(controller string, token []byte) (*Bridge, error) {
	parsed, err := url.Parse(controller)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || len(token) < 32 {
		return nil, errors.New("Hermes bridge controller configuration is invalid")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Bridge{
		controller: strings.TrimSuffix(controller, "/"), token: string(token),
		client: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if r.URL.Path != "/mcp" || r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST /mcp is supported"})
		return
	}
	var request rpcRequest
	if err := decodeBounded(r.Body, &request); err != nil || request.JSONRPC != "2.0" || request.Method == "" {
		b.writeRPC(w, nil, nil, &rpcError{Code: -32600, Message: "invalid JSON-RPC request"})
		return
	}
	if strings.HasPrefix(request.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch request.Method {
	case "initialize":
		b.writeRPC(w, request.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]bool{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "local-code-maintainer", "version": "1"},
		}, nil)
	case "ping":
		b.writeRPC(w, request.ID, map[string]any{}, nil)
	case "tools/list":
		b.writeRPC(w, request.ID, map[string]any{"tools": tools()}, nil)
	case "tools/call":
		var call toolCall
		if err := json.Unmarshal(request.Params, &call); err != nil || call.Name == "" || len(call.Arguments) == 0 {
			b.writeRPC(w, request.ID, nil, &rpcError{Code: -32602, Message: "invalid tool arguments"})
			return
		}
		result, status, err := b.callController(r.Context(), call)
		if err != nil {
			b.writeRPC(w, request.ID, nil, &rpcError{Code: -32000, Message: "controller tool call failed"})
			return
		}
		b.writeRPC(w, request.ID, map[string]any{
			"content": []map[string]string{{"type": "text", "text": string(result)}},
			"isError": status >= http.StatusBadRequest,
		}, nil)
	default:
		b.writeRPC(w, request.ID, nil, &rpcError{Code: -32601, Message: "method not found"})
	}
}

func (b *Bridge) writeRPC(w http.ResponseWriter, id json.RawMessage, result any, rpcErr *rpcError) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr})
}

func (b *Bridge) callController(ctx context.Context, call toolCall) ([]byte, int, error) {
	var arguments map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return nil, 0, err
	}
	method, path, body, err := controllerRequest(call.Name, arguments)
	if err != nil {
		return nil, 0, err
	}
	var reader io.Reader
	if body != nil {
		payload, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, 0, marshalErr
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, b.controller+path, reader)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+b.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := b.client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxPayloadBytes+1))
	if err != nil || int64(len(payload)) > maxPayloadBytes {
		return nil, 0, errors.New("controller response is unreadable or oversized")
	}
	if !json.Valid(payload) {
		payload, _ = json.Marshal(map[string]string{"error": "controller returned a non-JSON response"})
	}
	return payload, response.StatusCode, nil
}

func controllerRequest(name string, arguments map[string]json.RawMessage) (string, string, any, error) {
	requiredString := func(key string) (string, error) {
		var value string
		if err := json.Unmarshal(arguments[key], &value); err != nil || !safeID(value) {
			return "", fmt.Errorf("%s is invalid", key)
		}
		return value, nil
	}
	plainBody := func(keys ...string) (map[string]json.RawMessage, error) {
		body := make(map[string]json.RawMessage, len(keys))
		for _, key := range keys {
			value, ok := arguments[key]
			if ok {
				body[key] = value
			}
		}
		return body, nil
	}
	switch name {
	case "submit_job":
		return http.MethodPost, "/api/v1/hermes/tools/jobs/submit", arguments, nil
	case "list_jobs":
		return http.MethodGet, "/api/v1/hermes/tools/jobs", nil, nil
	case "job_status", "cancel_job", "retrieve_report", "request_review", "request_publication_approval":
		jobID, err := requiredString("job_id")
		if err != nil {
			return "", "", nil, err
		}
		action := ""
		method := http.MethodGet
		var body any
		switch name {
		case "cancel_job":
			method, action = http.MethodPost, "/cancel"
		case "retrieve_report":
			action = "/report"
		case "request_review":
			method, action = http.MethodPost, "/request-review"
			body, _ = plainBody("rationale")
		case "request_publication_approval":
			method, action = http.MethodPost, "/request-publication-approval"
			body, _ = plainBody("rationale")
		}
		return method, "/api/v1/hermes/tools/jobs/" + url.PathEscape(jobID) + action, body, nil
	case "query_project_memory":
		projectID, err := requiredString("project_id")
		if err != nil {
			return "", "", nil, err
		}
		var query string
		if err := json.Unmarshal(arguments["query"], &query); err != nil || strings.TrimSpace(query) == "" || len(query) > 4096 {
			return "", "", nil, errors.New("query is invalid")
		}
		return http.MethodGet, "/api/v1/hermes/tools/projects/" + url.PathEscape(projectID) + "/memory?q=" + url.QueryEscape(query), nil, nil
	case "list_schedules":
		return http.MethodGet, "/api/v1/hermes/tools/schedules", nil, nil
	case "propose_skill":
		return http.MethodPost, "/api/v1/hermes/tools/skill-proposals", arguments, nil
	default:
		return "", "", nil, errors.New("unknown controller tool")
	}
}

func safeID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || (index > 0 && strings.ContainsRune("._-", character)) {
			continue
		}
		return false
	}
	return true
}

func tools() []tool {
	object := func(required []string, properties map[string]any) map[string]any {
		schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
		if len(required) != 0 {
			schema["required"] = required
		}
		return schema
	}
	text := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	id := func(description string) map[string]any {
		return map[string]any{"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$", "description": description}
	}
	return []tool{
		{Name: "submit_job", Description: "Submit a bounded maintenance task to an enabled registered project.", InputSchema: object([]string{"project_id", "task"}, map[string]any{"project_id": id("registered project ID"), "task": text("maintenance task"), "issue_number": map[string]any{"type": "integer", "minimum": 1}})},
		{Name: "list_jobs", Description: "List recent controller jobs.", InputSchema: object(nil, map[string]any{})},
		{Name: "job_status", Description: "Retrieve one job's durable status.", InputSchema: object([]string{"job_id"}, map[string]any{"job_id": id("job ID")})},
		{Name: "cancel_job", Description: "Request cancellation of one non-terminal job.", InputSchema: object([]string{"job_id"}, map[string]any{"job_id": id("job ID")})},
		{Name: "retrieve_report", Description: "Retrieve bounded job, phase, finding, and artifact evidence.", InputSchema: object([]string{"job_id"}, map[string]any{"job_id": id("job ID")})},
		{Name: "request_review", Description: "Notify operators that a job is ready for review; this grants no approval.", InputSchema: object([]string{"job_id", "rationale"}, map[string]any{"job_id": id("job ID"), "rationale": text("reason for review")})},
		{Name: "request_publication_approval", Description: "Notify reviewers that publication approval is requested; this grants no approval.", InputSchema: object([]string{"job_id", "rationale"}, map[string]any{"job_id": id("job ID"), "rationale": text("reason for approval request")})},
		{Name: "query_project_memory", Description: "Search verified memory inside one registered project namespace.", InputSchema: object([]string{"project_id", "query"}, map[string]any{"project_id": id("registered project ID"), "query": text("bounded search query")})},
		{Name: "list_schedules", Description: "List configured controller schedules and their budgets.", InputSchema: object(nil, map[string]any{})},
		{Name: "propose_skill", Description: "Create a versioned, secret-scanned skill proposal that remains inert.", InputSchema: object([]string{"name", "description", "content"}, map[string]any{"name": id("proposal name"), "description": text("proposal purpose"), "content": text("proposed skill content")})},
	}
}

func decodeBounded(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxPayloadBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request contains trailing data")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
