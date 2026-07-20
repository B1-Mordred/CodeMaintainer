package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type RegistryRepository interface {
	GetConfigScope(context.Context, ScopeRef) (ScopeState, error)
	ApplyConfigScope(context.Context, ApplyScopeRequest) (RegistryRevision, ScopeState, error)
	GetConfigRegistryRevision(context.Context, string) (RegistryRevision, error)
	ListConfigRegistryRevisions(context.Context, ScopeRef, int) ([]RegistryRevision, error)
	GetConfigScopeAtRevision(context.Context, string) (ScopeState, error)
	CreateConfigDraft(context.Context, CreateDraftRequest) (Draft, error)
	GetConfigDraft(context.Context, string) (Draft, error)
	ListConfigDrafts(context.Context, ScopeRef, int) ([]Draft, error)
	UpdateConfigDraft(context.Context, UpdateDraftRequest) (Draft, error)
	TransitionConfigDraft(context.Context, TransitionDraftRequest) (Draft, error)
	SaveConfigCheck(context.Context, CheckResult) (CheckResult, error)
	ListConfigChecks(context.Context, string, int) ([]CheckResult, error)
	SaveJobConfigSnapshot(context.Context, JobSnapshot) (JobSnapshot, error)
	GetJobConfigSnapshot(context.Context, string) (JobSnapshot, error)
}

type RegistryService struct {
	repository RegistryRepository
	registry   *Registry
}

func NewRegistryService(repository RegistryRepository, registry *Registry) (*RegistryService, error) {
	if repository == nil || registry == nil {
		return nil, errors.New("configuration registry repository and registry are required")
	}
	return &RegistryService{repository: repository, registry: registry}, nil
}

func (service *RegistryService) Descriptors() []Descriptor { return service.registry.Descriptors() }

type ValidationIssue struct {
	Key      string `json:"key,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type ValidationReport struct {
	Valid                    bool              `json:"valid"`
	Issues                   []ValidationIssue `json:"issues"`
	ApplyModes               []ApplyMode       `json:"apply_modes"`
	RequiresReauthentication bool              `json:"requires_reauthentication"`
	NormalizedEntries        []DraftEntry      `json:"-"`
}

type EffectiveConfiguration struct {
	SchemaVersion int                       `json:"schema_version"`
	Scopes        []ScopeRef                `json:"scopes"`
	Values        map[string]EffectiveValue `json:"values"`
}

func (service *RegistryService) Scope(ctx context.Context, scope ScopeRef) (ScopeState, error) {
	return service.repository.GetConfigScope(ctx, scope)
}

func (service *RegistryService) Draft(ctx context.Context, id string) (Draft, error) {
	return service.repository.GetConfigDraft(ctx, id)
}

func (service *RegistryService) Drafts(ctx context.Context, scope ScopeRef, limit int) ([]Draft, error) {
	return service.repository.ListConfigDrafts(ctx, scope, limit)
}

func (service *RegistryService) Checks(ctx context.Context, draftID string, limit int) ([]CheckResult, error) {
	return service.repository.ListConfigChecks(ctx, draftID, limit)
}

func (service *RegistryService) Revisions(ctx context.Context, scope ScopeRef, limit int) ([]RegistryRevision, error) {
	return service.repository.ListConfigRegistryRevisions(ctx, scope, limit)
}

func (service *RegistryService) DiscardDraft(ctx context.Context, request TransitionDraftRequest) (Draft, error) {
	if request.Target != DraftDiscarded {
		return Draft{}, errors.New("discard operation requires discarded target")
	}
	return service.repository.TransitionConfigDraft(ctx, request)
}

func (service *RegistryService) EnsureSystemScope(ctx context.Context, active System, sourceRevision string) (ScopeState, error) {
	scope := ScopeRef{Kind: ScopeSystem}
	state, err := service.repository.GetConfigScope(ctx, scope)
	if err != nil || state.Version != 0 {
		return state, err
	}
	values := SystemScopedValues(active, sourceRevision, 1)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changes := make([]ScopeChange, 0, len(keys))
	for _, key := range keys {
		changes = append(changes, ScopeChange{Key: key, Value: values[key][0].Value, Configured: true})
	}
	_, state, err = service.repository.ApplyConfigScope(ctx, ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 0, ActorID: "system-bootstrap", ActorRole: "administrator",
		Operation: "import", Reason: "Import the active Increment 1 configuration into the scoped registry.",
		Changes: changes,
	})
	return state, err
}

func (service *RegistryService) Effective(ctx context.Context, scopes []ScopeRef) (EffectiveConfiguration, error) {
	seen := make(map[ScopeKind]bool, len(scopes))
	orderedScopes := append([]ScopeRef(nil), scopes...)
	for _, scope := range orderedScopes {
		if err := scope.Validate(); err != nil || scope.Kind == ScopeBuiltIn || seen[scope.Kind] {
			return EffectiveConfiguration{}, errors.New("effective configuration requires at most one valid stored scope of each kind")
		}
		seen[scope.Kind] = true
	}
	sort.Slice(orderedScopes, func(left, right int) bool {
		leftRank, _ := orderedScopes[left].Kind.rank()
		rightRank, _ := orderedScopes[right].Kind.rank()
		return leftRank < rightRank
	})
	valuesByKey := make(map[string][]ScopedValue)
	for _, scope := range orderedScopes {
		state, err := service.repository.GetConfigScope(ctx, scope)
		if err != nil {
			return EffectiveConfiguration{}, err
		}
		for _, value := range state.Values {
			valuesByKey[value.Key] = append(valuesByKey[value.Key], ScopedValue{
				Key: value.Key, Scope: value.Scope, Value: value.Value, Configured: value.Configured,
				Secret: value.Secret, SourceRevision: value.RevisionID, Version: value.Version,
			})
		}
	}
	result := EffectiveConfiguration{SchemaVersion: RegistrySchemaVersion, Scopes: orderedScopes, Values: make(map[string]EffectiveValue)}
	for _, descriptor := range service.registry.Descriptors() {
		effective, err := service.registry.Resolve(descriptor.Key, valuesByKey[descriptor.Key])
		if err != nil {
			return EffectiveConfiguration{}, err
		}
		result.Values[descriptor.Key] = effective
	}
	return result, nil
}

func (service *RegistryService) ValidateEntries(scope ScopeRef, entries []DraftEntry) ValidationReport {
	report := ValidationReport{Issues: []ValidationIssue{}, ApplyModes: []ApplyMode{}, NormalizedEntries: make([]DraftEntry, 0, len(entries))}
	if err := scope.Validate(); err != nil || scope.Kind == ScopeBuiltIn {
		report.Issues = append(report.Issues, ValidationIssue{Code: "invalid_scope", Message: "a valid mutable configuration scope is required", Severity: "error"})
		return report
	}
	if len(entries) == 0 || len(entries) > 500 {
		report.Issues = append(report.Issues, ValidationIssue{Code: "invalid_entry_count", Message: "between 1 and 500 configuration entries are required", Severity: "error"})
		return report
	}
	seenKeys := make(map[string]bool, len(entries))
	seenModes := make(map[ApplyMode]bool)
	for _, entry := range entries {
		if seenKeys[entry.Key] {
			report.Issues = append(report.Issues, ValidationIssue{Key: entry.Key, Code: "duplicate_key", Message: "a draft may change a setting only once", Severity: "error"})
			continue
		}
		seenKeys[entry.Key] = true
		descriptor, ok := service.registry.descriptors[entry.Key]
		if !ok {
			report.Issues = append(report.Issues, ValidationIssue{Key: entry.Key, Code: "unregistered_key", Message: "the controller does not register this setting", Severity: "error"})
			continue
		}
		if !service.registry.PermitsScope(entry.Key, scope.Kind) || descriptor.BootstrapControlled {
			report.Issues = append(report.Issues, ValidationIssue{Key: entry.Key, Code: "scope_not_permitted", Message: "this setting cannot be changed at the selected scope", Severity: "error"})
			continue
		}
		normalized := entry
		if entry.Reset {
			if entry.Configured || len(entry.Value) != 0 {
				report.Issues = append(report.Issues, ValidationIssue{Key: entry.Key, Code: "invalid_reset", Message: "reset entries cannot also contain a configured value", Severity: "error"})
				continue
			}
			normalized.Secret = descriptor.Secret
		} else if descriptor.Secret {
			if !entry.Secret || !entry.Configured || len(entry.Value) != 0 {
				report.Issues = append(report.Issues, ValidationIssue{Key: entry.Key, Code: "invalid_secret", Message: "write-only settings accept only configured secret state through an authorized secret handler", Severity: "error"})
				continue
			}
			report.RequiresReauthentication = true
			normalized.Secret = true
		} else {
			if entry.Secret || !entry.Configured {
				report.Issues = append(report.Issues, ValidationIssue{Key: entry.Key, Code: "invalid_value", Message: "a typed configured value is required", Severity: "error"})
				continue
			}
			canonical, err := service.registry.ValidateValue(entry.Key, entry.Value)
			if err != nil {
				report.Issues = append(report.Issues, ValidationIssue{Key: entry.Key, Code: "schema_validation", Message: err.Error(), Severity: "error"})
				continue
			}
			normalized.Value = canonical
			normalized.Secret = false
		}
		report.NormalizedEntries = append(report.NormalizedEntries, normalized)
		if !seenModes[descriptor.Apply] {
			report.ApplyModes = append(report.ApplyModes, descriptor.Apply)
			seenModes[descriptor.Apply] = true
		}
	}
	sort.Slice(report.ApplyModes, func(left, right int) bool { return report.ApplyModes[left] < report.ApplyModes[right] })
	report.Valid = len(report.Issues) == 0 && len(report.NormalizedEntries) == len(entries)
	return report
}

func (service *RegistryService) CreateDraft(ctx context.Context, request CreateDraftRequest) (Draft, ValidationReport, error) {
	report := service.ValidateEntries(request.Scope, request.Entries)
	if !report.Valid {
		return Draft{}, report, nil
	}
	request.Entries = report.NormalizedEntries
	draft, err := service.repository.CreateConfigDraft(ctx, request)
	return draft, report, err
}

func (service *RegistryService) UpdateDraft(ctx context.Context, request UpdateDraftRequest) (Draft, ValidationReport, error) {
	draft, err := service.repository.GetConfigDraft(ctx, request.ID)
	if err != nil {
		return Draft{}, ValidationReport{}, err
	}
	report := service.ValidateEntries(draft.Scope, request.Entries)
	if !report.Valid {
		return Draft{}, report, nil
	}
	request.Entries = report.NormalizedEntries
	updated, err := service.repository.UpdateConfigDraft(ctx, request)
	return updated, report, err
}

func (service *RegistryService) ValidateDraft(ctx context.Context, draftID string) (ValidationReport, CheckResult, error) {
	draft, err := service.repository.GetConfigDraft(ctx, draftID)
	if err != nil {
		return ValidationReport{}, CheckResult{}, err
	}
	report := service.ValidateEntries(draft.Scope, draft.Entries)
	payload, _ := json.Marshal(report)
	status := "passed"
	if !report.Valid {
		status = "failed"
	}
	check, err := service.repository.SaveConfigCheck(ctx, CheckResult{
		DraftID: draft.ID, DraftVersion: draft.Version, Kind: "validation",
		Handler: "registry", Status: status, Result: payload,
	})
	return report, check, err
}

func (service *RegistryService) DryRunDraft(ctx context.Context, draftID string) ([]CheckResult, error) {
	draft, err := service.repository.GetConfigDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	report := service.ValidateEntries(draft.Scope, draft.Entries)
	results := make([]CheckResult, 0)
	validationPayload, _ := json.Marshal(report)
	validationStatus := "passed"
	if !report.Valid {
		validationStatus = "failed"
	}
	validation, err := service.repository.SaveConfigCheck(ctx, CheckResult{
		DraftID: draft.ID, DraftVersion: draft.Version, Kind: "validation", Handler: "registry",
		Status: validationStatus, Result: validationPayload,
	})
	if err != nil {
		return nil, err
	}
	results = append(results, validation)
	if !report.Valid {
		return results, nil
	}
	handlers := make(map[string]bool)
	for _, entry := range report.NormalizedEntries {
		descriptor := service.registry.descriptors[entry.Key]
		if descriptor.DryRunHandler != "" {
			handlers[descriptor.DryRunHandler] = true
		}
		for _, prerequisite := range descriptor.Prerequisites {
			handlers["prerequisite:"+prerequisite] = true
		}
	}
	if len(handlers) == 0 {
		handlers["registry-only"] = true
	}
	ordered := make([]string, 0, len(handlers))
	for handler := range handlers {
		ordered = append(ordered, handler)
	}
	sort.Strings(ordered)
	for _, handler := range ordered {
		kind := "dry_run"
		if strings.HasPrefix(handler, "prerequisite:") {
			kind = "prerequisite"
		}
		result, saveErr := service.repository.SaveConfigCheck(ctx, CheckResult{
			DraftID: draft.ID, DraftVersion: draft.Version, Kind: kind, Handler: handler,
			Status: "passed", Result: json.RawMessage(`{"passed":true,"message":"No external side effect is required for this registered setting."}`),
		})
		if saveErr != nil {
			return nil, saveErr
		}
		results = append(results, result)
	}
	return results, nil
}

func (service *RegistryService) ReviewDraft(ctx context.Context, request TransitionDraftRequest) (Draft, ValidationReport, error) {
	if request.Target != DraftReviewed {
		return Draft{}, ValidationReport{}, errors.New("review operation requires reviewed target")
	}
	draft, err := service.repository.GetConfigDraft(ctx, request.ID)
	if err != nil {
		return Draft{}, ValidationReport{}, err
	}
	report := service.ValidateEntries(draft.Scope, draft.Entries)
	if !report.Valid {
		return Draft{}, report, nil
	}
	reviewed, err := service.repository.TransitionConfigDraft(ctx, request)
	return reviewed, report, err
}

func (service *RegistryService) ApplyDraft(ctx context.Context, draftID string, expectedVersion int64, actorID, actorRole, reason string, recentlyReauthenticated bool) (RegistryRevision, ScopeState, ValidationReport, error) {
	draft, err := service.repository.GetConfigDraft(ctx, draftID)
	if err != nil {
		return RegistryRevision{}, ScopeState{}, ValidationReport{}, err
	}
	if draft.State != DraftReviewed || draft.Version != expectedVersion {
		return RegistryRevision{}, ScopeState{}, ValidationReport{}, errors.New("reviewed draft version does not match")
	}
	report := service.ValidateEntries(draft.Scope, draft.Entries)
	if !report.Valid {
		return RegistryRevision{}, ScopeState{}, report, nil
	}
	if report.RequiresReauthentication && !recentlyReauthenticated {
		return RegistryRevision{}, ScopeState{}, report, errors.New("recent reauthentication is required for secret changes")
	}
	changes := draftEntriesToChanges(report.NormalizedEntries)
	revision, state, err := service.repository.ApplyConfigScope(ctx, ApplyScopeRequest{
		Scope: draft.Scope, ExpectedVersion: draft.BaseScopeVersion,
		ActorID: actorID, ActorRole: actorRole, Operation: "apply", Reason: reason,
		DraftID: draft.ID, DraftVersion: draft.Version, Changes: changes,
	})
	return revision, state, report, err
}

func (service *RegistryService) Rollback(ctx context.Context, revisionID string, expectedVersion int64, actorID, actorRole, reason string, recentlyReauthenticated bool) (RegistryRevision, ScopeState, error) {
	target, err := service.repository.GetConfigScopeAtRevision(ctx, revisionID)
	if err != nil {
		return RegistryRevision{}, ScopeState{}, err
	}
	current, err := service.repository.GetConfigScope(ctx, target.Scope)
	if err != nil {
		return RegistryRevision{}, ScopeState{}, err
	}
	if current.Version != expectedVersion {
		return RegistryRevision{}, ScopeState{}, errors.New("configuration scope version is stale")
	}
	targetByKey := make(map[string]StoredValue, len(target.Values))
	currentByKey := make(map[string]StoredValue, len(current.Values))
	for _, value := range target.Values {
		targetByKey[value.Key] = value
	}
	for _, value := range current.Values {
		currentByKey[value.Key] = value
	}
	keys := make(map[string]bool)
	for key := range targetByKey {
		keys[key] = true
	}
	for key := range currentByKey {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	changes := make([]ScopeChange, 0, len(ordered))
	for _, key := range ordered {
		targetValue, targetExists := targetByKey[key]
		currentValue, currentExists := currentByKey[key]
		descriptor, registered := service.registry.descriptors[key]
		if !registered {
			return RegistryRevision{}, ScopeState{}, fmt.Errorf("rollback contains unregistered key %q", key)
		}
		if descriptor.Secret {
			if targetExists && !currentExists {
				return RegistryRevision{}, ScopeState{}, errors.New("rollback requires re-entry of a write-only secret")
			}
			if !targetExists && currentExists {
				if !recentlyReauthenticated {
					return RegistryRevision{}, ScopeState{}, errors.New("recent reauthentication is required to clear a secret")
				}
				changes = append(changes, ScopeChange{Key: key, Secret: true, Configured: false})
			}
			continue
		}
		if targetExists {
			if !currentExists || string(targetValue.Value) != string(currentValue.Value) {
				changes = append(changes, ScopeChange{Key: key, Value: targetValue.Value, Configured: true})
			}
		} else if currentExists {
			changes = append(changes, ScopeChange{Key: key, Configured: false})
		}
	}
	if len(changes) == 0 {
		return RegistryRevision{}, ScopeState{}, errors.New("configuration already matches the selected revision")
	}
	return service.repository.ApplyConfigScope(ctx, ApplyScopeRequest{
		Scope: target.Scope, ExpectedVersion: expectedVersion, ActorID: actorID, ActorRole: actorRole,
		Operation: "rollback", Reason: reason, RollbackOf: revisionID, Changes: changes,
	})
}

func draftEntriesToChanges(entries []DraftEntry) []ScopeChange {
	changes := make([]ScopeChange, 0, len(entries))
	for _, entry := range entries {
		changes = append(changes, ScopeChange{Key: entry.Key, Value: entry.Value, Configured: !entry.Reset && entry.Configured, Secret: entry.Secret})
	}
	return changes
}

func (service *RegistryService) SnapshotJob(ctx context.Context, jobID string, scopes []ScopeRef) (JobSnapshot, error) {
	effective, err := service.Effective(ctx, scopes)
	if err != nil {
		return JobSnapshot{}, err
	}
	values := make(map[string][]ScopedValue)
	for key, value := range effective.Values {
		for _, contribution := range value.Contributions {
			if contribution.Scope.Kind == ScopeBuiltIn || !contribution.Configured {
				continue
			}
			values[key] = append(values[key], ScopedValue{
				Key: key, Scope: contribution.Scope, Value: contribution.Value, Configured: true,
				Secret: value.Secret, SourceRevision: contribution.SourceRevision, Version: contribution.Version,
			})
		}
	}
	snapshot, err := service.registry.Snapshot(values)
	if err != nil {
		return JobSnapshot{}, err
	}
	document, err := json.Marshal(snapshot)
	if err != nil {
		return JobSnapshot{}, fmt.Errorf("encode job configuration snapshot: %w", err)
	}
	return service.repository.SaveJobConfigSnapshot(ctx, JobSnapshot{
		JobID: jobID, SchemaVersion: snapshot.SchemaVersion, RegistryHash: snapshot.RegistryHash,
		SHA256: snapshot.SHA256, Document: document,
	})
}
