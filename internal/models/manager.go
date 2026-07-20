package models

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Profile struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	ModelFamily  string `json:"model_family"`
	Context      int    `json:"context"`
	Quantization string `json:"quantization"`
}

type Status struct {
	State              string  `json:"state"`
	ProfileID          string  `json:"profile_id,omitempty"`
	ModelFamily        string  `json:"model_family,omitempty"`
	Context            int     `json:"context,omitempty"`
	MemoryBytes        int64   `json:"memory_bytes"`
	PromptTokensSecond float64 `json:"prompt_tokens_second"`
	DecodeTokensSecond float64 `json:"decode_tokens_second"`
}

type SmokeResult struct {
	ProfileID string        `json:"profile_id"`
	Duration  time.Duration `json:"duration"`
	Healthy   bool          `json:"healthy"`
}

type Manager interface {
	Profiles(context.Context) ([]Profile, error)
	Load(context.Context, string) (Status, error)
	Unload(context.Context) error
	Status(context.Context) (Status, error)
	SmokeTest(context.Context, string) (SmokeResult, error)
}

type Fake struct {
	mu       sync.Mutex
	profiles map[string]Profile
	loaded   string
}

func NewFake(profiles []Profile) *Fake {
	values := make(map[string]Profile, len(profiles))
	for _, profile := range profiles {
		values[profile.ID] = profile
	}
	return &Fake{profiles: values}
}

func (f *Fake) Profiles(context.Context) ([]Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]Profile, 0, len(f.profiles))
	for _, profile := range f.profiles {
		result = append(result, profile)
	}
	return result, nil
}

func (f *Fake) Load(_ context.Context, id string) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	profile, ok := f.profiles[id]
	if !ok {
		return Status{}, errors.New("model profile is not allow-listed")
	}
	f.loaded = id
	return statusFor(profile), nil
}

func (f *Fake) Unload(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded = ""
	return nil
}

func (f *Fake) Status(context.Context) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loaded == "" {
		return Status{State: "unloaded"}, nil
	}
	return statusFor(f.profiles[f.loaded]), nil
}

func (f *Fake) SmokeTest(_ context.Context, id string) (SmokeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.profiles[id]; !ok {
		return SmokeResult{}, errors.New("model profile is not allow-listed")
	}
	return SmokeResult{ProfileID: id, Duration: time.Millisecond, Healthy: true}, nil
}

func statusFor(profile Profile) Status {
	return Status{State: "loaded", ProfileID: profile.ID, ModelFamily: profile.ModelFamily, Context: profile.Context, PromptTokensSecond: 1, DecodeTokensSecond: 1}
}
