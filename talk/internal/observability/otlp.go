package observability

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/pixime-net/talk/internal/domain"
)

// OpenTelemetry OTLP structures following the standard specification
// https://opentelemetry.io/docs/specs/otlp/

// OTLPTracesPayload is the root structure for OTLP traces
type OTLPTracesPayload struct {
	ResourceSpans []OTLPResourceSpans `json:"resourceSpans"`
}

// OTLPResourceSpans represents spans from a single resource
type OTLPResourceSpans struct {
	Resource   OTLPResource     `json:"resource"`
	ScopeSpans []OTLPScopeSpans `json:"scopeSpans"`
}

// OTLPResource describes the entity that produced the telemetry
type OTLPResource struct {
	Attributes []OTLPAttribute `json:"attributes"`
}

// OTLPScopeSpans represents spans from a single instrumentation scope
type OTLPScopeSpans struct {
	Scope OTLPInstrumentationScope `json:"scope"`
	Spans []OTLPSpan               `json:"spans"`
}

// OTLPInstrumentationScope represents the instrumentation library
type OTLPInstrumentationScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// OTLPSpan represents a single span in OpenTelemetry format
type OTLPSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId,omitempty"`
	Name              string          `json:"name"`
	Kind              int             `json:"kind"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []OTLPAttribute `json:"attributes"`
	Status            OTLPStatus      `json:"status"`
}

// OTLPAttribute represents a key-value pair attribute
type OTLPAttribute struct {
	Key   string    `json:"key"`
	Value OTLPValue `json:"value"`
}

// OTLPValue represents the value of an attribute
type OTLPValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *int64   `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

// OTLPStatus represents the status of a span
type OTLPStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

// OTLPTrace is our internal representation before sending to Langfuse
type OTLPTrace struct {
	Span OTLPSpan
}

// Helper functions to create OTLP values
func stringValue(s string) OTLPValue {
	return OTLPValue{StringValue: &s}
}

func intValue(i int64) OTLPValue {
	return OTLPValue{IntValue: &i}
}

// Mapping functions from domain events to OpenTelemetry format

// apiCallToOTLP converts a domain.MessageEvent to OpenTelemetry format
func (l *LangfuseUsageReporter) apiCallToOTLP(messageEvent domain.MessageEvent) (*OTLPTrace, error) {
	traceID := messageEvent.TurnID
	spanID := generateSpanID()
	parentSpanID := messageEvent.TurnSpanID

	// OTLPProvider constants are defined to match OTel GenAI semantic conventions directly.
	system := string(messageEvent.Model.OTLPProvider)

	attributes := []OTLPAttribute{
		// GenAI semantic conventions
		{Key: "gen_ai.provider.name", Value: stringValue(system)},
		{Key: "gen_ai.request.model", Value: stringValue(messageEvent.Model.APIModelID)},
		{Key: "gen_ai.response.model", Value: stringValue(messageEvent.Model.APIModelID)},
		{Key: "gen_ai.operation.name", Value: stringValue(string(messageEvent.Kind))},

		// Input and output for Langfuse
		{Key: "gen_ai.prompt", Value: stringValue(messageEvent.Input)},
		{Key: "gen_ai.completion", Value: stringValue(messageEvent.Output)},

		// Langfuse-specific input/output
		{Key: "langfuse.observation.input", Value: stringValue(messageEvent.Input)},
		{Key: "langfuse.observation.output", Value: stringValue(messageEvent.Output)},

		// Usage information
		{Key: "gen_ai.usage.input_tokens", Value: intValue(messageEvent.Usage.InputTokens)},
		{Key: "gen_ai.usage.output_tokens", Value: intValue(messageEvent.Usage.OutputTokens)},

		// Anthropic-specific cache information
		{Key: "gen_ai.usage.cache_read_tokens", Value: intValue(messageEvent.Usage.CacheReadTokens)},
		{Key: "gen_ai.usage.cache_write_tokens", Value: intValue(messageEvent.Usage.CacheWriteTokens)},

		// Langfuse-specific attributes
		{Key: "langfuse.observation.type", Value: stringValue("generation")},
		{Key: "langfuse.observation.level", Value: stringValue("DEFAULT")},
	}

	// Add tool call information if present
	if len(messageEvent.ToolCalls) > 0 {
		toolCallsJSON, _ := json.Marshal(messageEvent.ToolCalls)
		attributes = append(attributes, OTLPAttribute{
			Key:   "langfuse.observation.metadata.tool_calls",
			Value: stringValue(string(toolCallsJSON)),
		})

		// Add tool names for easier filtering
		var toolNames []string
		for _, tc := range messageEvent.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		toolNamesJSON, _ := json.Marshal(toolNames)
		attributes = append(attributes, OTLPAttribute{
			Key:   "langfuse.observation.metadata.tool_names",
			Value: stringValue(string(toolNamesJSON)),
		})
	}

	span := OTLPSpan{
		TraceID:           traceID,
		SpanID:            spanID,
		ParentSpanID:      parentSpanID,
		Name:              fmt.Sprintf("llm_%s", messageEvent.Kind),
		Kind:              3, // SPAN_KIND_CLIENT
		StartTimeUnixNano: strconv.FormatInt(messageEvent.StartedAt.UnixNano(), 10),
		EndTimeUnixNano:   strconv.FormatInt(messageEvent.EndedAt.UnixNano(), 10),
		Attributes:        attributes,
		Status: OTLPStatus{
			Code: 1, // STATUS_CODE_OK
		},
	}

	return &OTLPTrace{Span: span}, nil
}

// conversationTurnToOTLP converts a domain.TurnEvent to OpenTelemetry format
func (l *LangfuseUsageReporter) conversationTurnToOTLP(turnEvent domain.TurnEndEvent) (*OTLPTrace, error) {
	traceID := turnEvent.TurnID
	spanID := turnEvent.TurnSpanID

	attributes := []OTLPAttribute{
		// Trace-level attributes (Langfuse OTLP mapping)
		{Key: "langfuse.trace.name", Value: stringValue("conversation_turn")},
		{Key: "langfuse.session.id", Value: stringValue(turnEvent.SessionScope.SessionID())},
		{Key: "langfuse.user.id", Value: stringValue(turnEvent.SessionScope.UserID())},
		{Key: "langfuse.trace.input", Value: stringValue(turnEvent.Input)},
		{Key: "langfuse.trace.output", Value: stringValue(turnEvent.Output)},

		// Model information
		{Key: "gen_ai.request.model", Value: stringValue(turnEvent.Model.Name)},
		{Key: "gen_ai.response.model", Value: stringValue(turnEvent.Model.Name)},

		// Input and output for Langfuse
		{Key: "gen_ai.prompt", Value: stringValue(turnEvent.Input)},
		{Key: "gen_ai.completion", Value: stringValue(turnEvent.Output)},

		// Langfuse observation-level input/output
		{Key: "langfuse.observation.input", Value: stringValue(turnEvent.Input)},
		{Key: "langfuse.observation.output", Value: stringValue(turnEvent.Output)},

		// Aggregated usage information
		{Key: "gen_ai.usage.input_tokens", Value: intValue(turnEvent.TotalUsage.InputTokens)},
		{Key: "gen_ai.usage.output_tokens", Value: intValue(turnEvent.TotalUsage.OutputTokens)},
		{Key: "gen_ai.usage.cache_read_tokens", Value: intValue(turnEvent.TotalUsage.CacheReadTokens)},
		{Key: "gen_ai.usage.cache_write_tokens", Value: intValue(turnEvent.TotalUsage.CacheWriteTokens)},

		// Turn-specific information
		{Key: "call_count", Value: intValue(int64(turnEvent.CallCount))},

		// Langfuse-specific attributes
		{Key: "langfuse.observation.type", Value: stringValue("span")},
		{Key: "langfuse.observation.level", Value: stringValue("DEFAULT")},
	}

	// Add turn status if not the default "complete".
	if turnEvent.Status != "" && turnEvent.Status != domain.TurnStatusComplete {
		attributes = append(attributes, OTLPAttribute{
			Key:   "langfuse.trace.metadata.status",
			Value: stringValue(turnEvent.Status),
		})
	}

	// Add tool call information if present
	if len(turnEvent.ToolCalls) > 0 {
		toolCallsJSON, _ := json.Marshal(turnEvent.ToolCalls)
		attributes = append(attributes, OTLPAttribute{
			Key:   "langfuse.observation.metadata.tool_calls",
			Value: stringValue(string(toolCallsJSON)),
		})

		// Add tool names for easier filtering
		var toolNames []string
		for _, tc := range turnEvent.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		toolNamesJSON, _ := json.Marshal(toolNames)
		attributes = append(attributes, OTLPAttribute{
			Key:   "langfuse.observation.metadata.tool_names",
			Value: stringValue(string(toolNamesJSON)),
		})

		// Add tool count
		attributes = append(attributes, OTLPAttribute{
			Key:   "langfuse.observation.metadata.tool_count",
			Value: intValue(int64(len(turnEvent.ToolCalls))),
		})
	}

	span := OTLPSpan{
		TraceID:           traceID,
		SpanID:            spanID,
		Name:              "conversation_turn",
		Kind:              1, // SPAN_KIND_INTERNAL
		StartTimeUnixNano: strconv.FormatInt(turnEvent.StartedAt.UnixNano(), 10),
		EndTimeUnixNano:   strconv.FormatInt(turnEvent.EndedAt.UnixNano(), 10),
		Attributes:        attributes,
		Status: OTLPStatus{
			Code: 1, // STATUS_CODE_OK
		},
	}

	return &OTLPTrace{Span: span}, nil
}
