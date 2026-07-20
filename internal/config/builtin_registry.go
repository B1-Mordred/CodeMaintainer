package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func BuiltInRegistry(active System) (*Registry, error) {
	defaults := Default(active.Deployment.DataRoot)
	descriptors := []Descriptor{
		bootstrapStringDescriptor("deployment.listen_address", "Listen address", "Controller listen address supplied during trusted bootstrap.", active.Deployment.ListenAddress, 10),
		bootstrapStringDescriptor("deployment.data_root", "Data root", "Private appliance data root supplied during trusted bootstrap.", active.Deployment.DataRoot, 20),
		bootstrapStringDescriptor("deployment.profile", "Deployment profile", "Trusted adapter profile selected before controller startup.", active.Deployment.Profile, 30),
		integerDescriptor("workflow.max_review_cycles", "Maximum review cycles", "Maximum supported implementation and independent-review repair cycles.", 10, defaults.Workflow.MaxReviewCycles, 1, 10, allMutableScopes(), ApplyNewJobs),
		integerDescriptor("workflow.max_wall_seconds", "Maximum job wall time", "Maximum elapsed time reserved for one maintenance job.", 20, defaults.Workflow.MaxWallSeconds, 60, 604800, allMutableScopes(), ApplyNewJobs),
		integerDescriptor("workflow.max_log_bytes", "Maximum captured log bytes", "Maximum bounded worker log bytes retained for one operation.", 30, defaults.Workflow.MaxLogBytes, 1024, 1<<30, scopes(ScopeBuiltIn, ScopeSystem, ScopePack, ScopeProject, ScopeEnvironment, ScopeJobTemplate), ApplyNewJobs),
		stringArrayDescriptor("qc.block_on", "Blocking finding severities", "Finding severities that prevent unattended progress.", 10, defaults.QC.BlockOn, []string{"blocker", "must_fix", "should_fix", "note"}, allMutableScopes(), ApplyNewJobs),
		booleanDescriptor("qc.require_evidence_for_blocking", "Require evidence for blocking findings", "Reject blocking findings that do not point to stored verification or source evidence.", 20, defaults.QC.RequireEvidenceForBlocking, allMutableScopes(), ApplyNewJobs),
		booleanDescriptor("qc.require_verification_method", "Require a verification method", "Require every accepted finding to state how its resolution is verified.", 30, defaults.QC.RequireVerificationMethod, allMutableScopes(), ApplyNewJobs),
		booleanDescriptor("qc.human_waiver_enabled", "Allow human waivers", "Permit an authorized, recently reauthenticated reviewer to record a reasoned waiver.", 40, defaults.QC.HumanWaiverEnabled, scopes(ScopeBuiltIn, ScopeSystem, ScopeProject), ApplyNewJobs),
		booleanDescriptor("qc.waiver_rationale_required", "Require waiver rationale", "Require a bounded audited rationale for every human waiver.", 50, defaults.QC.WaiverRationaleRequired, scopes(ScopeBuiltIn, ScopeSystem, ScopeProject), ApplyNewJobs),
		pathArrayDescriptor("protected_paths.patterns", "Protected path patterns", "Repository-relative patterns that require deterministic protection policy.", 10, defaults.Protected.Patterns, scopes(ScopeBuiltIn, ScopeSystem, ScopePack, ScopeProject), ApplyNewJobs),
		booleanDescriptor("notifications.local_inbox_enabled", "Local operator inbox", "Create durable local notifications for schedule and workflow attention events.", 10, defaults.Notifications.LocalInboxEnabled, scopes(ScopeBuiltIn, ScopeSystem), ApplyLive),
		booleanDescriptor("intelligence.indexing_enabled", "Enable incremental indexing", "Permit trusted Git snapshots to refresh the project-separated derived code index.", 10, true, scopes(ScopeBuiltIn, ScopeSystem, ScopeProject), ApplyLive),
		integerDescriptor("intelligence.index_retention_days", "Parsed-blob retention", "Retention window for verified project-isolated parsed-blob cache identities.", 20, 30, 1, 3650, scopes(ScopeBuiltIn, ScopeSystem, ScopeProject), ApplyLive),
		integerDescriptor("intelligence.cache_quota_bytes", "Project cache quota", "Maximum verified cache metadata and object bytes retained for one project trust boundary.", 25, 512<<20, 1<<20, int64(1)<<40, scopes(ScopeBuiltIn, ScopeSystem, ScopeProject), ApplyLive),
		integerDescriptor("intelligence.context_input_tokens", "Context input budget", "Maximum deterministic per-stage input budget before the reserved model-output capacity.", 30, 32768, 1024, 262144, allMutableScopes(), ApplyNewJobs),
		integerDescriptor("intelligence.context_output_reserve_tokens", "Context output reserve", "Capacity reserved for agent output and excluded from selectable repository context.", 40, 8192, 256, 131072, allMutableScopes(), ApplyNewJobs),
		booleanDescriptor("verification.clean_final_cache_required", "Require clean final verification caches", "Require the final publishable suite to run in a fresh worker cache environment.", 10, true, scopes(ScopeBuiltIn, ScopeSystem, ScopePack, ScopeProject, ScopeEnvironment, ScopeJobTemplate, ScopeJobOverride), ApplyNewJobs),
	}
	for index := range descriptors {
		if descriptors[index].Namespace == "intelligence" || descriptors[index].Namespace == "verification" {
			descriptors[index].UI.DocumentationLink = "/docs/intelligence.md#configuration"
		}
	}
	for index := range descriptors {
		if descriptors[index].Key == "qc.human_waiver_enabled" {
			descriptors[index].Dependencies = []Dependency{{
				Key: "qc.waiver_rationale_required", Operator: "equals", Value: json.RawMessage(`true`),
				WhenValue: json.RawMessage(`true`),
				Message:   "Human waivers require audited rationale policy to remain enabled.",
			}}
		}
		if descriptors[index].Key == "notifications.local_inbox_enabled" {
			descriptors[index].Prerequisites = []string{"durable-controller-storage"}
			descriptors[index].DryRunHandler = "local-inbox-readiness"
		}
	}
	return NewRegistry(descriptors)
}

func scopes(values ...ScopeKind) []ScopeKind { return values }

func allMutableScopes() []ScopeKind {
	return scopes(ScopeBuiltIn, ScopeSystem, ScopePack, ScopeProject, ScopeEnvironment, ScopeJobTemplate, ScopeJobOverride)
}

func baseDescriptor(key, label, help, group string, order int, kind ValueKind, defaultValue any, permitted []ScopeKind, apply ApplyMode, validator func(json.RawMessage) error) Descriptor {
	encodedDefault, err := json.Marshal(defaultValue)
	if err != nil {
		panic(err)
	}
	return Descriptor{
		Key: key, Namespace: strings.Split(key, ".")[0], SchemaVersion: RegistrySchemaVersion,
		ValueKind: kind, JSONSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema"}`),
		UI: UIMetadata{
			Label: label, Help: help, Group: group, Order: order, Widget: widgetFor(kind),
			DocumentationLink: "/docs/configuration.md#" + strings.ReplaceAll(key, ".", "-"),
		},
		PermittedScopes: permitted, Default: encodedDefault, Recommended: cloneRaw(encodedDefault),
		RequiredPermission: "administer", Apply: apply, Exportable: true, Importable: true,
		Migration: "identity-v1", AuditRedaction: "value", validator: validator,
	}
}

func widgetFor(kind ValueKind) string {
	switch kind {
	case ValueBoolean:
		return "checkbox"
	case ValueInteger:
		return "number"
	case ValueStringArray:
		return "string_list"
	default:
		return "text"
	}
}

func bootstrapStringDescriptor(key, label, help, value string, order int) Descriptor {
	descriptor := baseDescriptor(key, label, help, "Deployment", order, ValueString, value, scopes(ScopeBuiltIn), ApplyOperatorRestart, stringValidator(1, 4096))
	descriptor.BootstrapControlled = true
	descriptor.Exportable = false
	descriptor.Importable = false
	descriptor.Migration = "bootstrap-only"
	descriptor.UI.Advanced = true
	descriptor.UI.Warnings = []string{"This value is controlled by trusted startup configuration and cannot be changed through the API or browser."}
	return descriptor
}

func integerDescriptor(key, label, help string, order int, value, minimum, maximum any, permitted []ScopeKind, apply ApplyMode) Descriptor {
	minimumInteger := toInt64(minimum)
	maximumInteger := toInt64(maximum)
	descriptor := baseDescriptor(key, label, help, strings.ToUpper(strings.Split(key, ".")[0]), order, ValueInteger, value, permitted, apply, integerValidator(minimumInteger, maximumInteger))
	descriptor.JSONSchema = json.RawMessage(fmt.Sprintf(`{"type":"integer","minimum":%d,"maximum":%d}`, minimumInteger, maximumInteger))
	if strings.HasSuffix(key, "_seconds") {
		descriptor.UI.Units = "seconds"
	}
	if strings.HasSuffix(key, "_bytes") {
		descriptor.UI.Units = "bytes"
	}
	return descriptor
}

func toInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	default:
		panic("integer descriptor bound must be int or int64")
	}
}

func booleanDescriptor(key, label, help string, order int, value bool, permitted []ScopeKind, apply ApplyMode) Descriptor {
	descriptor := baseDescriptor(key, label, help, strings.ToUpper(strings.Split(key, ".")[0]), order, ValueBoolean, value, permitted, apply, booleanValidator)
	descriptor.JSONSchema = json.RawMessage(`{"type":"boolean"}`)
	return descriptor
}

func stringArrayDescriptor(key, label, help string, order int, value, allowed []string, permitted []ScopeKind, apply ApplyMode) Descriptor {
	descriptor := baseDescriptor(key, label, help, strings.ToUpper(strings.Split(key, ".")[0]), order, ValueStringArray, value, permitted, apply, enumArrayValidator(allowed))
	allowedJSON, _ := json.Marshal(allowed)
	descriptor.JSONSchema = json.RawMessage(fmt.Sprintf(`{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","enum":%s}}`, allowedJSON))
	return descriptor
}

func pathArrayDescriptor(key, label, help string, order int, value []string, permitted []ScopeKind, apply ApplyMode) Descriptor {
	descriptor := baseDescriptor(key, label, help, "Protected paths", order, ValueStringArray, value, permitted, apply, boundedStringArrayValidator(1, 256, 1024))
	descriptor.JSONSchema = json.RawMessage(`{"type":"array","minItems":1,"maxItems":256,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":1024}}`)
	descriptor.UI.Widget = "path_pattern_list"
	descriptor.UI.Warnings = []string{"Patterns are trusted policy data and apply only to new jobs."}
	return descriptor
}

func booleanValidator(value json.RawMessage) error {
	var decoded bool
	if err := strictDecode(value, &decoded); err != nil {
		return errors.New("must be a boolean")
	}
	return nil
}

func integerValidator(minimum, maximum int64) func(json.RawMessage) error {
	return func(value json.RawMessage) error {
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		var decoded json.Number
		if err := decoder.Decode(&decoded); err != nil {
			return errors.New("must be an integer")
		}
		integer, err := decoded.Int64()
		if err != nil {
			return errors.New("must be an integer")
		}
		if integer < minimum || integer > maximum {
			return fmt.Errorf("must be between %d and %d", minimum, maximum)
		}
		return nil
	}
}

func stringValidator(minimum, maximum int) func(json.RawMessage) error {
	return func(value json.RawMessage) error {
		var decoded string
		if err := strictDecode(value, &decoded); err != nil {
			return errors.New("must be a string")
		}
		if len(decoded) < minimum || len(decoded) > maximum || strings.ContainsRune(decoded, 0) {
			return fmt.Errorf("must contain between %d and %d safe bytes", minimum, maximum)
		}
		return nil
	}
}

func boundedStringArrayValidator(minimum, maximum, maximumItemBytes int) func(json.RawMessage) error {
	return func(value json.RawMessage) error {
		var decoded []string
		if err := strictDecode(value, &decoded); err != nil {
			return errors.New("must be an array of strings")
		}
		if len(decoded) < minimum || len(decoded) > maximum {
			return fmt.Errorf("must contain between %d and %d items", minimum, maximum)
		}
		seen := make(map[string]bool, len(decoded))
		for _, item := range decoded {
			if item == "" || len(item) > maximumItemBytes || strings.ContainsRune(item, 0) {
				return errors.New("contains an empty, oversized, or unsafe item")
			}
			if seen[item] {
				return errors.New("must not contain duplicate items")
			}
			seen[item] = true
		}
		return nil
	}
}

func enumArrayValidator(allowed []string) func(json.RawMessage) error {
	allowedValues := make(map[string]bool, len(allowed))
	for _, value := range allowed {
		allowedValues[value] = true
	}
	return func(value json.RawMessage) error {
		if err := boundedStringArrayValidator(1, len(allowed), 128)(value); err != nil {
			return err
		}
		var decoded []string
		_ = json.Unmarshal(value, &decoded)
		for _, item := range decoded {
			if !allowedValues[item] {
				return fmt.Errorf("unsupported value %q", item)
			}
		}
		return nil
	}
}

func strictDecode(value json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("must contain one JSON value")
	}
	return nil
}

func SystemScopedValues(value System, revision string, version int64) map[string][]ScopedValue {
	result := make(map[string][]ScopedValue)
	add := func(key string, raw any) {
		encoded, err := json.Marshal(raw)
		if err != nil {
			panic(err)
		}
		result[key] = []ScopedValue{{
			Key: key, Scope: ScopeRef{Kind: ScopeSystem}, Value: encoded,
			Configured: true, SourceRevision: revision, Version: version,
		}}
	}
	add("workflow.max_review_cycles", value.Workflow.MaxReviewCycles)
	add("workflow.max_wall_seconds", value.Workflow.MaxWallSeconds)
	add("workflow.max_log_bytes", value.Workflow.MaxLogBytes)
	add("qc.block_on", value.QC.BlockOn)
	add("qc.require_evidence_for_blocking", value.QC.RequireEvidenceForBlocking)
	add("qc.require_verification_method", value.QC.RequireVerificationMethod)
	add("qc.human_waiver_enabled", value.QC.HumanWaiverEnabled)
	add("qc.waiver_rationale_required", value.QC.WaiverRationaleRequired)
	add("protected_paths.patterns", value.Protected.Patterns)
	add("notifications.local_inbox_enabled", value.Notifications.LocalInboxEnabled)
	return result
}

func ApplySystemChanges(current System, changes []ScopeChange) (System, error) {
	result := current
	defaults := Default(current.Deployment.DataRoot)
	for _, change := range changes {
		if change.Secret {
			return System{}, fmt.Errorf("legacy system projection cannot contain secret setting %q", change.Key)
		}
		value := change.Value
		switch change.Key {
		case "workflow.max_review_cycles":
			if !change.Configured {
				result.Workflow.MaxReviewCycles = defaults.Workflow.MaxReviewCycles
			} else if err := strictDecode(value, &result.Workflow.MaxReviewCycles); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "workflow.max_wall_seconds":
			if !change.Configured {
				result.Workflow.MaxWallSeconds = defaults.Workflow.MaxWallSeconds
			} else if err := strictDecode(value, &result.Workflow.MaxWallSeconds); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "workflow.max_log_bytes":
			if !change.Configured {
				result.Workflow.MaxLogBytes = defaults.Workflow.MaxLogBytes
			} else if err := strictDecode(value, &result.Workflow.MaxLogBytes); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "qc.block_on":
			if !change.Configured {
				result.QC.BlockOn = append([]string(nil), defaults.QC.BlockOn...)
			} else if err := strictDecode(value, &result.QC.BlockOn); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "qc.require_evidence_for_blocking":
			if !change.Configured {
				result.QC.RequireEvidenceForBlocking = defaults.QC.RequireEvidenceForBlocking
			} else if err := strictDecode(value, &result.QC.RequireEvidenceForBlocking); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "qc.require_verification_method":
			if !change.Configured {
				result.QC.RequireVerificationMethod = defaults.QC.RequireVerificationMethod
			} else if err := strictDecode(value, &result.QC.RequireVerificationMethod); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "qc.human_waiver_enabled":
			if !change.Configured {
				result.QC.HumanWaiverEnabled = defaults.QC.HumanWaiverEnabled
			} else if err := strictDecode(value, &result.QC.HumanWaiverEnabled); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "qc.waiver_rationale_required":
			if !change.Configured {
				result.QC.WaiverRationaleRequired = defaults.QC.WaiverRationaleRequired
			} else if err := strictDecode(value, &result.QC.WaiverRationaleRequired); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "protected_paths.patterns":
			if !change.Configured {
				result.Protected.Patterns = append([]string(nil), defaults.Protected.Patterns...)
			} else if err := strictDecode(value, &result.Protected.Patterns); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		case "notifications.local_inbox_enabled":
			if !change.Configured {
				result.Notifications.LocalInboxEnabled = defaults.Notifications.LocalInboxEnabled
			} else if err := strictDecode(value, &result.Notifications.LocalInboxEnabled); err != nil {
				return System{}, fmt.Errorf("decode %s: %w", change.Key, err)
			}
		default:
			continue
		}
	}
	if validationErrors := ValidateChange(current, result); len(validationErrors) != 0 {
		return System{}, errors.New(strings.Join(validationErrors, "; "))
	}
	return result, nil
}
