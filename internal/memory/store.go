package memory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProjectScope struct {
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
}

func (s ProjectScope) Valid() bool {
	return s.Owner != "" && s.Repository != "" && !strings.Contains(s.Owner, "/") && !strings.Contains(s.Repository, "/")
}

func (s ProjectScope) Namespace() string {
	return "viking://resources/projects/" + s.Owner + "/" + s.Repository + "/"
}

type Status string

const (
	StatusQuarantine Status = "quarantine"
	StatusCanonical  Status = "canonical"
	StatusStale      Status = "stale"
)

type Record struct {
	ID             string       `json:"id"`
	Scope          ProjectScope `json:"scope"`
	Namespace      string       `json:"namespace"`
	Content        string       `json:"content"`
	ContentHash    string       `json:"content_hash"`
	SourceURI      string       `json:"source_uri"`
	BaseCommit     string       `json:"base_commit"`
	AffectedPaths  []string     `json:"affected_paths"`
	Status         Status       `json:"status"`
	Verified       bool         `json:"verified"`
	SecretScanPass bool         `json:"secret_scan_pass"`
	CreatedAt      time.Time    `json:"created_at"`
}

type Store interface {
	PutCandidate(context.Context, ProjectScope, Record) (Record, error)
	Search(context.Context, ProjectScope, string, int) ([]Record, error)
	Promote(context.Context, ProjectScope, string, string) (Record, error)
	Invalidate(context.Context, ProjectScope, string, string) (Record, error)
	Delete(context.Context, ProjectScope, string, string) error
}

var ErrScope = errors.New("invalid or mismatched project scope")

type Fake struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewFake() *Fake { return &Fake{records: make(map[string]Record)} }

func (f *Fake) PutCandidate(_ context.Context, scope ProjectScope, record Record) (Record, error) {
	if !scope.Valid() || (record.Scope.Valid() && record.Scope != scope) || record.ID == "" {
		return Record{}, ErrScope
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	record.Scope = scope
	record.Namespace = scope.Namespace()
	record.Status = StatusQuarantine
	record.Verified = false
	record.CreatedAt = time.Now().UTC()
	f.records[record.ID] = record
	return record, nil
}

func (f *Fake) Search(_ context.Context, scope ProjectScope, query string, limit int) ([]Record, error) {
	if !scope.Valid() {
		return nil, ErrScope
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]Record, 0)
	query = strings.ToLower(query)
	for _, record := range f.records {
		if record.Scope == scope && record.Status == StatusCanonical && strings.Contains(strings.ToLower(record.Content), query) {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (f *Fake) Promote(_ context.Context, scope ProjectScope, id, actor string) (Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[id]
	if !ok {
		return Record{}, errors.New("memory record not found")
	}
	if !scope.Valid() || record.Scope != scope {
		return Record{}, ErrScope
	}
	if actor == "" || !record.Verified || !record.SecretScanPass {
		return Record{}, errors.New("memory record is not eligible for promotion")
	}
	record.Status = StatusCanonical
	f.records[id] = record
	return record, nil
}

func (f *Fake) Invalidate(_ context.Context, scope ProjectScope, id, actor string) (Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[id]
	if !ok {
		return Record{}, errors.New("memory record not found")
	}
	if !scope.Valid() || record.Scope != scope || actor == "" {
		return Record{}, ErrScope
	}
	record.Status = StatusStale
	f.records[id] = record
	return record, nil
}

func (f *Fake) Delete(_ context.Context, scope ProjectScope, id, actor string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[id]
	if !ok {
		return errors.New("memory record not found")
	}
	if !scope.Valid() || record.Scope != scope || actor == "" {
		return ErrScope
	}
	delete(f.records, id)
	return nil
}
