package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/agents"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "fake-model-server")
	server := &http.Server{
		Addr: "0.0.0.0:8082", Handler: handler(logger), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("fake model server stopped", "error", err)
		os.Exit(1)
	}
}

func handler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]string{{"id": "active"}}})
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
		var request struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Temperature    float64           `json:"temperature"`
			MaxTokens      int               `json:"max_tokens"`
			ResponseFormat map[string]string `json:"response_format"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || len(request.Messages) != 2 || request.Model != "active" || request.Stream ||
			request.MaxTokens < 1 || request.MaxTokens > 16384 || request.ResponseFormat["type"] != "json_object" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid bounded fake-model request"})
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trailing request data"})
			return
		}
		packetPayload, err := extractPacket(request.Messages[1].Content)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var probe struct {
			Mode string `json:"mode"`
		}
		_ = json.Unmarshal(packetPayload, &probe)
		var content []byte
		switch probe.Mode {
		case "implementation", "repair":
			packet, err := agents.DecodeTaskPacket(packetPayload, probe.Mode)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			content, err = fakeImplementation(packet)
			if err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
				return
			}
		case "qc":
			packet, err := agents.DecodeTaskPacket(packetPayload, "qc")
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			content, _ = json.Marshal(fakeQC(packet))
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown task mode"})
			return
		}
		logger.Info("served deterministic fake completion", "mode", probe.Mode)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "fake-completion", "model": "active",
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": string(content)}}},
		})
	})
	return mux
}

func extractPacket(content string) ([]byte, error) {
	const start = "UNTRUSTED_TASK_PACKET_JSON\n"
	const end = "\nEND_UNTRUSTED_TASK_PACKET_JSON"
	if !strings.HasPrefix(content, start) || !strings.HasSuffix(content, end) {
		return nil, errors.New("task packet delimiters are missing")
	}
	payload := []byte(strings.TrimSuffix(strings.TrimPrefix(content, start), end))
	if len(payload) > agents.MaxPacketBytes || !json.Valid(payload) {
		return nil, errors.New("task packet is invalid or oversized")
	}
	return payload, nil
}

func fakeImplementation(packet agents.TaskPacket) ([]byte, error) {
	result := agents.ImplementationResult{
		SchemaVersion: 1,
		Summary: agents.ActionSummary{
			Reproduction: "The deterministic seeded regression is reproduced by the locked test evidence.",
			Plan:         "Apply the smallest source correction and retain the locked regression coverage.",
			Changes:      []string{"Corrected the seeded implementation defect."}, RegressionTests: []string{"Retained the locked regression test."},
			Checks: []string{"Controller will run targeted and full verification."}, Evidence: []string{"Structured fake-model fixture response."},
		},
	}
	for _, file := range packet.RelevantFiles {
		updated := strings.Replace(file.Content, "return a - b", "return a + b", 1)
		if packet.Mode == "repair" {
			updated = strings.Replace(updated, "return a + b + 1", "return a + b", 1)
		}
		if updated != file.Content {
			result.Edits = append(result.Edits, agents.Edit{Path: file.Path, Content: updated, ExpectedSHA256: agents.HashContent([]byte(file.Content))})
			break
		}
	}
	if len(result.Edits) == 0 && packet.Mode == "implementation" {
		return nil, fmt.Errorf("fake fixture did not contain the seeded defect")
	}
	return json.Marshal(result)
}

func fakeQC(packet agents.TaskPacket) agents.QCReport {
	report := agents.QCReport{
		SchemaVersion: 1, JobID: packet.JobID, BaseSHA: packet.BaseSHA, ResultSHA: packet.ResultSHA,
		Verdict: "pass", Findings: []agents.Finding{}, VerificationRequests: []agents.VerificationRequest{},
	}
	if packet.ReviewCycle == 0 {
		path := "unknown"
		if len(packet.RelevantFiles) != 0 {
			path = packet.RelevantFiles[0].Path
		}
		report.Verdict = "blocking_findings"
		report.Findings = []agents.Finding{{
			ID: "QC-FIXTURE-001", Severity: "must_fix", Category: "regression_coverage",
			Claim:    "The seeded fixture requires one explicit negative-input regression assertion.",
			Location: agents.Location{Path: path, Line: 1}, Evidence: "The locked fixture marks the first review cycle for deterministic rejection.",
			RequiredResolution: "Add or retain the explicit negative-input regression assertion.", VerificationMethod: "Run the allow-listed full_tests class.",
		}}
	}
	return report
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var buffer bytes.Buffer
	_ = json.NewEncoder(&buffer).Encode(value)
	_, _ = w.Write(buffer.Bytes())
}
