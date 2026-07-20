package config

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestBuiltInRegistryInventoriesEverySystemField(t *testing.T) {
	active := Default(".data")
	registry, err := BuiltInRegistry(active)
	if err != nil {
		t.Fatal(err)
	}
	descriptors := registry.Descriptors()
	wantKeys := []string{
		"deployment.data_root", "deployment.listen_address", "deployment.profile",
		"notifications.local_inbox_enabled", "protected_paths.patterns", "qc.block_on",
		"qc.human_waiver_enabled", "qc.require_evidence_for_blocking",
		"qc.require_verification_method", "qc.waiver_rationale_required",
		"workflow.max_log_bytes", "workflow.max_review_cycles", "workflow.max_wall_seconds",
	}
	gotKeys := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		gotKeys = append(gotKeys, descriptor.Key)
		if descriptor.SchemaVersion != RegistrySchemaVersion || !json.Valid(descriptor.JSONSchema) {
			t.Errorf("%s has incomplete schema metadata", descriptor.Key)
		}
		if descriptor.UI.Label == "" || descriptor.UI.Help == "" || descriptor.UI.DocumentationLink == "" {
			t.Errorf("%s has incomplete UI metadata", descriptor.Key)
		}
		if descriptor.RequiredPermission != "administer" || descriptor.Migration == "" || descriptor.AuditRedaction == "" {
			t.Errorf("%s has incomplete control metadata", descriptor.Key)
		}
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("registered keys = %v, want %v", gotKeys, wantKeys)
	}
}

func TestRegistryResolvesAllSevenScopesDeterministically(t *testing.T) {
	registry, err := BuiltInRegistry(Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	values := []ScopedValue{
		storedInteger("workflow.max_review_cycles", ScopeSystem, "", 2, "system-1"),
		storedInteger("workflow.max_review_cycles", ScopePack, "php@1", 3, "pack-1"),
		storedInteger("workflow.max_review_cycles", ScopeProject, "project-1", 4, "project-1"),
		storedInteger("workflow.max_review_cycles", ScopeEnvironment, "runner-1", 5, "environment-1"),
		storedInteger("workflow.max_review_cycles", ScopeJobTemplate, "template-1", 6, "template-1"),
		storedInteger("workflow.max_review_cycles", ScopeJobOverride, "job-1", 7, "job-1"),
	}
	for seed := int64(0); seed < 25; seed++ {
		shuffled := append([]ScopedValue(nil), values...)
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(left, right int) {
			shuffled[left], shuffled[right] = shuffled[right], shuffled[left]
		})
		effective, resolveErr := registry.Resolve("workflow.max_review_cycles", shuffled)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if string(effective.Value) != "7" || effective.SourceScope.Kind != ScopeJobOverride || effective.SourceRevision != "job-1" {
			t.Fatalf("seed %d resolved %#v", seed, effective)
		}
		if len(effective.Contributions) != 7 {
			t.Fatalf("seed %d has %d contributions", seed, len(effective.Contributions))
		}
		for index, contribution := range effective.Contributions {
			if contribution.Effective != (index == 6) || contribution.HigherPriorityOverride != (index != 6) {
				t.Errorf("seed %d contribution %d has inconsistent inheritance flags: %#v", seed, index, contribution)
			}
		}
	}
}

func TestRegistryRejectsUnknownInvalidAndAmbiguousValues(t *testing.T) {
	registry, err := BuiltInRegistry(Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		key    string
		values []ScopedValue
		want   string
	}{
		{"unknown key", "workflow.not_registered", nil, "unregistered"},
		{"wrong key", "workflow.max_review_cycles", []ScopedValue{storedInteger("workflow.max_wall_seconds", ScopeSystem, "", 2, "r1")}, "received value"},
		{"invalid integer", "workflow.max_review_cycles", []ScopedValue{{Key: "workflow.max_review_cycles", Scope: ScopeRef{Kind: ScopeSystem}, Value: json.RawMessage(`2.5`), SourceRevision: "r1", Version: 1}}, "must be an integer"},
		{"illegal bootstrap scope", "deployment.profile", []ScopedValue{{Key: "deployment.profile", Scope: ScopeRef{Kind: ScopeSystem}, Value: json.RawMessage(`"production"`), SourceRevision: "r1", Version: 1}}, "not permitted"},
		{"duplicate layer", "workflow.max_review_cycles", []ScopedValue{storedInteger("workflow.max_review_cycles", ScopeProject, "one", 2, "r1"), storedInteger("workflow.max_review_cycles", ScopeProject, "two", 3, "r2")}, "multiple values"},
		{"missing revision", "workflow.max_review_cycles", []ScopedValue{{Key: "workflow.max_review_cycles", Scope: ScopeRef{Kind: ScopeSystem}, Value: json.RawMessage(`2`), Version: 1}}, "source revision"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, resolveErr := registry.Resolve(test.key, test.values)
			if resolveErr == nil || !strings.Contains(resolveErr.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", resolveErr, test.want)
			}
		})
	}
}

func TestSnapshotIsCompleteDeterministicAndRejectsUnknownKeys(t *testing.T) {
	active := Default(".data")
	registry, err := BuiltInRegistry(active)
	if err != nil {
		t.Fatal(err)
	}
	values := SystemScopedValues(active, "config-4", 4)
	first, err := registry.Snapshot(values)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Snapshot(values)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 == "" || first.RegistryHash == "" || first.SHA256 != second.SHA256 || first.RegistryHash != second.RegistryHash {
		t.Fatalf("snapshots are not deterministic: %#v %#v", first, second)
	}
	if len(first.Values) != 13 {
		t.Fatalf("snapshot has %d values, want 13", len(first.Values))
	}
	if got := string(first.Values["workflow.max_wall_seconds"].Value); got != "14400" {
		t.Fatalf("wall-time value = %s", got)
	}
	values["untrusted.command"] = []ScopedValue{}
	if _, err := registry.Snapshot(values); err == nil || !strings.Contains(err.Error(), "unregistered") {
		t.Fatalf("unknown snapshot key was accepted: %v", err)
	}
}

func TestSecretValuesAreNeverReturnedOrExportable(t *testing.T) {
	descriptor := baseDescriptor("provider.api_key", "API key", "Write-only provider credential.", "Providers", 1, ValueString, "unset", scopes(ScopeBuiltIn, ScopeSystem), ApplyLive, stringValidator(1, 128))
	descriptor.Secret = true
	descriptor.Exportable = false
	descriptor.AuditRedaction = "configured_state_only"
	registry, err := NewRegistry([]Descriptor{descriptor})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := registry.Resolve("provider.api_key", []ScopedValue{{
		Key: "provider.api_key", Scope: ScopeRef{Kind: ScopeSystem},
		SourceRevision: "secret-1", Version: 1, Secret: true, Configured: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(effective)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "super-secret") || len(effective.Value) != 0 || !effective.Configured {
		t.Fatalf("secret leaked or configured state missing: %s", encoded)
	}
}

func TestRegistryDefensivelyCopiesPublicDescriptors(t *testing.T) {
	registry, err := BuiltInRegistry(Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	descriptors := registry.Descriptors()
	descriptors[0].PermittedScopes[0] = ScopeJobOverride
	descriptors[0].Default[0] = 'x'
	descriptor, _ := registry.Descriptor("deployment.data_root")
	if descriptor.PermittedScopes[0] != ScopeBuiltIn || !json.Valid(descriptor.Default) {
		t.Fatalf("caller mutated registry descriptor: %#v", descriptor)
	}
}

func storedInteger(key string, scope ScopeKind, id string, value int, revision string) ScopedValue {
	encoded, _ := json.Marshal(value)
	return ScopedValue{Key: key, Scope: ScopeRef{Kind: scope, ID: id}, Value: encoded, SourceRevision: revision, Version: 1}
}
