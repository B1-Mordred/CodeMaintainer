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
)

//go:embed prompts/*.txt
var promptFiles embed.FS

type ModelClient struct {
	endpoint string
	client   *http.Client
}

func NewModelClient(endpoint string) (*ModelClient, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/v1") {
		return nil, errors.New("model endpoint must be a credential-free internal HTTP v1 endpoint")
	}
	return &ModelClient{endpoint: strings.TrimSuffix(endpoint, "/"), client: &http.Client{Timeout: 30 * time.Minute}}, nil
}

func (c *ModelClient) Complete(ctx context.Context, systemPrompt string, packet TaskPacket, responseContract ContractKind) ([]byte, error) {
	packetPayload, err := json.Marshal(packet)
	if err != nil {
		return nil, err
	}
	responseFormat, err := ResponseFormat(responseContract)
	if err != nil {
		return nil, err
	}
	requestPayload, err := json.Marshal(map[string]any{
		"model": "active", "stream": false, "temperature": 0.1, "max_tokens": 16384,
		"response_format": responseFormat,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": "UNTRUSTED_TASK_PACKET_JSON\n" + string(packetPayload) + "\nEND_UNTRUSTED_TASK_PACKET_JSON"},
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

func RunImplementation(ctx context.Context, client *ModelClient, packetPayload []byte, worktree string) ([]byte, error) {
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
	response, err := client.Complete(ctx, string(prompt), packet, ContractImplementationResult)
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

func RunQC(ctx context.Context, client *ModelClient, packetPayload []byte) ([]byte, error) {
	packet, err := DecodeTaskPacket(packetPayload, "qc")
	if err != nil {
		return nil, err
	}
	prompt, err := promptFiles.ReadFile("prompts/qc.txt")
	if err != nil {
		return nil, err
	}
	response, err := client.Complete(ctx, string(prompt), packet, ContractQCReport)
	if err != nil {
		return nil, err
	}
	report, err := DecodeQCReport(response, packet)
	if err != nil {
		return nil, err
	}
	return json.Marshal(report)
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
