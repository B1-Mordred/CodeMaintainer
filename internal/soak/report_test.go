package soak

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRunProducesDeterministicBoundedPassReport(t *testing.T) {
	report, err := Run(context.Background(), DefaultInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
	required := requiredCheckIDs()
	for _, check := range report.Checks {
		delete(required, check.ID)
		if check.Status != StatusPassed {
			t.Fatalf("%s status = %s", check.ID, check.Status)
		}
	}
	if len(required) != 0 {
		t.Fatalf("missing checks: %#v", required)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxReportBytes {
		t.Fatalf("report bytes = %d, want <= %d", len(encoded), maxReportBytes)
	}

	again, err := Run(context.Background(), DefaultInput())
	if err != nil {
		t.Fatal(err)
	}
	if report.ReportSHA256 != again.ReportSHA256 {
		t.Fatalf("report hash drifted: %s != %s", report.ReportSHA256, again.ReportSHA256)
	}
}

func TestRunRejectsUnboundedIterations(t *testing.T) {
	if _, err := Run(context.Background(), Input{Iterations: maxIterations + 1}); err == nil {
		t.Fatal("unbounded soak iterations were accepted")
	}
}

func TestReportValidationRejectsThresholdDrift(t *testing.T) {
	report, err := Run(context.Background(), DefaultInput())
	if err != nil {
		t.Fatal(err)
	}
	report.Checks[0].Threshold.Operator = "<="
	report.Checks[0].Threshold.Limit = -1
	if err := report.Validate(); err == nil {
		t.Fatal("report with impossible pass threshold validated")
	}
}
