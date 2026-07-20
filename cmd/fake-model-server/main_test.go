package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local-code-maintainer/appliance/internal/agents"
)

func TestFakeModelDeterministicallyRejectsFirstQCAndPassesSecond(t *testing.T) {
	packet := agents.TaskPacket{
		SchemaVersion: 1, Mode: "qc", JobID: "job_fixture", OriginalTask: "fix", BaseSHA: strings.Repeat("a", 40),
		ResultSHA: strings.Repeat("b", 40), AcceptanceCriteria: []agents.Criterion{{ID: "AC-1", Statement: "works", VerificationMethod: "full_tests"}},
		RelevantFiles: []agents.FileContext{{Path: "answer.go", Content: "package answer"}},
	}
	server := httptest.NewServer(handler(slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	for cycle, verdict := range []string{"blocking_findings", "pass"} {
		packet.ReviewCycle = cycle
		packetPayload, _ := json.Marshal(packet)
		requestPayload, _ := json.Marshal(map[string]any{
			"model": "active", "stream": false, "temperature": 0.1, "max_tokens": 100,
			"response_format": map[string]string{"type": "json_object"},
			"messages":        []map[string]string{{"role": "system", "content": "fixed"}, {"role": "user", "content": "UNTRUSTED_TASK_PACKET_JSON\n" + string(packetPayload) + "\nEND_UNTRUSTED_TASK_PACKET_JSON"}},
		})
		response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(string(requestPayload)))
		if err != nil {
			t.Fatal(err)
		}
		var completion struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		_ = json.NewDecoder(response.Body).Decode(&completion)
		response.Body.Close()
		var report agents.QCReport
		if len(completion.Choices) != 1 {
			t.Fatalf("missing completion in cycle %d", cycle)
		}
		_ = json.Unmarshal([]byte(completion.Choices[0].Message.Content), &report)
		if report.Verdict != verdict {
			t.Fatalf("cycle %d verdict = %s, want %s", cycle, report.Verdict, verdict)
		}
	}
}
