package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

type syncStoreFake struct {
	record    Record
	operation IndexOperation
	claimed   bool
	completed bool
	failed    bool
}

func (f *syncStoreFake) GetMemory(context.Context, ProjectScope, string) (Record, error) {
	return f.record, nil
}

func (f *syncStoreFake) ClaimMemoryIndexOperation(context.Context, string, time.Duration) (IndexOperation, error) {
	if f.claimed {
		return IndexOperation{}, ErrNoIndexOperation
	}
	f.claimed = true
	f.operation.Attempts++
	return f.operation, nil
}

func (f *syncStoreFake) CompleteMemoryIndexOperation(context.Context, string, string) error {
	f.completed = true
	return nil
}

func (f *syncStoreFake) FailMemoryIndexOperation(context.Context, string, string, string, time.Duration) error {
	f.failed = true
	return nil
}

func (f *syncStoreFake) EnqueueProjectMemoryRebuild(context.Context, ProjectScope, string) (int, error) {
	return 0, nil
}

func TestSynchronizerResolvesAuthoritativeRecordBeforeIndexing(t *testing.T) {
	scope := ProjectScope{Owner: "owner", Repository: "repo"}
	record := Record{
		ID: "memory_0123456789abcdef0123456789abcdef", Scope: scope, Namespace: scope.Namespace(),
		Content: "verified", Status: StatusCanonical, Version: 2,
	}
	store := &syncStoreFake{record: record, operation: IndexOperation{
		ID: "operation", RecordID: record.ID, Scope: scope, Action: "upsert", RecordVersion: 2,
	}}
	index := NewFakeIndex()
	synchronizer, err := NewSynchronizer(store, index, "test-indexer", time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.ProcessOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.completed || store.failed {
		t.Fatalf("operation state: completed=%t failed=%t", store.completed, store.failed)
	}
	matches, err := index.Find(context.Background(), scope, "verified", 10)
	if err != nil || len(matches) != 1 || matches[0].RecordID != record.ID {
		t.Fatalf("record not indexed: %+v, %v", matches, err)
	}
}

func TestSynchronizerSkipsSupersededUpsert(t *testing.T) {
	scope := ProjectScope{Owner: "owner", Repository: "repo"}
	record := Record{ID: "memory_0123456789abcdef0123456789abcdef", Scope: scope, Namespace: scope.Namespace(), Status: StatusStale, Version: 3}
	store := &syncStoreFake{record: record, operation: IndexOperation{ID: "operation", RecordID: record.ID, Scope: scope, Action: "upsert", RecordVersion: 2}}
	index := NewFakeIndex()
	synchronizer, _ := NewSynchronizer(store, index, "test-indexer", time.Second, nil)
	if err := synchronizer.ProcessOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.completed {
		t.Fatal("superseded operation was not completed")
	}
	if _, err := index.Find(context.Background(), scope, "x", 10); err != nil && !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
