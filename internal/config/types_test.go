package config

import "testing"

func TestDefaultConfigurationIsValidAndSecure(t *testing.T) {
	value := Default(".data")
	if errors := Validate(value); len(errors) != 0 {
		t.Fatalf("default configuration is invalid: %v", errors)
	}
	if value.Deployment.ListenAddress != "127.0.0.1:8080" {
		t.Fatalf("unsafe default listen address: %s", value.Deployment.ListenAddress)
	}
	if !value.QC.RequireEvidenceForBlocking || !value.QC.RequireVerificationMethod {
		t.Fatal("default QC policy allows unsupported blocking findings")
	}
	if !value.QC.HumanWaiverEnabled || !value.QC.WaiverRationaleRequired {
		t.Fatal("default waiver policy is incomplete")
	}
}

func TestInvalidLimitsAreRejected(t *testing.T) {
	value := Default(".data")
	value.Workflow.MaxReviewCycles = 0
	value.Workflow.MaxWallSeconds = 1
	value.Workflow.MaxLogBytes = 1
	if got := len(Validate(value)); got != 3 {
		t.Fatalf("got %d validation errors, want 3", got)
	}
}
