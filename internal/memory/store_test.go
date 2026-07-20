package memory

import (
	"context"
	"errors"
	"testing"
)

func TestCandidatesStayQuarantinedAndProjectScoped(t *testing.T) {
	store := NewFake()
	ctx := context.Background()
	alpha := ProjectScope{Owner: "owner", Repository: "alpha"}
	beta := ProjectScope{Owner: "owner", Repository: "beta"}
	record, err := store.PutCandidate(ctx, alpha, Record{ID: "record-one", Content: "verified build command", SecretScanPass: true})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusQuarantine || record.Verified {
		t.Fatalf("candidate promoted itself: %#v", record)
	}
	for _, scope := range []ProjectScope{alpha, beta} {
		items, err := store.Search(ctx, scope, "build", 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 0 {
			t.Fatalf("quarantine leaked into search for %#v", scope)
		}
	}
	if _, err := store.Promote(ctx, beta, record.ID, "reviewer"); !errors.Is(err, ErrScope) {
		t.Fatalf("cross-project promotion returned %v", err)
	}
}

func TestInvalidScopeIsRejectedBeforeLookup(t *testing.T) {
	store := NewFake()
	if _, err := store.Search(context.Background(), ProjectScope{Owner: "../owner", Repository: "repo"}, "x", 1); !errors.Is(err, ErrScope) {
		t.Fatalf("invalid scope returned %v", err)
	}
}
