package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	MaxStreamChunks       = 4096
	MaxStreamChunkBytes   = 64 << 10
	MaxStreamContentBytes = 1 << 20
	MaxStreamToolCalls    = 32
)

type StreamReport struct {
	Family             string       `json:"family"`
	Terminal           bool         `json:"terminal"`
	Canceled           bool         `json:"canceled"`
	Truncated          bool         `json:"truncated"`
	ContentBytes       int          `json:"content_bytes"`
	ToolCallDeltas     int          `json:"tool_call_deltas"`
	PartialUsageEvents int          `json:"partial_usage_events"`
	Usage              StreamUsage  `json:"usage"`
	Events             []StreamStep `json:"events"`
}

type StreamStep struct {
	Type      string `json:"type"`
	TextBytes int    `json:"text_bytes,omitempty"`
	ToolKey   string `json:"tool_key,omitempty"`
	Usage     bool   `json:"usage,omitempty"`
}

type StreamUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

func ParseStreamEvents(family string, chunks [][]byte) (StreamReport, error) {
	if !validFamily(family) {
		return StreamReport{}, errors.New("provider stream family is invalid")
	}
	if len(chunks) == 0 || len(chunks) > MaxStreamChunks {
		return StreamReport{}, errors.New("provider stream chunk count is invalid")
	}
	report := StreamReport{Family: family, Events: []StreamStep{}}
	activeToolKey := ""
	toolKeys := map[string]struct{}{}
	for _, chunk := range chunks {
		if len(chunk) == 0 || len(chunk) > MaxStreamChunkBytes {
			return StreamReport{}, errors.New("provider stream chunk is empty or oversized")
		}
		records, err := streamRecords(chunk)
		if err != nil {
			return StreamReport{}, err
		}
		for _, record := range records {
			if record == "[DONE]" {
				report.Terminal = true
				report.Events = append(report.Events, StreamStep{Type: "done"})
				continue
			}
			var raw map[string]any
			decoder := json.NewDecoder(strings.NewReader(record))
			decoder.UseNumber()
			if err := decoder.Decode(&raw); err != nil {
				return StreamReport{}, errors.New("provider stream event must be JSON or [DONE]")
			}
			if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
				return StreamReport{}, errors.New("provider stream event has trailing data")
			}
			if eventCanceled(raw) {
				report.Canceled = true
				report.Terminal = true
				report.Events = append(report.Events, StreamStep{Type: "canceled"})
				continue
			}
			if eventTruncated(raw) {
				report.Truncated = true
			}
			textBytes := eventTextBytes(raw)
			if textBytes > 0 {
				report.ContentBytes += textBytes
				if report.ContentBytes > MaxStreamContentBytes {
					return StreamReport{}, errors.New("provider stream content exceeds bounded size")
				}
				report.Events = append(report.Events, StreamStep{Type: "content_delta", TextBytes: textBytes})
			}
			keys := eventToolKeys(raw)
			for _, key := range keys {
				if activeToolKey != "" && activeToolKey != key {
					return StreamReport{}, errors.New("provider stream interleaved tool-call deltas are not accepted")
				}
				activeToolKey = key
				toolKeys[key] = struct{}{}
				if len(toolKeys) > MaxStreamToolCalls {
					return StreamReport{}, errors.New("provider stream has too many tool-call deltas")
				}
				report.ToolCallDeltas++
				report.Events = append(report.Events, StreamStep{Type: "tool_call_delta", ToolKey: key})
			}
			if usage, ok := eventUsage(raw); ok {
				report.PartialUsageEvents++
				report.Usage = mergeUsage(report.Usage, usage)
				report.Events = append(report.Events, StreamStep{Type: "usage_delta", Usage: true})
			}
			if eventTerminal(raw) {
				report.Terminal = true
				report.Events = append(report.Events, StreamStep{Type: "done"})
			}
			if len(report.Events) > MaxStreamChunks {
				return StreamReport{}, errors.New("provider stream event count exceeds bounded size")
			}
		}
	}
	if report.Truncated {
		return report, errors.New("provider stream ended with truncation")
	}
	if !report.Terminal {
		return report, errors.New("provider stream ended before a terminal event")
	}
	return report, nil
}

func streamRecords(chunk []byte) ([]string, error) {
	var records []string
	for _, rawLine := range bytes.Split(chunk, []byte{'\n'}) {
		line := strings.TrimSpace(string(rawLine))
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "" {
			continue
		}
		if len(line) > MaxStreamChunkBytes {
			return nil, errors.New("provider stream line exceeds bounded size")
		}
		records = append(records, line)
	}
	if len(records) == 0 {
		return nil, errors.New("provider stream chunk has no data records")
	}
	return records, nil
}

func eventCanceled(raw map[string]any) bool {
	return hasStringValue(raw, "type", "cancel") || hasStringValue(raw, "stop_reason", "cancel") || hasStringValue(raw, "finish_reason", "cancel")
}

func eventTruncated(raw map[string]any) bool {
	return hasStringValue(raw, "finish_reason", "length") || hasStringValue(raw, "stop_reason", "max_tokens") || hasStringValue(raw, "finishReason", "MAX_TOKENS")
}

func eventTerminal(raw map[string]any) bool {
	return hasStringValue(raw, "type", "done") ||
		hasStringValue(raw, "type", "message_stop") ||
		hasStringValue(raw, "type", "response.completed") ||
		hasStringValue(raw, "stopReason", "end_turn") ||
		hasStringValue(raw, "finishReason", "STOP") ||
		hasStringValue(raw, "finish_reason", "stop")
}

func eventTextBytes(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		total := 0
		for key, child := range typed {
			lower := strings.ToLower(key)
			if lower == "content" || lower == "text" || lower == "delta" {
				if text, ok := child.(string); ok {
					total += len(text)
					continue
				}
			}
			total += eventTextBytes(child)
		}
		return total
	case []any:
		total := 0
		for _, child := range typed {
			total += eventTextBytes(child)
		}
		return total
	default:
		return 0
	}
}

func eventToolKeys(value any) []string {
	out := []string{}
	collectToolKeys(value, "", &out)
	return out
}

func collectToolKeys(value any, fallback string, out *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		key := fallback
		if id, ok := stringField(typed, "id"); ok {
			key = id
		} else if index, ok := numberField(typed, "index"); ok {
			key = fmt.Sprintf("index:%d", index)
		} else if index, ok := numberField(typed, "contentBlockIndex"); ok {
			key = fmt.Sprintf("content-block:%d", index)
		}
		if _, ok := typed["tool_calls"]; ok {
			collectToolKeys(typed["tool_calls"], key, out)
			return
		}
		if _, ok := typed["toolUse"]; ok {
			if key == "" {
				key = "tool-use"
			}
			*out = append(*out, key)
		}
		if function, ok := typed["function"].(map[string]any); ok {
			if _, hasArguments := function["arguments"]; hasArguments {
				if key == "" {
					key = "function"
				}
				*out = append(*out, key)
			}
		}
		if delta, ok := typed["delta"].(map[string]any); ok {
			if _, hasJSON := delta["partial_json"]; hasJSON {
				if key == "" {
					key = "content-block"
				}
				*out = append(*out, key)
			}
		}
		for _, child := range typed {
			collectToolKeys(child, key, out)
		}
	case []any:
		for index, child := range typed {
			next := fallback
			if next == "" {
				next = fmt.Sprintf("index:%d", index)
			}
			collectToolKeys(child, next, out)
		}
	}
}

func eventUsage(value any) (StreamUsage, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if usage, ok := usageFromMap(typed); ok {
			return usage, true
		}
		for _, child := range typed {
			if usage, ok := eventUsage(child); ok {
				return usage, true
			}
		}
	case []any:
		for _, child := range typed {
			if usage, ok := eventUsage(child); ok {
				return usage, true
			}
		}
	}
	return StreamUsage{}, false
}

func usageFromMap(value map[string]any) (StreamUsage, bool) {
	usage := StreamUsage{}
	seen := false
	for _, candidate := range []struct {
		keys   []string
		assign func(int)
	}{
		{[]string{"prompt_tokens", "input_tokens", "inputTokens", "promptTokenCount"}, func(v int) { usage.InputTokens = v }},
		{[]string{"completion_tokens", "output_tokens", "outputTokens", "candidatesTokenCount"}, func(v int) { usage.OutputTokens = v }},
		{[]string{"total_tokens", "totalTokens", "totalTokenCount"}, func(v int) { usage.TotalTokens = v }},
	} {
		for _, key := range candidate.keys {
			if number, ok := numberField(value, key); ok {
				candidate.assign(number)
				seen = true
				break
			}
		}
	}
	return usage, seen
}

func mergeUsage(current, next StreamUsage) StreamUsage {
	if next.InputTokens > 0 {
		current.InputTokens = next.InputTokens
	}
	if next.OutputTokens > 0 {
		current.OutputTokens = next.OutputTokens
	}
	if next.TotalTokens > 0 {
		current.TotalTokens = next.TotalTokens
	}
	return current
}

func hasStringValue(value any, key, want string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if actual, ok := typed[key].(string); ok && strings.Contains(strings.ToLower(actual), strings.ToLower(want)) {
			return true
		}
		for _, child := range typed {
			if hasStringValue(child, key, want) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if hasStringValue(child, key, want) {
				return true
			}
		}
	}
	return false
}

func stringField(value map[string]any, key string) (string, bool) {
	if raw, ok := value[key].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw), true
	}
	return "", false
}

func numberField(value map[string]any, key string) (int, bool) {
	raw, ok := value[key]
	if !ok {
		return 0, false
	}
	switch typed := raw.(type) {
	case json.Number:
		number, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(number), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}
