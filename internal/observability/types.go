package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion = 1

	KindSpan   = "span"
	KindMetric = "metric"
	KindLog    = "log"

	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"

	SupportBundleReady = "ready"
)

const (
	maxAttributeBytes = 32 << 10
	maxManifestBytes  = 64 << 10
)

type Event struct {
	ID               string          `json:"id"`
	SchemaVersion    int             `json:"schema_version"`
	TraceID          string          `json:"trace_id"`
	SpanID           string          `json:"span_id"`
	CorrelationID    string          `json:"correlation_id"`
	Component        string          `json:"component"`
	Kind             string          `json:"kind"`
	Name             string          `json:"name"`
	Severity         string          `json:"severity"`
	Attributes       json.RawMessage `json:"attributes"`
	RedactionCount   int             `json:"redaction_count"`
	DurationMillis   int64           `json:"duration_millis"`
	QueueMillis      int64           `json:"queue_millis"`
	RetryCount       int             `json:"retry_count"`
	ResourceBytes    int64           `json:"resource_bytes"`
	ExternalExported bool            `json:"external_exported"`
	ExternalEndpoint string          `json:"external_endpoint"`
	ActorID          string          `json:"actor_id"`
	CreatedAt        time.Time       `json:"created_at"`
}

type SupportBundle struct {
	ID              string          `json:"id"`
	SchemaVersion   int             `json:"schema_version"`
	Status          string          `json:"status"`
	Reason          string          `json:"reason"`
	Sections        []string        `json:"sections"`
	RedactionPolicy string          `json:"redaction_policy"`
	Manifest        json.RawMessage `json:"manifest"`
	ManifestSHA256  string          `json:"manifest_sha256"`
	BundleSHA256    string          `json:"bundle_sha256"`
	Bytes           int64           `json:"bytes"`
	ActorID         string          `json:"actor_id"`
	CreatedAt       time.Time       `json:"created_at"`
}

type Status struct {
	SchemaVersion         int             `json:"schema_version"`
	LocalCollector        string          `json:"local_collector"`
	RetentionDays         int             `json:"retention_days"`
	SamplingRatio         float64         `json:"sampling_ratio"`
	ExternalOTLPEnabled   bool            `json:"external_otlp_enabled"`
	ExternalOTLPAllowlist []string        `json:"external_otlp_allowlist"`
	RedactionPolicy       string          `json:"redaction_policy"`
	RecentEvents          []Event         `json:"recent_events"`
	RecentSupportBundles  []SupportBundle `json:"recent_support_bundles"`
}

type RecordEventRequest struct {
	TraceID        string          `json:"trace_id"`
	SpanID         string          `json:"span_id"`
	CorrelationID  string          `json:"correlation_id"`
	Component      string          `json:"component"`
	Kind           string          `json:"kind"`
	Name           string          `json:"name"`
	Severity       string          `json:"severity"`
	Attributes     json.RawMessage `json:"attributes"`
	DurationMillis int64           `json:"duration_millis"`
	QueueMillis    int64           `json:"queue_millis"`
	RetryCount     int             `json:"retry_count"`
	ResourceBytes  int64           `json:"resource_bytes"`
}

type CreateSupportBundleRequest struct {
	Reason   string   `json:"reason"`
	Sections []string `json:"sections"`
}

type Store interface {
	RecordObservabilityEvent(context.Context, Event) (Event, error)
	ListObservabilityEvents(context.Context, string, int) ([]Event, error)
	RecordSupportBundle(context.Context, SupportBundle) (SupportBundle, error)
	ListSupportBundles(context.Context, int) ([]SupportBundle, error)
}

func NewEvent(request RecordEventRequest, actor string, now time.Time) (Event, error) {
	kind := strings.TrimSpace(request.Kind)
	if kind == "" {
		kind = KindSpan
	}
	severity := strings.TrimSpace(request.Severity)
	if severity == "" {
		severity = SeverityInfo
	}
	attributes, redactions, err := RedactAttributes(request.Attributes)
	if err != nil {
		return Event{}, err
	}
	event := Event{
		ID:               "obsevent_" + sha256Text(request.Component + "\n" + request.Name + "\n" + now.UTC().Format(time.RFC3339Nano))[:32],
		SchemaVersion:    SchemaVersion,
		TraceID:          safeText(request.TraceID, 128),
		SpanID:           safeText(request.SpanID, 128),
		CorrelationID:    safeText(request.CorrelationID, 128),
		Component:        safeText(request.Component, 128),
		Kind:             kind,
		Name:             safeText(request.Name, 256),
		Severity:         severity,
		Attributes:       attributes,
		RedactionCount:   redactions,
		DurationMillis:   request.DurationMillis,
		QueueMillis:      request.QueueMillis,
		RetryCount:       request.RetryCount,
		ResourceBytes:    request.ResourceBytes,
		ExternalExported: false,
		ExternalEndpoint: "",
		ActorID:          safeText(actor, 128),
		CreatedAt:        now.UTC(),
	}
	if event.TraceID == "" {
		event.TraceID = sha256Text(event.Component + "\n" + event.Name + "\n" + event.CreatedAt.Format(time.RFC3339Nano))[:32]
	}
	if event.SpanID == "" {
		event.SpanID = sha256Text(event.TraceID + "\nspan")[:16]
	}
	if event.CorrelationID == "" {
		event.CorrelationID = event.TraceID
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func NewSupportBundle(events []Event, request CreateSupportBundleRequest, actor string, now time.Time) (SupportBundle, error) {
	sections := normalizeList(request.Sections)
	if len(sections) == 0 {
		sections = []string{"system_status", "recent_telemetry", "configuration_summary", "support_manifest"}
	}
	reason := safeText(request.Reason, 4096)
	if reason == "" {
		reason = "operator requested redacted support bundle"
	}
	eventIDs := make([]string, 0, len(events))
	components := map[string]int{}
	redactions := 0
	for _, event := range events {
		eventIDs = append(eventIDs, event.ID)
		components[event.Component]++
		redactions += event.RedactionCount
	}
	manifest, _ := json.Marshal(map[string]any{
		"schema_version":         SchemaVersion,
		"created_at":             now.UTC().Format(time.RFC3339Nano),
		"sections":               sections,
		"redaction_policy":       RedactionPolicy(),
		"external_otlp_enabled":  false,
		"local_collector":        "controller-sqlite",
		"retention_days":         30,
		"sampling_ratio":         1.0,
		"recent_event_ids":       eventIDs,
		"component_counts":       components,
		"redactions":             redactions,
		"excluded":               []string{"secrets", "raw request bodies", "sensitive prompts", "hidden reasoning", "unrestricted source content", "remote provider payloads"},
		"operator_only_exports":  []string{"external OTLP endpoint activation", "large artifact archive transfer"},
		"diagnostic_limit_bytes": maxManifestBytes,
	})
	bundle := SupportBundle{
		ID:              "supportbundle_" + sha256Text(reason + "\n" + now.UTC().Format(time.RFC3339Nano))[:32],
		SchemaVersion:   SchemaVersion,
		Status:          SupportBundleReady,
		Reason:          reason,
		Sections:        sections,
		RedactionPolicy: RedactionPolicy(),
		Manifest:        json.RawMessage(manifest),
		ManifestSHA256:  sha256Text(string(manifest)),
		BundleSHA256:    sha256Text("support-bundle\n" + sha256Text(string(manifest))),
		Bytes:           int64(len(manifest)),
		ActorID:         safeText(actor, 128),
		CreatedAt:       now.UTC(),
	}
	if err := bundle.Validate(); err != nil {
		return SupportBundle{}, err
	}
	return bundle, nil
}

func (event *Event) Validate() error {
	if event.SchemaVersion == 0 {
		event.SchemaVersion = SchemaVersion
	}
	event.ID = strings.TrimSpace(event.ID)
	event.TraceID = safeText(event.TraceID, 128)
	event.SpanID = safeText(event.SpanID, 128)
	event.CorrelationID = safeText(event.CorrelationID, 128)
	event.Component = safeText(event.Component, 128)
	event.Kind = strings.TrimSpace(event.Kind)
	event.Name = safeText(event.Name, 256)
	event.Severity = strings.TrimSpace(event.Severity)
	event.ExternalEndpoint = strings.TrimSpace(event.ExternalEndpoint)
	event.ActorID = safeText(event.ActorID, 128)
	if event.SchemaVersion != SchemaVersion || event.ID == "" || event.TraceID == "" || event.SpanID == "" ||
		event.CorrelationID == "" || event.Component == "" || event.Name == "" || event.ActorID == "" {
		return errors.New("observability event identity is incomplete")
	}
	if !oneOf(event.Kind, KindSpan, KindMetric, KindLog) || !oneOf(event.Severity, SeverityInfo, SeverityWarn, SeverityError) {
		return errors.New("observability event kind or severity is invalid")
	}
	if event.DurationMillis < 0 || event.QueueMillis < 0 || event.RetryCount < 0 || event.ResourceBytes < 0 {
		return errors.New("observability event metrics are invalid")
	}
	if event.ExternalExported || event.ExternalEndpoint != "" {
		return errors.New("external OTLP export is disabled by default")
	}
	redacted, redactions, err := RedactAttributes(event.Attributes)
	if err != nil {
		return err
	}
	event.Attributes = redacted
	if redactions > event.RedactionCount {
		event.RedactionCount = redactions
	}
	return nil
}

func (bundle *SupportBundle) Validate() error {
	if bundle.SchemaVersion == 0 {
		bundle.SchemaVersion = SchemaVersion
	}
	bundle.ID = strings.TrimSpace(bundle.ID)
	bundle.Status = strings.TrimSpace(bundle.Status)
	bundle.Reason = safeText(bundle.Reason, 4096)
	bundle.Sections = normalizeList(bundle.Sections)
	bundle.RedactionPolicy = strings.TrimSpace(bundle.RedactionPolicy)
	bundle.ManifestSHA256 = strings.TrimSpace(bundle.ManifestSHA256)
	bundle.BundleSHA256 = strings.TrimSpace(bundle.BundleSHA256)
	bundle.ActorID = safeText(bundle.ActorID, 128)
	if bundle.SchemaVersion != SchemaVersion || bundle.ID == "" || bundle.Status != SupportBundleReady ||
		bundle.Reason == "" || len(bundle.Sections) == 0 || bundle.RedactionPolicy == "" ||
		bundle.ManifestSHA256 == "" || bundle.BundleSHA256 == "" || bundle.ActorID == "" {
		return errors.New("support bundle identity is incomplete")
	}
	if len(bundle.ManifestSHA256) != 64 || len(bundle.BundleSHA256) != 64 {
		return errors.New("support bundle hashes must be sha256")
	}
	if len(bundle.Manifest) == 0 || len(bundle.Manifest) > maxManifestBytes || !json.Valid(bundle.Manifest) || bundle.Bytes < 1 || bundle.Bytes > maxManifestBytes {
		return errors.New("support bundle manifest is invalid")
	}
	if sha256Text(string(bundle.Manifest)) != bundle.ManifestSHA256 {
		return errors.New("support bundle manifest hash mismatch")
	}
	return nil
}

func RedactAttributes(raw json.RawMessage) (json.RawMessage, int, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), 0, nil
	}
	if len(raw) > maxAttributeBytes {
		return nil, 0, errors.New("observability attributes exceed bounded size")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, 0, errors.New("observability attributes must be valid JSON")
	}
	redacted, count := redactValue(value, "")
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return nil, 0, err
	}
	if len(encoded) > maxAttributeBytes {
		return nil, 0, errors.New("observability attributes exceed bounded size after redaction")
	}
	return json.RawMessage(encoded), count, nil
}

func RedactionPolicy() string {
	return "redact secret/token/password/key/auth/cookie/header/prompt/request-body/source-content/hidden-reasoning fields before storage or export"
}

func redactValue(value any, keyPath string) (any, int) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		count := 0
		for key, child := range typed {
			path := key
			if keyPath != "" {
				path = keyPath + "." + key
			}
			if sensitiveKey(key) || sensitiveKey(path) {
				out[key] = "[REDACTED]"
				count++
				continue
			}
			next, childCount := redactValue(child, path)
			out[key] = next
			count += childCount
		}
		return out, count
	case []any:
		out := make([]any, len(typed))
		count := 0
		for index, child := range typed {
			next, childCount := redactValue(child, fmt.Sprintf("%s[%d]", keyPath, index))
			out[index] = next
			count += childCount
		}
		return out, count
	case string:
		if len(typed) > 2048 {
			return typed[:2048] + "...[TRUNCATED]", 1
		}
		return typed, 0
	default:
		return typed, 0
	}
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, pattern := range []string{"secret", "token", "password", "api_key", "apikey", "auth", "cookie", "header", "prompt", "request_body", "request.body", "body", "source_content", "source.content", "hidden_reasoning", "reasoning", "chain_of_thought"} {
		if strings.Contains(key, pattern) {
			return true
		}
	}
	return false
}

func normalizeList(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = safeText(item, 128)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func safeText(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return -1
		}
		return r
	}, value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
