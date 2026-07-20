package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// PatchOperation is one RFC 6902-style JSON Patch operation. Values are kept
// as ordinary JSON-compatible Go values so marshaling remains deterministic.
type PatchOperation struct {
	Operation string `json:"op"`
	Path      string `json:"path"`
	Value     any    `json:"value,omitempty"`
}

func Diff(before, after json.RawMessage) (json.RawMessage, error) {
	var left, right any
	if err := json.Unmarshal(before, &left); err != nil {
		return nil, fmt.Errorf("decode previous configuration: %w", err)
	}
	if err := json.Unmarshal(after, &right); err != nil {
		return nil, fmt.Errorf("decode proposed configuration: %w", err)
	}
	operations := make([]PatchOperation, 0)
	diffValue("", left, right, &operations)
	result, err := json.Marshal(operations)
	if err != nil {
		return nil, fmt.Errorf("encode configuration diff: %w", err)
	}
	return result, nil
}

func diffValue(path string, before, after any, operations *[]PatchOperation) {
	if reflect.DeepEqual(before, after) {
		return
	}
	left, leftMap := before.(map[string]any)
	right, rightMap := after.(map[string]any)
	if leftMap && rightMap {
		keys := make(map[string]struct{}, len(left)+len(right))
		for key := range left {
			keys[key] = struct{}{}
		}
		for key := range right {
			keys[key] = struct{}{}
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			next := path + "/" + escapePointer(key)
			leftValue, leftOK := left[key]
			rightValue, rightOK := right[key]
			switch {
			case leftOK && !rightOK:
				*operations = append(*operations, PatchOperation{Operation: "remove", Path: next})
			case !leftOK && rightOK:
				*operations = append(*operations, PatchOperation{Operation: "add", Path: next, Value: rightValue})
			default:
				diffValue(next, leftValue, rightValue, operations)
			}
		}
		return
	}
	if path == "" {
		path = "/"
	}
	*operations = append(*operations, PatchOperation{Operation: "replace", Path: path, Value: after})
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
