package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
)

type IndexMatch struct {
	RecordID string  `json:"record_id"`
	URI      string  `json:"uri"`
	Score    float64 `json:"score"`
	Abstract string  `json:"abstract,omitempty"`
}

// Index is a replaceable semantic index. It is never the authoritative memory
// lifecycle or provenance store; callers must resolve every returned RecordID
// through a project-scoped DurableStore lookup before using its content.
type Index interface {
	Health(context.Context) error
	Upsert(context.Context, Record) error
	Forget(context.Context, ProjectScope, string) error
	Find(context.Context, ProjectScope, string, int) ([]IndexMatch, error)
	Reindex(context.Context, ProjectScope, string) error
}

type FakeIndex struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewFakeIndex() *FakeIndex { return &FakeIndex{records: make(map[string]Record)} }

func (f *FakeIndex) Health(context.Context) error { return nil }

func (f *FakeIndex) Upsert(_ context.Context, record Record) error {
	if !record.Scope.Valid() || !ValidID(record.ID) || record.Status != StatusCanonical {
		return ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[record.Scope.Namespace()+record.ID] = record
	return nil
}

func (f *FakeIndex) Forget(_ context.Context, scope ProjectScope, id string) error {
	if !scope.Valid() || !ValidID(id) {
		return ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.records, scope.Namespace()+id)
	return nil
}

func (f *FakeIndex) Find(_ context.Context, scope ProjectScope, query string, limit int) ([]IndexMatch, error) {
	if !scope.Valid() || strings.TrimSpace(query) == "" || len(query) > 4096 {
		return nil, ErrInvalid
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	matches := make([]IndexMatch, 0)
	query = strings.ToLower(query)
	for _, record := range f.records {
		if record.Scope != scope || !strings.Contains(strings.ToLower(record.Content), query) {
			continue
		}
		matches = append(matches, IndexMatch{RecordID: record.ID, URI: indexRecordURI(scope, record.ID), Score: 1})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].RecordID < matches[j].RecordID })
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func (f *FakeIndex) Reindex(context.Context, ProjectScope, string) error { return nil }

func indexRecordURI(scope ProjectScope, id string) string {
	return scope.Namespace() + "records/" + id + ".md"
}
