package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/local-code-maintainer/appliance/internal/automation"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func TestSchedulesDispatchAtomicallyIntoTheSerialJobQueue(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	clock := time.Date(2026, 7, 20, 20, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	ctx := context.Background()
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{ID: "owner-repo", Provider: "local", Repository: "owner/repo", DefaultBranch: "main", LocalRemoteName: "fixture.git"}, "admin"); err != nil {
		t.Fatal(err)
	}
	schedule, err := store.SaveSchedule(ctx, automation.ScheduleRequest{
		ProjectID: "owner-repo", Name: "nightly audit", TaskType: "audit", Task: "Audit the repository and report supported maintenance findings.",
		IntervalSeconds: 3600, WindowStartMinute: 0, WindowEndMinute: 0, MaxWallSeconds: 1800, MaxTokens: 20000,
		Enabled: true, NextRunAt: clock.Add(-time.Minute),
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.DispatchDueSchedule(ctx, "controller-scheduler")
	if err != nil || run.Status != "enqueued" || run.JobID == "" {
		t.Fatalf("dispatch = %+v, %v", run, err)
	}
	job, err := store.GetJob(ctx, run.JobID)
	if err != nil || job.ProjectID != "owner-repo" || job.Repository != "owner/repo" || job.State != "queued" {
		t.Fatalf("scheduled job = %+v, %v", job, err)
	}
	updated, err := store.GetSchedule(ctx, schedule.ID)
	if err != nil || !updated.NextRunAt.After(clock) || updated.Version != schedule.Version+1 {
		t.Fatalf("schedule did not advance: %+v, %v", updated, err)
	}
	if _, err := store.DispatchDueSchedule(ctx, "controller-scheduler"); !errors.Is(err, automation.ErrNoDueSchedule) {
		t.Fatalf("duplicate dispatch was possible: %v", err)
	}
	runs, err := store.ListScheduleRuns(ctx, 10)
	if err != nil || len(runs) != 1 || runs[0].JobID != run.JobID {
		t.Fatalf("schedule history = %+v, %v", runs, err)
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM schedule_runs WHERE id = ?", run.ID); err == nil {
		t.Fatal("append-only schedule run was deleted")
	}
}

func TestScheduleMaintenanceWindowSkipsWithoutCreatingJob(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	clock := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	ctx := context.Background()
	store.UpsertProject(ctx, projects.UpsertRequest{ID: "owner-repo", Provider: "local", Repository: "owner/repo", DefaultBranch: "main", LocalRemoteName: "fixture.git"}, "admin")
	_, err = store.SaveSchedule(ctx, automation.ScheduleRequest{
		ProjectID: "owner-repo", Name: "windowed", TaskType: "maintenance", Task: "Perform bounded maintenance.",
		IntervalSeconds: 3600, WindowStartMinute: 60, WindowEndMinute: 120, MaxWallSeconds: 600, MaxTokens: 1000,
		Enabled: true, NextRunAt: clock.Add(-time.Minute),
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.DispatchDueSchedule(ctx, "scheduler")
	if err != nil || run.Status != "skipped" || run.JobID != "" {
		t.Fatalf("windowed run = %+v, %v", run, err)
	}
	jobs, err := store.ListJobs(ctx, 10, 0)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("skipped window created jobs: %+v, %v", jobs, err)
	}
}

func TestSkillProposalsRemainInertAfterAdministratorReview(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	proposal, err := store.CreateSkillProposal(ctx, automation.SkillProposalRequest{
		Name: "summarize-reports", Description: "Proposed report summarization instructions.",
		Content: "# Summarize reports\n\nRead controller-provided reports only.", ProposedBy: "hermes-service",
	})
	if err != nil || proposal.Status != "proposed" || proposal.Activated {
		t.Fatalf("proposal = %+v, %v", proposal, err)
	}
	proposal, err = store.ReviewSkillProposal(ctx, proposal.ID, automation.SkillReviewRequest{
		Decision: "approve", Rationale: "content reviewed but activation remains a separate unavailable action",
		ActorID: "admin", ExpectedVersion: proposal.Version,
	})
	if err != nil || proposal.Status != "approved_inert" || proposal.Activated {
		t.Fatalf("reviewed proposal became executable: %+v, %v", proposal, err)
	}
	if _, err := store.CreateSkillProposal(ctx, automation.SkillProposalRequest{
		Name: "unsafe", Description: "unsafe", Content: "password=do-not-store", ProposedBy: "hermes-service",
	}); err == nil {
		t.Fatal("secret-bearing skill proposal was accepted")
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE skill_proposals SET activated = 1 WHERE id = ?", proposal.ID); err == nil || !strings.Contains(err.Error(), "CHECK constraint") {
		t.Fatalf("database allowed automatic activation: %v", err)
	}
}

func TestAutomationApprovalRequestsAreAppendOnlyAndDoNotApprove(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{ProjectID: "project", Repository: "owner/repo", Task: "task", ActorID: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateApprovalRequest(ctx, job.ID, "publication_approval", "hermes-service", "operator should inspect the verified report")
	if err != nil || request.JobID != job.ID {
		t.Fatalf("approval request = %+v, %v", request, err)
	}
	approvals, err := store.ListApprovals(ctx, job.ID)
	if err != nil || len(approvals) != 0 {
		t.Fatalf("request created authority: %+v, %v", approvals, err)
	}
	requests, err := store.ListApprovalRequests(ctx, 10)
	if err != nil || len(requests) != 1 || requests[0].ID != request.ID {
		t.Fatalf("request history = %+v, %v", requests, err)
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM automation_requests WHERE id = ?", request.ID); err == nil {
		t.Fatal("append-only automation request was deleted")
	}
}
