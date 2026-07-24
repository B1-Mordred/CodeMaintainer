package agents

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	documentation "github.com/B1-Mordred/CodeMaintainer/internal/docagent"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
)

//go:embed prompts/*.txt
var promptFiles embed.FS

type ModelClient struct {
	endpoint string
	client   *http.Client
}

type CompletionClient interface {
	Complete(context.Context, string, TaskPacket, ContractKind, string) ([]byte, error)
}

func NewModelClient(endpoint string) (*ModelClient, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/v1") {
		return nil, errors.New("model endpoint must be a credential-free internal HTTP v1 endpoint")
	}
	return &ModelClient{endpoint: strings.TrimSuffix(endpoint, "/"), client: &http.Client{Timeout: 30 * time.Minute}}, nil
}

func (c *ModelClient) Complete(ctx context.Context, systemPrompt string, packet TaskPacket, responseContract ContractKind, validationFeedback string) ([]byte, error) {
	packetPayload, err := json.Marshal(packet)
	if err != nil {
		return nil, err
	}
	responseFormat, err := ResponseFormat(responseContract)
	if err != nil {
		return nil, err
	}
	userContent := "UNTRUSTED_TASK_PACKET_JSON\n" + string(packetPayload) + "\nEND_UNTRUSTED_TASK_PACKET_JSON"
	if feedback := boundedValidationFeedback(validationFeedback); feedback != "" {
		userContent += "\n\nCONTROLLER_VALIDATION_FEEDBACK\n" + feedback + "\nEND_CONTROLLER_VALIDATION_FEEDBACK"
	}
	requestPayload, err := json.Marshal(map[string]any{
		"model": "active", "stream": false, "temperature": 0.1, "max_tokens": 16384,
		"response_format": responseFormat,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userContent},
		},
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/chat/completions", bytes.NewReader(requestPayload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 12<<20+1))
	if err != nil || len(payload) > 12<<20 || response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bounded model request failed with HTTP %d", response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &completion); err != nil || len(completion.Choices) != 1 || completion.Choices[0].Message.Content == "" {
		return nil, errors.New("model response does not contain one structured completion")
	}
	return []byte(completion.Choices[0].Message.Content), nil
}

func RunImplementation(ctx context.Context, client CompletionClient, packetPayload []byte, worktree string) ([]byte, error) {
	mode := "implementation"
	var modeProbe struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(packetPayload, &modeProbe)
	if modeProbe.Mode == "repair" {
		mode = "repair"
	}
	packet, err := DecodeTaskPacket(packetPayload, mode)
	if err != nil {
		return nil, err
	}
	prompt, err := promptFiles.ReadFile("prompts/implementation.txt")
	if err != nil {
		return nil, err
	}
	response, err := completeValidated(ctx, client, string(prompt), packet, ContractImplementationResult, "implementation_result", func(candidate []byte) error {
		_, decodeErr := DecodeImplementationResult(candidate)
		return decodeErr
	})
	if err != nil {
		return nil, err
	}
	result, err := DecodeImplementationResult(response)
	if err != nil {
		return nil, err
	}
	if err := ApplyEdits(worktree, result.Edits); err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func RunQC(ctx context.Context, client CompletionClient, packetPayload []byte) ([]byte, error) {
	packet, err := DecodeTaskPacket(packetPayload, "qc")
	if err != nil {
		return nil, err
	}
	prompt, err := promptFiles.ReadFile("prompts/qc.txt")
	if err != nil {
		return nil, err
	}
	response, err := completeValidated(ctx, client, string(prompt), packet, ContractQCReport, "qc_report", func(candidate []byte) error {
		_, decodeErr := DecodeQCReport(candidate, packet)
		return decodeErr
	})
	if err != nil {
		return nil, err
	}
	report, err := DecodeQCReport(response, packet)
	if err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

func RunTestDesigner(ctx context.Context, client CompletionClient, packetPayload []byte) ([]byte, error) {
	packet, err := DecodeTaskPacket(packetPayload, "test_designer")
	if err != nil {
		return nil, err
	}
	prompt, err := promptFiles.ReadFile("prompts/test_designer.txt")
	if err != nil {
		return nil, err
	}
	response, err := completeValidated(ctx, client, string(prompt), packet, ContractTestProposal, "test_proposal", func(candidate []byte) error {
		_, decodeErr := testdesigner.DecodeReport(candidate, packet.JobID, packet.ContractSHA256, packet.RiskLevel, packet.ResultSHA)
		return decodeErr
	})
	if err != nil {
		return nil, err
	}
	report, err := testdesigner.DecodeReport(response, packet.JobID, packet.ContractSHA256, packet.RiskLevel, packet.ResultSHA)
	if err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

func RunDocumentation(ctx context.Context, client CompletionClient, packetPayload []byte, worktree string) ([]byte, error) {
	packet, err := DecodeTaskPacket(packetPayload, "documentation")
	if err != nil {
		return nil, err
	}
	prompt, err := promptFiles.ReadFile("prompts/documentation.txt")
	if err != nil {
		return nil, err
	}
	response, err := completeValidated(ctx, client, string(prompt), packet, ContractDocumentationManifest, "documentation_manifest", func(candidate []byte) error {
		_, decodeErr := documentation.DecodeManifest(candidate, packet.JobID, packet.ProjectID, packet.ContractSHA256, packet.RiskLevel, packet.ResultSHA)
		return decodeErr
	})
	if err != nil {
		return nil, err
	}
	manifest, err := documentation.DecodeManifest(response, packet.JobID, packet.ProjectID, packet.ContractSHA256, packet.RiskLevel, packet.ResultSHA)
	if err != nil {
		return nil, err
	}
	edits := make([]Edit, 0, len(manifest.Edits))
	for _, edit := range manifest.Edits {
		edits = append(edits, Edit{Path: edit.Path, Content: edit.Content, ExpectedSHA256: edit.ExpectedSHA256})
	}
	if len(edits) != 0 {
		if err := ApplyEdits(worktree, edits); err != nil {
			return nil, err
		}
	}
	return json.Marshal(manifest)
}

func completeValidated(ctx context.Context, client CompletionClient, prompt string, packet TaskPacket, contract ContractKind, phase string, validate func([]byte) error) ([]byte, error) {
	descriptor, err := DescriptorFor(contract, SchemaVersion)
	if err != nil {
		return nil, err
	}
	attempts := descriptor.RetryPolicy.MaxAttempts
	if attempts < 1 || attempts > 20 {
		return nil, errors.New("agent contract retry policy is outside controller bounds")
	}
	var feedback string
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		response, err := client.Complete(ctx, prompt, packet, contract, feedback)
		if err != nil {
			return nil, err
		}
		if err := validate(response); err != nil {
			lastErr = err
			if !descriptor.RetryPolicy.ValidationFeedback || attempt == attempts {
				break
			}
			feedback = fmt.Sprintf("Attempt %d of %d for %s was rejected by trusted controller validation: %s. Return one corrected JSON document that exactly matches %s schema version %d. Do not include markdown, comments, hidden reasoning, extra fields, or unrelated content.",
				attempt, attempts, phase, err.Error(), contract, descriptor.SchemaVersion)
			continue
		}
		return response, nil
	}
	if lastErr == nil {
		lastErr = errors.New("structured output did not validate")
	}
	return nil, fmt.Errorf("%s validation failed after %d attempt(s): %w", phase, attempts, lastErr)
}

func boundedValidationFeedback(feedback string) string {
	feedback = strings.TrimSpace(feedback)
	if len(feedback) > 4096 {
		feedback = feedback[:4096] + "...[truncated]"
	}
	return feedback
}

func ApplyEdits(worktree string, edits []Edit) error {
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil || !filepath.IsAbs(root) {
		return errors.New("worktree root cannot be resolved")
	}
	for _, edit := range edits {
		if !safeRelativePath(edit.Path) {
			return errors.New("edit path is unsafe")
		}
		target := filepath.Join(root, edit.Path)
		parent, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil {
			return fmt.Errorf("edit parent must already exist: %w", err)
		}
		relativeParent, err := filepath.Rel(root, parent)
		if err != nil || relativeParent == ".." || strings.HasPrefix(relativeParent, ".."+string(filepath.Separator)) {
			return errors.New("edit parent escapes worktree")
		}
		info, statErr := os.Lstat(target)
		if statErr == nil {
			if !info.Mode().IsRegular() || edit.ExpectedSHA256 == "" {
				return errors.New("existing edit target must be a regular file with an expected hash")
			}
			current, err := os.ReadFile(target)
			if err != nil || HashContent(current) != edit.ExpectedSHA256 {
				return errors.New("edit target changed after context construction")
			}
		} else if !errors.Is(statErr, os.ErrNotExist) || edit.ExpectedSHA256 != "" {
			return errors.New("new edit target has invalid precondition")
		}
		temporary, err := os.CreateTemp(parent, ".maintainer-edit-*")
		if err != nil {
			return err
		}
		temporaryName := temporary.Name()
		if _, err = temporary.WriteString(edit.Content); err == nil {
			err = temporary.Sync()
		}
		if closeErr := temporary.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(temporaryName)
			return err
		}
		if err := os.Chmod(temporaryName, 0o600); err != nil {
			os.Remove(temporaryName)
			return err
		}
		if err := os.Rename(temporaryName, target); err != nil {
			os.Remove(temporaryName)
			return err
		}
	}
	return nil
}
