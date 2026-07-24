package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type ProbeObservation struct {
	ObservedModelID      string
	NativeAPIShape       string
	Capabilities         CapabilitySet
	RequestSchemaSHA256  string
	ResponseSchemaSHA256 string
	LatencyMillis        int64
}

type Adapter interface {
	Family() string
	Probe(context.Context, ProviderProfile, EndpointProfile, ModelProfile) (ProbeObservation, error)
}

func AdapterForFamily(family string) (Adapter, bool) {
	if !validFamily(family) {
		return nil, false
	}
	return fakeAdapter{family: family}, true
}

type fakeAdapter struct {
	family string
}

func (a fakeAdapter) Family() string { return a.family }

func (a fakeAdapter) Probe(ctx context.Context, provider ProviderProfile, endpoint EndpointProfile, model ModelProfile) (ProbeObservation, error) {
	if err := ctx.Err(); err != nil {
		return ProbeObservation{}, err
	}
	if provider.InterfaceFamily != a.family || model.ProviderID != provider.ID || model.EndpointID != endpoint.ID {
		return ProbeObservation{}, errors.New("provider, endpoint, and model do not match adapter family")
	}
	if err := endpoint.Validate(provider); err != nil {
		return ProbeObservation{}, err
	}
	if err := model.Validate(provider, endpoint); err != nil {
		return ProbeObservation{}, err
	}
	requestShape := requestShapeForFamily(a.family)
	responseShape := responseShapeForFamily(a.family)
	return ProbeObservation{
		ObservedModelID:      model.ModelID,
		NativeAPIShape:       nativeShapeForFamily(a.family),
		Capabilities:         capabilitiesForFamily(a.family),
		RequestSchemaSHA256:  sha256Text(requestShape),
		ResponseSchemaSHA256: sha256Text(responseShape),
		LatencyMillis:        int64(10 + len(a.family)),
	}, nil
}

func capabilitiesForFamily(family string) CapabilitySet {
	switch family {
	case FamilyLocalLlama:
		return CapabilitySet{ChatCompletions: true, Streaming: true, Cancellation: true, StructuredOutputs: true, SystemMessages: true, UsageAccounting: true, ImmutableModelIDs: true}
	case FamilyOpenAIResponses:
		return CapabilitySet{ResponsesAPI: true, Streaming: true, Cancellation: true, StructuredOutputs: true, ToolCalls: true, ParallelToolCalls: true, StableToolCallIDs: true, SystemMessages: true, DeveloperMessages: true, UsageAccounting: true, ReasoningControls: true, PromptCaching: true, Batch: true, Asynchronous: true, ModelListing: true, ImmutableModelIDs: true}
	case FamilyOpenAIChat, FamilyOpenAICompatible:
		return CapabilitySet{ChatCompletions: true, Streaming: true, Cancellation: true, StructuredOutputs: true, ToolCalls: true, StableToolCallIDs: true, SystemMessages: true, UsageAccounting: true, ModelListing: true, ImmutableModelIDs: true}
	case FamilyAzureOpenAI:
		return CapabilitySet{ResponsesAPI: true, ChatCompletions: true, Streaming: true, Cancellation: true, StructuredOutputs: true, ToolCalls: true, StableToolCallIDs: true, SystemMessages: true, DeveloperMessages: true, UsageAccounting: true, ReasoningControls: true, PromptCaching: true, Batch: true, Asynchronous: true, ModelListing: true, ImmutableModelIDs: true}
	case FamilyAnthropicMessages:
		return CapabilitySet{Streaming: true, Cancellation: true, StructuredOutputs: true, ToolCalls: true, ParallelToolCalls: true, StableToolCallIDs: true, SystemMessages: true, UsageAccounting: true, ReasoningControls: true, PromptCaching: true, Batch: true, Asynchronous: true, ModelListing: true, ImmutableModelIDs: true}
	case FamilyGemini, FamilyVertexGemini:
		return CapabilitySet{Streaming: true, Cancellation: true, StructuredOutputs: true, ToolCalls: true, StableToolCallIDs: true, SystemMessages: true, UsageAccounting: true, ReasoningControls: true, PromptCaching: true, Batch: true, Asynchronous: true, ModelListing: true, ImmutableModelIDs: true}
	case FamilyBedrockConverse:
		return CapabilitySet{Streaming: true, Cancellation: true, StructuredOutputs: true, ToolCalls: true, StableToolCallIDs: true, SystemMessages: true, UsageAccounting: true, ReasoningControls: true, Batch: true, Asynchronous: true, ModelListing: true, ImmutableModelIDs: true}
	case FamilyBedrockResponsesCompat:
		return CapabilitySet{ResponsesAPI: true, Streaming: true, Cancellation: true, StructuredOutputs: true, ToolCalls: true, StableToolCallIDs: true, SystemMessages: true, DeveloperMessages: true, UsageAccounting: true, ReasoningControls: true, Batch: true, Asynchronous: true, ModelListing: true, ImmutableModelIDs: true}
	default:
		return CapabilitySet{}
	}
}

func nativeShapeForFamily(family string) string {
	switch family {
	case FamilyOpenAIResponses:
		return "openai.responses.create"
	case FamilyOpenAIChat, FamilyOpenAICompatible:
		return "openai.chat.completions.create"
	case FamilyAzureOpenAI:
		return "azure.openai.deployment.responses_or_chat"
	case FamilyAnthropicMessages:
		return "anthropic.messages.create"
	case FamilyGemini:
		return "google.generativeai.generateContent"
	case FamilyVertexGemini:
		return "vertexai.generateContent"
	case FamilyBedrockConverse:
		return "bedrock.converse"
	case FamilyBedrockResponsesCompat:
		return "bedrock.responses_compatible"
	default:
		return "local.llamacpp.openai_compatible_chat"
	}
}

func requestShapeForFamily(family string) string {
	return fmt.Sprintf("codemaintainer/provider-probe-request/%s/schema-v1", family)
}

func responseShapeForFamily(family string) string {
	return fmt.Sprintf("codemaintainer/provider-probe-response/%s/schema-v1", family)
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func probeTimestamp(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now()
}
