package config

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestBootstrapControlledDeploymentCannotChangeThroughAPI(t *testing.T) {
	before := Default(".data")
	after := before
	after.Deployment.DataRoot = "/host/path/from/browser"
	after.Deployment.ListenAddress = "0.0.0.0:8080"
	got := strings.Join(ValidateChange(before, after), "\n")
	if !strings.Contains(got, "data_root") || !strings.Contains(got, "listen_address") {
		t.Fatalf("unsafe bootstrap changes were not rejected: %s", got)
	}
}

func TestDiffIsDeterministicAndUsesEscapedJSONPointers(t *testing.T) {
	before := json.RawMessage(`{"z":1,"nested":{"same":true,"a/b":"old"},"remove":2}`)
	after := json.RawMessage(`{"z":2,"nested":{"same":true,"a/b":"new"},"add":3}`)
	first, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("diff is nondeterministic:\n%s\n%s", first, second)
	}
	want := `[{"op":"add","path":"/add","value":3},{"op":"replace","path":"/nested/a~1b","value":"new"},{"op":"remove","path":"/remove"},{"op":"replace","path":"/z","value":2}]`
	if string(first) != want {
		t.Fatalf("diff = %s, want %s", first, want)
	}
}
