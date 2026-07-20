package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

const RegistrySchemaVersion = 1

type ScopeKind string

const (
	ScopeBuiltIn     ScopeKind = "built_in"
	ScopeSystem      ScopeKind = "system"
	ScopePack        ScopeKind = "capability_pack"
	ScopeProject     ScopeKind = "project"
	ScopeEnvironment ScopeKind = "environment"
	ScopeJobTemplate ScopeKind = "job_template"
	ScopeJobOverride ScopeKind = "job_override"
)

var allScopeKinds = []ScopeKind{
	ScopeBuiltIn,
	ScopeSystem,
	ScopePack,
	ScopeProject,
	ScopeEnvironment,
	ScopeJobTemplate,
	ScopeJobOverride,
}

func (scope ScopeKind) rank() (int, bool) {
	for index, candidate := range allScopeKinds {
		if scope == candidate {
			return index, true
		}
	}
	return 0, false
}

type ApplyMode string

const (
	ApplyLive            ApplyMode = "live"
	ApplyNewJobs         ApplyMode = "new_jobs"
	ApplyServiceReload   ApplyMode = "service_reload"
	ApplyOperatorRestart ApplyMode = "operator_restart"
)

type ValueKind string

const (
	ValueBoolean     ValueKind = "boolean"
	ValueInteger     ValueKind = "integer"
	ValueString      ValueKind = "string"
	ValueStringArray ValueKind = "string_array"
)

type UIMetadata struct {
	Label             string   `json:"label"`
	Help              string   `json:"help"`
	Group             string   `json:"group"`
	Order             int      `json:"order"`
	Widget            string   `json:"widget"`
	Units             string   `json:"units,omitempty"`
	Examples          []string `json:"examples,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	DocumentationLink string   `json:"documentation_link"`
	Advanced          bool     `json:"advanced"`
}

type Dependency struct {
	Key      string          `json:"key"`
	Operator string          `json:"operator"`
	Value    json.RawMessage `json:"value,omitempty"`
	Message  string          `json:"message"`
}

type Descriptor struct {
	Key                 string          `json:"key"`
	Namespace           string          `json:"namespace"`
	SchemaVersion       int             `json:"schema_version"`
	ValueKind           ValueKind       `json:"value_kind"`
	JSONSchema          json.RawMessage `json:"json_schema"`
	UI                  UIMetadata      `json:"ui"`
	PermittedScopes     []ScopeKind     `json:"permitted_scopes"`
	Default             json.RawMessage `json:"default"`
	Recommended         json.RawMessage `json:"recommended,omitempty"`
	Secret              bool            `json:"secret"`
	RequiredPermission  string          `json:"required_permission"`
	Apply               ApplyMode       `json:"apply"`
	Dependencies        []Dependency    `json:"dependencies,omitempty"`
	Incompatibilities   []Dependency    `json:"incompatibilities,omitempty"`
	Prerequisites       []string        `json:"prerequisites,omitempty"`
	DryRunHandler       string          `json:"dry_run_handler,omitempty"`
	Exportable          bool            `json:"exportable"`
	Importable          bool            `json:"importable"`
	Migration           string          `json:"migration"`
	AuditRedaction      string          `json:"audit_redaction"`
	BootstrapControlled bool            `json:"bootstrap_controlled"`
	validator           func(json.RawMessage) error
}

type Registry struct {
	descriptors map[string]Descriptor
	ordered     []string
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

func NewRegistry(descriptors []Descriptor) (*Registry, error) {
	registry := &Registry{descriptors: make(map[string]Descriptor, len(descriptors))}
	for _, descriptor := range descriptors {
		if err := validateDescriptor(descriptor); err != nil {
			return nil, fmt.Errorf("descriptor %q: %w", descriptor.Key, err)
		}
		if _, exists := registry.descriptors[descriptor.Key]; exists {
			return nil, fmt.Errorf("duplicate descriptor %q", descriptor.Key)
		}
		descriptor = cloneDescriptor(descriptor)
		registry.descriptors[descriptor.Key] = descriptor
		registry.ordered = append(registry.ordered, descriptor.Key)
	}
	sort.Strings(registry.ordered)
	return registry, nil
}

func validateDescriptor(descriptor Descriptor) error {
	if !keyPattern.MatchString(descriptor.Key) {
		return errors.New("key must be a stable dotted lowercase namespace")
	}
	parts := strings.Split(descriptor.Key, ".")
	if descriptor.Namespace != parts[0] {
		return errors.New("namespace must match the first key segment")
	}
	if descriptor.SchemaVersion < 1 {
		return errors.New("schema_version must be positive")
	}
	if !json.Valid(descriptor.JSONSchema) || len(descriptor.JSONSchema) == 0 {
		return errors.New("json_schema must be valid JSON")
	}
	if descriptor.UI.Label == "" || descriptor.UI.Help == "" || descriptor.UI.Group == "" || descriptor.UI.Widget == "" || descriptor.UI.DocumentationLink == "" {
		return errors.New("complete UI label, help, group, widget, and documentation link are required")
	}
	if descriptor.RequiredPermission == "" {
		return errors.New("required_permission is required")
	}
	switch descriptor.Apply {
	case ApplyLive, ApplyNewJobs, ApplyServiceReload, ApplyOperatorRestart:
	default:
		return fmt.Errorf("invalid apply mode %q", descriptor.Apply)
	}
	if descriptor.Migration == "" || descriptor.AuditRedaction == "" {
		return errors.New("migration and audit_redaction behavior are required")
	}
	seenScopes := make(map[ScopeKind]bool, len(descriptor.PermittedScopes))
	for _, scope := range descriptor.PermittedScopes {
		if _, ok := scope.rank(); !ok {
			return fmt.Errorf("invalid scope %q", scope)
		}
		if seenScopes[scope] {
			return fmt.Errorf("duplicate scope %q", scope)
		}
		seenScopes[scope] = true
	}
	if !seenScopes[ScopeBuiltIn] {
		return errors.New("built_in scope is required")
	}
	if descriptor.validator == nil {
		return errors.New("trusted value validator is required")
	}
	if err := descriptor.validator(descriptor.Default); err != nil {
		return fmt.Errorf("invalid default: %w", err)
	}
	if len(descriptor.Recommended) != 0 {
		if err := descriptor.validator(descriptor.Recommended); err != nil {
			return fmt.Errorf("invalid recommended value: %w", err)
		}
	}
	if descriptor.Secret && (descriptor.Exportable || descriptor.AuditRedaction != "configured_state_only") {
		return errors.New("secret descriptors must be non-exportable and redact to configured state")
	}
	if descriptor.BootstrapControlled && len(descriptor.PermittedScopes) != 1 {
		return errors.New("bootstrap-controlled settings may only expose their built-in effective value")
	}
	return nil
}

func (registry *Registry) Descriptors() []Descriptor {
	result := make([]Descriptor, 0, len(registry.ordered))
	for _, key := range registry.ordered {
		descriptor := cloneDescriptor(registry.descriptors[key])
		descriptor.validator = nil
		result = append(result, descriptor)
	}
	return result
}

func (registry *Registry) Descriptor(key string) (Descriptor, bool) {
	descriptor, ok := registry.descriptors[key]
	if ok {
		descriptor = cloneDescriptor(descriptor)
		descriptor.validator = nil
	}
	return descriptor, ok
}

func (registry *Registry) ValidateValue(key string, value json.RawMessage) (json.RawMessage, error) {
	_, canonical, err := registry.validateValue(key, value)
	return canonical, err
}

func (registry *Registry) PermitsScope(key string, scope ScopeKind) bool {
	descriptor, ok := registry.descriptors[key]
	if !ok {
		return false
	}
	for _, candidate := range descriptor.PermittedScopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func (registry *Registry) validateValue(key string, value json.RawMessage) (Descriptor, json.RawMessage, error) {
	descriptor, ok := registry.descriptors[key]
	if !ok {
		return Descriptor{}, nil, fmt.Errorf("unregistered configuration key %q", key)
	}
	canonical, err := canonicalJSON(value)
	if err != nil {
		return Descriptor{}, nil, fmt.Errorf("%s: %w", key, err)
	}
	if err := descriptor.validator(canonical); err != nil {
		return Descriptor{}, nil, fmt.Errorf("%s: %w", key, err)
	}
	return descriptor, canonical, nil
}

func canonicalJSON(value json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, errors.New("value must be valid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("value must contain one JSON value")
	}
	result, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("canonicalize value: %w", err)
	}
	return result, nil
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	descriptor.PermittedScopes = append([]ScopeKind(nil), descriptor.PermittedScopes...)
	descriptor.Default = cloneRaw(descriptor.Default)
	descriptor.Recommended = cloneRaw(descriptor.Recommended)
	descriptor.JSONSchema = cloneRaw(descriptor.JSONSchema)
	descriptor.UI.Examples = append([]string(nil), descriptor.UI.Examples...)
	descriptor.UI.Warnings = append([]string(nil), descriptor.UI.Warnings...)
	descriptor.Prerequisites = append([]string(nil), descriptor.Prerequisites...)
	descriptor.Dependencies = cloneDependencies(descriptor.Dependencies)
	descriptor.Incompatibilities = cloneDependencies(descriptor.Incompatibilities)
	return descriptor
}

func cloneDependencies(values []Dependency) []Dependency {
	result := append([]Dependency(nil), values...)
	for index := range result {
		result[index].Value = cloneRaw(result[index].Value)
	}
	return result
}

type ScopeRef struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id,omitempty"`
}

func (scope ScopeRef) Validate() error {
	if _, ok := scope.Kind.rank(); !ok {
		return fmt.Errorf("invalid scope %q", scope.Kind)
	}
	if scope.Kind == ScopeBuiltIn || scope.Kind == ScopeSystem {
		if scope.ID != "" {
			return fmt.Errorf("scope %q must not have an id", scope.Kind)
		}
		return nil
	}
	if strings.TrimSpace(scope.ID) == "" || len(scope.ID) > 256 {
		return fmt.Errorf("scope %q requires a bounded id", scope.Kind)
	}
	return nil
}

type ScopedValue struct {
	Key            string          `json:"key"`
	Scope          ScopeRef        `json:"scope"`
	Value          json.RawMessage `json:"value,omitempty"`
	Configured     bool            `json:"configured"`
	Secret         bool            `json:"secret"`
	SourceRevision string          `json:"source_revision"`
	Version        int64           `json:"version"`
}

type Contribution struct {
	Scope                  ScopeRef        `json:"scope"`
	Value                  json.RawMessage `json:"value,omitempty"`
	Configured             bool            `json:"configured"`
	SourceRevision         string          `json:"source_revision"`
	Version                int64           `json:"version"`
	Effective              bool            `json:"effective"`
	HigherPriorityOverride bool            `json:"higher_priority_override"`
}

type EffectiveValue struct {
	Key            string          `json:"key"`
	Value          json.RawMessage `json:"value,omitempty"`
	Configured     bool            `json:"configured"`
	SourceScope    ScopeRef        `json:"source_scope"`
	SourceRevision string          `json:"source_revision"`
	Contributions  []Contribution  `json:"contributions"`
	Apply          ApplyMode       `json:"apply"`
	Secret         bool            `json:"secret"`
}

func (registry *Registry) Resolve(key string, values []ScopedValue) (EffectiveValue, error) {
	descriptor, canonicalDefault, err := registry.validateValue(key, registry.descriptors[key].Default)
	if err != nil {
		return EffectiveValue{}, err
	}
	all := make([]ScopedValue, 0, len(values)+1)
	all = append(all, ScopedValue{Key: key, Scope: ScopeRef{Kind: ScopeBuiltIn}, Value: canonicalDefault, Configured: true, SourceRevision: "registry-v1", Version: 1})
	seen := map[ScopeKind]bool{ScopeBuiltIn: true}
	permitted := make(map[ScopeKind]bool, len(descriptor.PermittedScopes))
	for _, scope := range descriptor.PermittedScopes {
		permitted[scope] = true
	}
	for _, candidate := range values {
		if candidate.Key != key {
			return EffectiveValue{}, fmt.Errorf("resolver for %q received value for %q", key, candidate.Key)
		}
		if err := candidate.Scope.Validate(); err != nil {
			return EffectiveValue{}, err
		}
		if candidate.Scope.Kind == ScopeBuiltIn {
			return EffectiveValue{}, errors.New("built_in value is owned by the registry")
		}
		if !permitted[candidate.Scope.Kind] {
			return EffectiveValue{}, fmt.Errorf("scope %q is not permitted for %q", candidate.Scope.Kind, key)
		}
		if seen[candidate.Scope.Kind] {
			return EffectiveValue{}, fmt.Errorf("multiple values for scope %q must be composed before resolution", candidate.Scope.Kind)
		}
		seen[candidate.Scope.Kind] = true
		if candidate.Version < 1 || candidate.SourceRevision == "" {
			return EffectiveValue{}, errors.New("stored values require a positive version and source revision")
		}
		if descriptor.Secret {
			if !candidate.Secret || len(candidate.Value) != 0 || !candidate.Configured {
				return EffectiveValue{}, fmt.Errorf("%s requires configured write-only state", key)
			}
		} else {
			if candidate.Secret {
				return EffectiveValue{}, fmt.Errorf("%s is not a secret setting", key)
			}
			_, canonical, validationErr := registry.validateValue(key, candidate.Value)
			if validationErr != nil {
				return EffectiveValue{}, validationErr
			}
			candidate.Value = canonical
		}
		candidate.Configured = true
		all = append(all, candidate)
	}
	sort.Slice(all, func(left, right int) bool {
		leftRank, _ := all[left].Scope.Kind.rank()
		rightRank, _ := all[right].Scope.Kind.rank()
		return leftRank < rightRank
	})
	winner := all[len(all)-1]
	contributions := make([]Contribution, 0, len(all))
	for index, candidate := range all {
		contribution := Contribution{
			Scope: candidate.Scope, Value: cloneRaw(candidate.Value), Configured: candidate.Configured,
			SourceRevision: candidate.SourceRevision, Version: candidate.Version,
			Effective: index == len(all)-1, HigherPriorityOverride: index != len(all)-1,
		}
		if descriptor.Secret {
			contribution.Value = nil
		}
		contributions = append(contributions, contribution)
	}
	result := EffectiveValue{
		Key: key, Value: cloneRaw(winner.Value), Configured: winner.Configured,
		SourceScope: winner.Scope, SourceRevision: winner.SourceRevision,
		Contributions: contributions, Apply: descriptor.Apply, Secret: descriptor.Secret,
	}
	if descriptor.Secret {
		result.Value = nil
	}
	return result, nil
}

type Snapshot struct {
	SchemaVersion int                       `json:"schema_version"`
	RegistryHash  string                    `json:"registry_hash"`
	Values        map[string]EffectiveValue `json:"values"`
	SHA256        string                    `json:"sha256"`
}

func (registry *Registry) Snapshot(values map[string][]ScopedValue) (Snapshot, error) {
	result := Snapshot{SchemaVersion: RegistrySchemaVersion, Values: make(map[string]EffectiveValue, len(registry.ordered))}
	for key := range values {
		if _, exists := registry.descriptors[key]; !exists {
			return Snapshot{}, fmt.Errorf("unregistered configuration key %q", key)
		}
	}
	for _, key := range registry.ordered {
		effective, err := registry.Resolve(key, values[key])
		if err != nil {
			return Snapshot{}, err
		}
		result.Values[key] = effective
	}
	descriptors, err := json.Marshal(registry.Descriptors())
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode registry descriptors: %w", err)
	}
	registryDigest := sha256.Sum256(descriptors)
	result.RegistryHash = hex.EncodeToString(registryDigest[:])
	payload, err := json.Marshal(struct {
		SchemaVersion int                       `json:"schema_version"`
		RegistryHash  string                    `json:"registry_hash"`
		Values        map[string]EffectiveValue `json:"values"`
	}{result.SchemaVersion, result.RegistryHash, result.Values})
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode configuration snapshot: %w", err)
	}
	digest := sha256.Sum256(payload)
	result.SHA256 = hex.EncodeToString(digest[:])
	return result, nil
}
