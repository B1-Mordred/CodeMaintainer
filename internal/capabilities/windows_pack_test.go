package capabilities

import (
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker"
)

func TestWindowsPackIsChecksummedAuthorityNeutralAndSimulatorFirst(t *testing.T) {
	catalog, err := BuiltInCatalog()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := catalog.Get("windows-dotnet-labautomation", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	report := ValidateManifest(manifest)
	if !report.ChecksumValid || !report.AuthoritySafe || !report.Compatible {
		t.Fatalf("trust report = %#v", report)
	}
	operations := map[string]bool{}
	for _, operation := range manifest.OperationClasses {
		operations[operation] = true
	}
	for _, required := range []string{windowsworker.JobDotNet, windowsworker.JobServiceLifecycle, windowsworker.JobInstallerLifecycle, windowsworker.JobHamiltonDiscovery, windowsworker.JobVPNWorkflow, windowsworker.JobReleaseConsistency, windowsworker.JobInstallerEvidence, windowsworker.JobEquipmentSimulator, windowsworker.JobSigningRequest} {
		if !operations[required] {
			t.Fatalf("missing Windows operation %q", required)
		}
	}
	if manifest.RunnerProfileIDs[0] != "windows-simulator" || len(manifest.Rehearsals) < 3 {
		t.Fatalf("Windows simulator/rehearsals = %#v %#v", manifest.RunnerProfileIDs, manifest.Rehearsals)
	}
}
