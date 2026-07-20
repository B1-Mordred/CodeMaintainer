package memory

import (
	"context"
	"testing"
)

func TestConfiguredIndexSmokeIsScopedSearchableAndCleanedUp(t *testing.T) {
	index := NewFakeIndex()
	scope := ProjectScope{Owner: "owner", Repository: "repo"}
	result, err := SmokeIndex(context.Background(), index, scope)
	if err != nil || result.Status != "passed" || !result.SearchSeen || result.Namespace != scope.Namespace() {
		t.Fatalf("smoke result = %#v, %v", result, err)
	}
	index.mu.Lock()
	remaining := len(index.records)
	index.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("smoke left %d derived records", remaining)
	}
}
