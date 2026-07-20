package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

type ImportValue struct {
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value,omitempty"`
	Configured bool            `json:"configured"`
	Secret     bool            `json:"secret"`
	Redacted   bool            `json:"redacted"`
}

type DeclarativeConfig struct {
	SchemaVersion int           `json:"schema_version"`
	RegistryHash  string        `json:"registry_hash"`
	Scope         ScopeRef      `json:"scope"`
	ScopeVersion  int64         `json:"scope_version"`
	RevisionID    string        `json:"revision_id"`
	Values        []ImportValue `json:"values"`
	DocumentHash  string        `json:"document_hash"`
}

type ImportPreview struct {
	Valid            bool              `json:"valid"`
	Mode             string            `json:"mode"`
	Scope            ScopeRef          `json:"scope"`
	BaseScopeVersion int64             `json:"base_scope_version"`
	Issues           []ValidationIssue `json:"issues"`
	Entries          []DraftEntry      `json:"entries"`
	PreservedUnknown []ImportValue     `json:"preserved_unknown"`
}

func (service *RegistryService) ExportScope(ctx context.Context, scope ScopeRef) (DeclarativeConfig, error) {
	state, err := service.repository.GetConfigScope(ctx, scope)
	if err != nil {
		return DeclarativeConfig{}, err
	}
	registrySnapshot, err := service.registry.Snapshot(nil)
	if err != nil {
		return DeclarativeConfig{}, err
	}
	document := DeclarativeConfig{
		SchemaVersion: RegistrySchemaVersion, RegistryHash: registrySnapshot.RegistryHash,
		Scope: state.Scope, ScopeVersion: state.Version, RevisionID: state.RevisionID,
		Values: make([]ImportValue, 0, len(state.Values)),
	}
	for _, value := range state.Values {
		descriptor, ok := service.registry.descriptors[value.Key]
		if !ok {
			return DeclarativeConfig{}, fmt.Errorf("stored scope contains unregistered key %q", value.Key)
		}
		if descriptor.Secret {
			document.Values = append(document.Values, ImportValue{Key: value.Key, Configured: value.Configured, Secret: true, Redacted: true})
			continue
		}
		if !descriptor.Exportable {
			continue
		}
		document.Values = append(document.Values, ImportValue{Key: value.Key, Value: append(json.RawMessage(nil), value.Value...), Configured: value.Configured})
	}
	sort.Slice(document.Values, func(left, right int) bool { return document.Values[left].Key < document.Values[right].Key })
	document.DocumentHash, err = declarativeConfigHash(document)
	if err != nil {
		return DeclarativeConfig{}, err
	}
	return document, nil
}

func declarativeConfigHash(document DeclarativeConfig) (string, error) {
	document.DocumentHash = ""
	payload, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("encode declarative configuration: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func ComputeDeclarativeConfigHash(document DeclarativeConfig) (string, error) {
	return declarativeConfigHash(document)
}

func (service *RegistryService) PreviewImport(document DeclarativeConfig, mode string, target ScopeRef, baseScopeVersion int64) ImportPreview {
	preview := ImportPreview{
		Mode: mode, Scope: target, BaseScopeVersion: baseScopeVersion,
		Issues: []ValidationIssue{}, Entries: []DraftEntry{}, PreservedUnknown: []ImportValue{},
	}
	if mode != "strict" && mode != "forward_compatible" {
		preview.Issues = append(preview.Issues, ValidationIssue{Code: "invalid_import_mode", Message: "import mode must be strict or forward_compatible", Severity: "error"})
	}
	if err := target.Validate(); err != nil || target.Kind == ScopeBuiltIn {
		preview.Issues = append(preview.Issues, ValidationIssue{Code: "invalid_scope", Message: "a valid mutable target scope is required", Severity: "error"})
	}
	if document.SchemaVersion != RegistrySchemaVersion {
		preview.Issues = append(preview.Issues, ValidationIssue{Code: "unsupported_schema", Message: "the declarative configuration schema version is unsupported", Severity: "error"})
	}
	if document.Scope != target {
		preview.Issues = append(preview.Issues, ValidationIssue{Code: "scope_mismatch", Message: "the import document is bound to a different scope", Severity: "error"})
	}
	wantHash, err := declarativeConfigHash(document)
	if err != nil || document.DocumentHash == "" || wantHash != document.DocumentHash {
		preview.Issues = append(preview.Issues, ValidationIssue{Code: "document_hash_mismatch", Message: "the declarative configuration hash is missing or invalid", Severity: "error"})
	}
	registrySnapshot, snapshotErr := service.registry.Snapshot(nil)
	if snapshotErr != nil || document.RegistryHash != registrySnapshot.RegistryHash {
		preview.Issues = append(preview.Issues, ValidationIssue{Code: "registry_version_mismatch", Message: "the document was exported by a different descriptor registry; every known value will still be validated", Severity: "warning"})
	}
	seen := make(map[string]bool, len(document.Values))
	for _, value := range document.Values {
		if seen[value.Key] {
			preview.Issues = append(preview.Issues, ValidationIssue{Key: value.Key, Code: "duplicate_key", Message: "the import contains a duplicate key", Severity: "error"})
			continue
		}
		seen[value.Key] = true
		descriptor, registered := service.registry.descriptors[value.Key]
		if !registered {
			validOpaqueValue := json.Valid(value.Value) || (len(value.Value) == 0 && value.Secret && value.Redacted)
			if mode == "forward_compatible" && validOpaqueValue {
				preserved := value
				preserved.Value = append(json.RawMessage(nil), value.Value...)
				preview.PreservedUnknown = append(preview.PreservedUnknown, preserved)
				preview.Issues = append(preview.Issues, ValidationIssue{Key: value.Key, Code: "unknown_preserved", Message: "the unknown key will be preserved with the import draft but never applied", Severity: "warning"})
			} else {
				preview.Issues = append(preview.Issues, ValidationIssue{Key: value.Key, Code: "unregistered_key", Message: "the controller does not register this imported key", Severity: "error"})
			}
			continue
		}
		if descriptor.Secret || value.Secret || value.Redacted {
			preview.Issues = append(preview.Issues, ValidationIssue{Key: value.Key, Code: "secret_not_imported", Message: "write-only secret state is informational and must be configured separately", Severity: "warning"})
			continue
		}
		if !descriptor.Importable {
			preview.Issues = append(preview.Issues, ValidationIssue{Key: value.Key, Code: "not_importable", Message: "this setting is controlled outside declarative import", Severity: "error"})
			continue
		}
		preview.Entries = append(preview.Entries, DraftEntry{Key: value.Key, Value: append(json.RawMessage(nil), value.Value...), Configured: value.Configured})
	}
	sort.Slice(preview.Entries, func(left, right int) bool { return preview.Entries[left].Key < preview.Entries[right].Key })
	sort.Slice(preview.PreservedUnknown, func(left, right int) bool {
		return preview.PreservedUnknown[left].Key < preview.PreservedUnknown[right].Key
	})
	if len(preview.Entries) == 0 {
		preview.Issues = append(preview.Issues, ValidationIssue{Code: "no_applicable_values", Message: "the import contains no registered importable values", Severity: "error"})
	} else {
		report := service.ValidateEntries(target, preview.Entries)
		preview.Entries = report.NormalizedEntries
		preview.Issues = append(preview.Issues, report.Issues...)
	}
	preview.Valid = true
	for _, issue := range preview.Issues {
		if issue.Severity == "error" {
			preview.Valid = false
			break
		}
	}
	return preview
}

func (service *RegistryService) ImportDraft(ctx context.Context, document DeclarativeConfig, mode string, target ScopeRef, baseScopeVersion int64, actorID, reason string) (Draft, ImportPreview, error) {
	preview := service.PreviewImport(document, mode, target, baseScopeVersion)
	if !preview.Valid {
		return Draft{}, preview, nil
	}
	draft, err := service.repository.CreateConfigDraft(ctx, CreateDraftRequest{
		Scope: target, Operation: "import", BaseScopeVersion: baseScopeVersion,
		AuthorID: actorID, Reason: reason, Entries: preview.Entries, UnknownEntries: preview.PreservedUnknown,
	})
	return draft, preview, err
}

func VerifyDeclarativeConfig(document DeclarativeConfig) error {
	want, err := declarativeConfigHash(document)
	if err != nil {
		return err
	}
	if document.DocumentHash == "" || document.DocumentHash != want {
		return errors.New("declarative configuration hash mismatch")
	}
	return nil
}
