package providers

import (
	"bytes"
	"testing"
)

func TestParseStreamEventsAcceptsProviderFamiliesAndPartialUsage(t *testing.T) {
	cases := []struct {
		name   string
		family string
		chunks [][]byte
	}{
		{
			name:   "openai chat",
			family: FamilyOpenAIChat,
			chunks: [][]byte{
				[]byte(`data: {"choices":[{"delta":{"content":"hello "}}]}` + "\n"),
				[]byte(`data: {"choices":[{"delta":{"content":"world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}` + "\n"),
			},
		},
		{
			name:   "openai responses",
			family: FamilyOpenAIResponses,
			chunks: [][]byte{
				[]byte(`data: {"type":"response.output_text.delta","delta":"hello"}` + "\n"),
				[]byte(`data: {"type":"response.completed","response":{"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}` + "\n"),
			},
		},
		{
			name:   "anthropic",
			family: FamilyAnthropicMessages,
			chunks: [][]byte{
				[]byte(`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"text":"hello"}}` + "\n"),
				[]byte(`data: {"type":"message_delta","usage":{"output_tokens":2}}` + "\n" + `data: {"type":"message_stop"}` + "\n"),
			},
		},
		{
			name:   "gemini",
			family: FamilyGemini,
			chunks: [][]byte{
				[]byte(`data: {"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}` + "\n"),
			},
		},
		{
			name:   "bedrock",
			family: FamilyBedrockConverse,
			chunks: [][]byte{
				[]byte(`data: {"contentBlockDelta":{"contentBlockIndex":0,"delta":{"text":"hello"}}}` + "\n"),
				[]byte(`data: {"metadata":{"usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}}` + "\n" + `data: {"messageStop":{"stopReason":"end_turn"}}` + "\n"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report, err := ParseStreamEvents(tc.family, tc.chunks)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Terminal || report.ContentBytes == 0 || report.PartialUsageEvents == 0 {
				t.Fatalf("incomplete stream report: %#v", report)
			}
		})
	}
}

func TestParseStreamEventsRejectsInterleavedToolDeltasCancellationAndTruncation(t *testing.T) {
	_, err := ParseStreamEvents(FamilyOpenAIChat, [][]byte{
		[]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"arguments":"{\"a\""}}]}}]}` + "\n"),
		[]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"arguments":"{\"b\""}}]}}]}` + "\n"),
		[]byte(`data: [DONE]` + "\n"),
	})
	if err == nil {
		t.Fatal("interleaved tool-call deltas were accepted")
	}

	canceled, err := ParseStreamEvents(FamilyOpenAIResponses, [][]byte{
		[]byte(`data: {"type":"response.canceled"}` + "\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !canceled.Canceled || !canceled.Terminal {
		t.Fatalf("cancellation was not terminal: %#v", canceled)
	}

	truncated, err := ParseStreamEvents(FamilyGemini, [][]byte{
		[]byte(`data: {"candidates":[{"finishReason":"MAX_TOKENS"}]}` + "\n"),
	})
	if err == nil {
		t.Fatal("truncated stream was accepted")
	}
	if !truncated.Truncated {
		t.Fatalf("truncated evidence missing: %#v", truncated)
	}
}

func TestParseStreamEventsRejectsMalformedAndUnboundedChunks(t *testing.T) {
	if _, err := ParseStreamEvents(FamilyOpenAIResponses, [][]byte{[]byte(`data: {"type":` + "\n")}); err == nil {
		t.Fatal("malformed JSON stream chunk was accepted")
	}
	if _, err := ParseStreamEvents(FamilyOpenAIResponses, [][]byte{bytes.Repeat([]byte("x"), MaxStreamChunkBytes+1)}); err == nil {
		t.Fatal("oversized stream chunk was accepted")
	}
	if _, err := ParseStreamEvents(FamilyOpenAIResponses, [][]byte{[]byte(`data: {"type":"response.output_text.delta","delta":"unterminated"}` + "\n")}); err == nil {
		t.Fatal("stream without terminal event was accepted")
	}
}

func FuzzParseStreamEventsRejectsMalformedChunks(f *testing.F) {
	f.Add(FamilyOpenAIResponses, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\ndata: {\"type\":\"response.completed\"}\n")
	f.Add(FamilyOpenAIChat, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{}\"}}]},\"finish_reason\":\"stop\"}]}\n")
	f.Add(FamilyAnthropicMessages, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"partial_json\":\"{\\\"x\\\":\"}}\n")
	f.Add(FamilyGemini, "data: {\"candidates\":[{\"finishReason\":\"MAX_TOKENS\"}]}\n")
	f.Fuzz(func(t *testing.T, family, raw string) {
		if len(raw) > MaxStreamChunkBytes*2 {
			t.Skip()
		}
		report, err := ParseStreamEvents(family, [][]byte{[]byte(raw)})
		if err == nil && (!report.Terminal || report.ContentBytes > MaxStreamContentBytes || len(report.Events) > MaxStreamChunks) {
			t.Fatalf("accepted invalid stream report: %#v", report)
		}
	})
}
