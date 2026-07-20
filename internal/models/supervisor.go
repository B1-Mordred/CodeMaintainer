package models

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Runtime interface {
	Start(context.Context, Manifest, string) error
	Stop(context.Context) error
	Status(context.Context, Manifest) (Status, error)
	Smoke(context.Context, Manifest) (SmokeResult, Status, error)
}

type Supervisor struct {
	mu        sync.Mutex
	catalog   *Catalog
	modelRoot string
	runtime   Runtime
	loaded    string
}

func NewSupervisor(catalog *Catalog, modelRoot string, runtime Runtime) (*Supervisor, error) {
	if catalog == nil || len(catalog.profiles) == 0 || modelRoot == "" || runtime == nil {
		return nil, errors.New("catalog, model root, and runtime are required")
	}
	return &Supervisor{catalog: catalog, modelRoot: modelRoot, runtime: runtime}, nil
}

func (s *Supervisor) Profiles(context.Context) ([]Profile, error) { return s.catalog.Profiles(), nil }

func (s *Supervisor) Load(ctx context.Context, id string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, ok := s.catalog.Manifest(id)
	if !ok {
		return Status{}, errors.New("model profile is not allow-listed")
	}
	if s.loaded == id {
		return s.runtime.Status(ctx, manifest)
	}
	if s.loaded != "" {
		if err := s.runtime.Stop(ctx); err != nil {
			return Status{}, fmt.Errorf("stop previously loaded model: %w", err)
		}
		s.loaded = ""
	}
	path, err := VerifyInstalledModel(s.modelRoot, manifest)
	if err != nil {
		return Status{}, err
	}
	if err := s.runtime.Start(ctx, manifest, path); err != nil {
		return Status{}, err
	}
	s.loaded = id
	status, err := s.runtime.Status(ctx, manifest)
	if err != nil {
		_ = s.runtime.Stop(context.Background())
		s.loaded = ""
		return Status{}, err
	}
	return status, nil
}

func (s *Supervisor) Unload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded == "" {
		return nil
	}
	if err := s.runtime.Stop(ctx); err != nil {
		return err
	}
	s.loaded = ""
	return nil
}

func (s *Supervisor) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded == "" {
		return Status{State: "unloaded"}, nil
	}
	manifest, ok := s.catalog.Manifest(s.loaded)
	if !ok {
		return Status{}, errors.New("loaded model is no longer allow-listed")
	}
	return s.runtime.Status(ctx, manifest)
}

func (s *Supervisor) SmokeTest(ctx context.Context, id string) (SmokeResult, error) {
	if _, err := s.Load(ctx, id); err != nil {
		return SmokeResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, _ := s.catalog.Manifest(s.loaded)
	result, _, err := s.runtime.Smoke(ctx, manifest)
	return result, err
}

type LlamaRuntime struct {
	mu       sync.Mutex
	binary   string
	endpoint string
	client   *http.Client
	command  *exec.Cmd
	done     chan error
	logs     *boundedWriter
	last     Status
}

func NewLlamaRuntime(binary string) (*LlamaRuntime, error) {
	if binary == "" || !strings.HasPrefix(binary, "/") {
		return nil, errors.New("llama-server binary path must be absolute")
	}
	return &LlamaRuntime{
		binary: binary, endpoint: "http://127.0.0.1:8082",
		client: &http.Client{Timeout: 10 * time.Second}, logs: &boundedWriter{maximum: 1 << 20},
	}, nil
}

func (r *LlamaRuntime) Start(ctx context.Context, manifest Manifest, modelPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.command != nil {
		return errors.New("llama-server is already running")
	}
	args := []string{
		"--model", modelPath, "--host", "0.0.0.0", "--port", "8082",
		"--ctx-size", strconv.Itoa(manifest.Context), "--threads", strconv.Itoa(manifest.Threads),
		"--batch-size", strconv.Itoa(manifest.Batch), "--ubatch-size", strconv.Itoa(manifest.UBatch),
		"--metrics", "--no-webui",
	}
	if manifest.NUMA != "disabled" {
		args = append(args, "--numa", manifest.NUMA)
	}
	command := exec.Command(r.binary, args...)
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/tmp"}
	command.Stdout = r.logs
	command.Stderr = r.logs
	if err := command.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}
	r.command = command
	r.done = make(chan error, 1)
	go func() { r.done <- command.Wait() }()
	r.mu.Unlock()
	err := r.waitHealthy(ctx, 2*time.Minute)
	r.mu.Lock()
	if err != nil {
		_ = command.Process.Kill()
		<-r.done
		r.command = nil
		r.done = nil
		return err
	}
	r.last = statusForManifest(manifest)
	return nil
}

func (r *LlamaRuntime) waitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("llama-server health timeout")
		case <-ticker.C:
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint+"/health", nil)
			response, err := r.client.Do(request)
			if err == nil {
				io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

func (r *LlamaRuntime) Stop(ctx context.Context) error {
	r.mu.Lock()
	if r.command == nil {
		r.mu.Unlock()
		return nil
	}
	command, done := r.command, r.done
	r.mu.Unlock()
	if err := command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-ctx.Done():
		_ = command.Process.Kill()
		<-done
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		<-done
	case <-done:
	}
	r.mu.Lock()
	r.command = nil
	r.done = nil
	r.last = Status{}
	r.mu.Unlock()
	return nil
}

func (r *LlamaRuntime) Status(ctx context.Context, manifest Manifest) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.command == nil || r.command.Process == nil {
		return Status{State: "unloaded"}, nil
	}
	status := r.last
	if status.State == "" {
		status = statusForManifest(manifest)
	}
	if payload, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", r.command.Process.Pid)); err == nil {
		fields := strings.Fields(string(payload))
		if len(fields) > 1 {
			pages, _ := strconv.ParseInt(fields[1], 10, 64)
			status.MemoryBytes = pages * int64(os.Getpagesize())
		}
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint+"/health", nil)
	response, err := r.client.Do(request)
	if err != nil {
		status.State = "unhealthy"
		return status, nil
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		status.State = "unhealthy"
	}
	return status, nil
}

func (r *LlamaRuntime) Smoke(ctx context.Context, manifest Manifest) (SmokeResult, Status, error) {
	started := time.Now()
	requestPayload := map[string]any{
		"model": manifest.ID, "messages": []map[string]string{{"role": "user", "content": "Reply with exactly OK."}},
		"max_tokens": 8, "temperature": manifest.Sampling.Temperature, "top_p": manifest.Sampling.TopP,
		"seed": manifest.Sampling.Seed, "stream": false,
	}
	payload, _ := json.Marshal(requestPayload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return SmokeResult{}, Status{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return SmokeResult{}, Status{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 || response.StatusCode != http.StatusOK {
		return SmokeResult{}, Status{}, errors.New("bounded llama-server smoke test failed")
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Timings struct {
			Prompt float64 `json:"prompt_per_second"`
			Decode float64 `json:"predicted_per_second"`
		} `json:"timings"`
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Choices) != 1 || strings.TrimSpace(result.Choices[0].Message.Content) != "OK" {
		return SmokeResult{}, Status{}, errors.New("llama-server smoke response is malformed")
	}
	r.mu.Lock()
	r.last = statusForManifest(manifest)
	r.last.PromptTokensSecond = result.Timings.Prompt
	r.last.DecodeTokensSecond = result.Timings.Decode
	status := r.last
	r.mu.Unlock()
	return SmokeResult{ProfileID: manifest.ID, Duration: time.Since(started), Healthy: true}, status, nil
}

func statusForManifest(manifest Manifest) Status {
	return Status{State: "loaded", ProfileID: manifest.ID, ModelFamily: manifest.ModelFamily, Context: manifest.Context}
}

type boundedWriter struct {
	mu      sync.Mutex
	buffer  []byte
	maximum int
}

func (w *boundedWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, payload...)
	if len(w.buffer) > w.maximum {
		w.buffer = append([]byte(nil), w.buffer[len(w.buffer)-w.maximum:]...)
	}
	return len(payload), nil
}
