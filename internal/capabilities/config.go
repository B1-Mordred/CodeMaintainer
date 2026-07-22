package capabilities

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxCapabilityConfigurationBytes = 64 << 10

var configurationKey = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){0,7}$`)

// NormalizeConfiguration validates a browser/CLI supplied pack configuration
// against the controller-owned manifest schema, fills registered defaults, and
// returns deterministic JSON. Unknown fields, arrays, duplicate keys, and
// values outside the closed field contract fail before persistence.
func NormalizeConfiguration(manifest Manifest, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > maxCapabilityConfigurationBytes {
		return nil, errors.New("capability configuration exceeds the size limit")
	}
	decoded, err := decodeUniqueJSON(raw)
	if err != nil {
		return nil, errors.New("capability configuration must be one valid JSON object")
	}
	input, ok := decoded.(map[string]any)
	if !ok {
		return nil, errors.New("capability configuration must be one JSON object")
	}
	fields := make(map[string]UIField, len(manifest.UISchema))
	values := make(map[string]any, len(manifest.UISchema))
	for _, field := range manifest.UISchema {
		if _, duplicate := fields[field.Key]; duplicate {
			return nil, errors.New("capability UI schema contains a duplicate field")
		}
		fields[field.Key] = field
		fallback, err := decodeUniqueJSON(field.Default)
		if err != nil {
			return nil, fmt.Errorf("capability field %s has an invalid default", field.Key)
		}
		value, err := validateConfigurationValue(field, fallback)
		if err != nil {
			return nil, fmt.Errorf("capability field %s default: %w", field.Key, err)
		}
		values[field.Key] = value
	}
	provided := map[string]any{}
	if err := flattenConfiguration("", input, fields, provided); err != nil {
		return nil, err
	}
	for key, candidate := range provided {
		value, err := validateConfigurationValue(fields[key], candidate)
		if err != nil {
			return nil, fmt.Errorf("capability field %s: %w", key, err)
		}
		values[key] = value
	}
	if err := validateConfigurationRelations(manifest.ID, values); err != nil {
		return nil, err
	}
	result := map[string]any{}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		setConfigurationValue(result, strings.Split(key, "."), values[key])
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxCapabilityConfigurationBytes {
		return nil, errors.New("normalized capability configuration is invalid")
	}
	return encoded, nil
}

func validateConfigurationRelations(packID string, values map[string]any) error {
	if packID == "sbom-fmea-security" {
		reference, _ := values["suppression.reference"].(string)
		reason, _ := values["suppression.reason"].(string)
		expires, _ := values["suppression.expires_at"].(string)
		if reference != "" && (strings.TrimSpace(reason) == "" || expires == "") {
			return errors.New("a selected security suppression requires a bounded reason and RFC3339 expiry")
		}
		if reference == "" && (reason != "" || expires != "") {
			return errors.New("security suppression reason and expiry require a suppression reference")
		}
	}
	return nil
}

func flattenConfiguration(prefix string, input map[string]any, fields map[string]UIField, output map[string]any) error {
	for key, value := range input {
		candidate := key
		if prefix != "" {
			candidate = prefix + "." + key
		}
		if _, exact := fields[candidate]; exact {
			if _, nested := value.(map[string]any); nested {
				return fmt.Errorf("capability field %s must be a scalar", candidate)
			}
			output[candidate] = value
			continue
		}
		hasChildren := false
		for fieldKey := range fields {
			if strings.HasPrefix(fieldKey, candidate+".") {
				hasChildren = true
				break
			}
		}
		nested, ok := value.(map[string]any)
		if !hasChildren || !ok {
			return fmt.Errorf("unknown capability configuration field %s", candidate)
		}
		if err := flattenConfiguration(candidate, nested, fields, output); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigurationValue(field UIField, value any) (any, error) {
	switch field.Kind {
	case "boolean":
		if _, ok := value.(bool); !ok {
			return nil, errors.New("must be a boolean")
		}
	case "enum":
		text, ok := value.(string)
		if !ok || !contains(field.Allowed, text) {
			return nil, errors.New("must be one registered option")
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return nil, errors.New("must be a number")
		}
		parsed, err := strconv.ParseFloat(string(number), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || (field.Minimum != nil && parsed < *field.Minimum) || (field.Maximum != nil && parsed > *field.Maximum) {
			return nil, errors.New("is outside the registered numeric bounds")
		}
	case "string":
		text, ok := value.(string)
		if !ok || (field.MaxLength > 0 && len(text) > field.MaxLength) {
			return nil, errors.New("must be a bounded string")
		}
		switch field.Format {
		case "", "text":
		case "identifier":
			if text != "" && !safeID.MatchString(text) {
				return nil, errors.New("must be an opaque identifier")
			}
		case "repository-reference":
			clean := filepath.ToSlash(filepath.Clean(text))
			if text != "" && (clean == "." || clean != text || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(text, "\\")) {
				return nil, errors.New("must be a safe repository-relative reference")
			}
		case "date-time":
			if text != "" {
				if _, err := time.Parse(time.RFC3339, text); err != nil {
					return nil, errors.New("must be an RFC3339 timestamp")
				}
			}
		default:
			return nil, errors.New("uses an unknown string format")
		}
	default:
		return nil, errors.New("uses an unknown field kind")
	}
	return value, nil
}

func setConfigurationValue(target map[string]any, parts []string, value any) {
	for len(parts) > 1 {
		next, ok := target[parts[0]].(map[string]any)
		if !ok {
			next = map[string]any{}
			target[parts[0]] = next
		}
		target = next
		parts = parts[1:]
	}
	target[parts[0]] = value
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func decodeUniqueJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeUniqueValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON input")
	}
	return value, nil
}

func decodeUniqueValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return token, nil
	}
	switch delim {
	case '{':
		result := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("object key is not a string")
			}
			if _, duplicate := result[key]; duplicate {
				return nil, errors.New("duplicate JSON object key")
			}
			value, err := decodeUniqueValue(decoder)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		if close, err := decoder.Token(); err != nil || close != json.Delim('}') {
			return nil, errors.New("unterminated JSON object")
		}
		return result, nil
	case '[':
		result := []any{}
		for decoder.More() {
			value, err := decodeUniqueValue(decoder)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		if close, err := decoder.Token(); err != nil || close != json.Delim(']') {
			return nil, errors.New("unterminated JSON array")
		}
		return result, nil
	default:
		return nil, errors.New("unexpected JSON delimiter")
	}
}
