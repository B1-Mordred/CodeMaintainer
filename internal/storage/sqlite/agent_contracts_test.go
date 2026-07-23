package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/agents"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestAgentContractValidationsAreAppendOnlyAndListedNewestFirst(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{ProjectID: "project-one", Repository: "owner/repo", Task: "repair defect", ActorID: "operator-one"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := agents.NewValidationRecord(job.ID, "implementation_packet", agents.ContractTaskPacket, []byte(`{"schema_version":1}`), 1, true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAgentContractValidation(ctx, first); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	second, err := agents.NewValidationRecord(job.ID, "implementation_result", agents.ContractImplementationResult, []byte(`{"schema_version":1}`), 1, false, errors.New("missing edits"), "artifact-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAgentContractValidation(ctx, second); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListAgentContractValidations(ctx, job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Phase != "implementation_result" || items[0].Error != "missing edits" || items[0].Valid {
		t.Fatalf("unexpected validation history: %#v", items)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE agent_contract_validations SET valid=1"); err == nil {
		t.Fatal("agent contract validation ledger accepted an update")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM agent_contract_validations"); err == nil {
		t.Fatal("agent contract validation ledger accepted a delete")
	}
}
