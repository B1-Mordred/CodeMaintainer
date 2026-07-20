package hermesbridge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBridgeListsOnlyNarrowToolsAndForwardsServerToken(t *testing.T) {
	token := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/hermes/tools/jobs/submit" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+string(token) {
			t.Fatalf("unexpected forwarded request: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": "job_1"})
	}))
	t.Cleanup(controller.Close)
	bridge, err := New(controller.URL, token)
	if err != nil {
		t.Fatal(err)
	}

	list := rpc(t, bridge, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	encoded, _ := json.Marshal(list.Result)
	for _, forbidden := range []string{"terminal", "shell", "docker", "github", "approve", "merge", "policy"} {
		if strings.Contains(strings.ToLower(string(encoded)), `"name":"`+forbidden) {
			t.Fatalf("forbidden tool exposed: %s", forbidden)
		}
	}
	var result struct {
		Tools []tool `json:"tools"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil || len(result.Tools) != 10 {
		t.Fatalf("tool list = %s, err=%v", encoded, err)
	}

	call := rpc(t, bridge, `{"jsonrpc":"2.0","id":"call","method":"tools/call","params":{"name":"submit_job","arguments":{"project_id":"project","task":"inspect the registered project"}}}`)
	if call.Error != nil {
		t.Fatalf("tool call failed: %+v", call.Error)
	}
	callPayload, _ := json.Marshal(call.Result)
	if !strings.Contains(string(callPayload), "job_1") {
		t.Fatalf("tool result = %s", callPayload)
	}
}

func TestBridgeRejectsUnknownMethodsAndUnsafePathArguments(t *testing.T) {
	bridge, err := New("http://controller:8080", []byte(strings.Repeat("a", 64)))
	if err != nil {
		t.Fatal(err)
	}
	unknown := rpc(t, bridge, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	if unknown.Error == nil || unknown.Error.Code != -32601 {
		t.Fatalf("unknown method response = %+v", unknown)
	}
	unsafe := rpc(t, bridge, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"job_status","arguments":{"job_id":"../../admin"}}}`)
	if unsafe.Error == nil {
		t.Fatalf("unsafe path argument was accepted: %+v", unsafe)
	}
}

func rpc(t *testing.T, handler http.Handler, body string) rpcResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	payload, _ := io.ReadAll(response.Result().Body)
	var envelope rpcResponse
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode RPC response %q: %v", payload, err)
	}
	return envelope
}
