package agui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/pixime-net/talk/internal/domain"
)

func TestAGUIEmitter_HandleToolCallStart(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	event := domain.ToolCallEvent{
		TurnID: "turn-1",
		ToolCall: domain.ToolCall{
			ID:    "call-123",
			Name:  "get_weather",
			Input: map[string]any{"city": "Paris"},
		},
	}

	if err := emitter.HandleToolCallStart(context.Background(), event); err != nil {
		t.Fatalf("HandleToolCallStart error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())

	if len(evts) != 2 {
		t.Fatalf("got %d events, want 2", len(evts))
	}

	// First event: TOOL_CALL_START
	if got := evts[0]["type"]; got != "TOOL_CALL_START" {
		t.Errorf("event[0] type = %q, want %q", got, "TOOL_CALL_START")
	}
	if got := evts[0]["toolCallId"]; got != "call-123" {
		t.Errorf("event[0] toolCallId = %q, want %q", got, "call-123")
	}
	if got := evts[0]["toolCallName"]; got != "get_weather" {
		t.Errorf("event[0] toolCallName = %q, want %q", got, "get_weather")
	}

	// Second event: TOOL_CALL_ARGS
	if got := evts[1]["type"]; got != "TOOL_CALL_ARGS" {
		t.Errorf("event[1] type = %q, want %q", got, "TOOL_CALL_ARGS")
	}
	if got := evts[1]["toolCallId"]; got != "call-123" {
		t.Errorf("event[1] toolCallId = %q, want %q", got, "call-123")
	}

	// Verify args delta is valid JSON containing "city":"Paris".
	delta, _ := evts[1]["delta"].(string)
	var args map[string]any
	if err := json.Unmarshal([]byte(delta), &args); err != nil {
		t.Fatalf("delta is not valid JSON: %v", err)
	}
	if args["city"] != "Paris" {
		t.Errorf("args[city] = %v, want %q", args["city"], "Paris")
	}
}

func TestAGUIEmitter_HandleToolCallEnd(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	event := domain.ToolCallEndEvent{
		TurnID: "turn-1",
		ToolCall: domain.ToolCall{
			ID:   "call-456",
			Name: "get_weather",
		},
		Result: domain.ToolResult{ToolCallID: "call-456", Content: "sunny"},
	}

	if err := emitter.HandleToolCallEnd(context.Background(), event); err != nil {
		t.Fatalf("HandleToolCallEnd error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())

	if len(evts) != 2 {
		t.Fatalf("got %d events, want 2", len(evts))
	}

	if got := evts[0]["type"]; got != "TOOL_CALL_END" {
		t.Errorf("event[0] type = %q, want %q", got, "TOOL_CALL_END")
	}
	if got := evts[0]["toolCallId"]; got != "call-456" {
		t.Errorf("event[0] toolCallId = %q, want %q", got, "call-456")
	}
	if got := evts[1]["type"]; got != "TOOL_CALL_RESULT" {
		t.Errorf("event[1] type = %q, want %q", got, "TOOL_CALL_RESULT")
	}
	if got := evts[1]["toolCallId"]; got != "call-456" {
		t.Errorf("event[1] toolCallId = %q, want %q", got, "call-456")
	}
	if got := evts[1]["content"]; got != "sunny" {
		t.Errorf("event[1] content = %q, want %q", got, "sunny")
	}
}

func TestAGUIEmitter_HandleMessageEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	event := domain.MessageEvent{
		Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: "Hello!",
		},
	}

	if err := emitter.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())

	if len(evts) != 3 {
		t.Fatalf("got %d events, want 3 (START, CONTENT, END)", len(evts))
	}

	if got := evts[0]["type"]; got != "TEXT_MESSAGE_START" {
		t.Errorf("event[0] type = %q, want %q", got, "TEXT_MESSAGE_START")
	}
	if got := evts[0]["role"]; got != "assistant" {
		t.Errorf("event[0] role = %q, want %q", got, "assistant")
	}
	if got := evts[1]["type"]; got != "TEXT_MESSAGE_CONTENT" {
		t.Errorf("event[1] type = %q, want %q", got, "TEXT_MESSAGE_CONTENT")
	}
	if got := evts[1]["delta"]; got != "Hello!" {
		t.Errorf("event[1] delta = %q, want %q", got, "Hello!")
	}
	if got := evts[2]["type"]; got != "TEXT_MESSAGE_END" {
		t.Errorf("event[2] type = %q, want %q", got, "TEXT_MESSAGE_END")
	}
}

func TestAGUIEmitter_HandleMessageEvent_SkipsNonAssistant(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	// User messages should be skipped.
	event := domain.MessageEvent{
		Message: domain.Message{
			Role:    domain.RoleUser,
			Content: "hi",
		},
	}

	if err := emitter.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())
	if len(evts) != 0 {
		t.Errorf("got %d events for user message, want 0", len(evts))
	}
}

func TestAGUIEmitter_HandleMessageEvent_SkipsToolCallMessages(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	event := domain.MessageEvent{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   "",
			ToolCalls: []domain.ToolCall{{ID: "tc-1", Name: "tool"}},
		},
	}

	if err := emitter.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())
	if len(evts) != 0 {
		t.Errorf("got %d events for tool-call message, want 0", len(evts))
	}
}

func TestAGUIEmitter_CancelledContext(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	event := domain.ToolCallEvent{
		TurnID:   "turn-1",
		ToolCall: domain.ToolCall{ID: "call-789", Name: "tool"},
	}

	if err := emitter.HandleToolCallStart(ctx, event); err != nil {
		t.Fatalf("expected nil error on cancelled context, got: %v", err)
	}

	// No events should be written.
	evts := parseSSEData(t, rec.Body.Bytes())
	if len(evts) != 0 {
		t.Errorf("got %d events on cancelled context, want 0", len(evts))
	}
}

// parseSSEData extracts JSON objects from SSE data frames.
func parseSSEData(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var result []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonData := strings.TrimPrefix(line, "data: ")
		var m map[string]any
		if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
			t.Fatalf("unmarshaling event: %v\ndata: %s", err, jsonData)
		}
		result = append(result, m)
	}
	return result
}

func TestAGUIEmitter_ReasoningThenText(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	event := domain.MessageEvent{
		Message: domain.Message{
			Role:     domain.RoleAssistant,
			Content:  "The answer is 42.",
			Thinking: "Let me think about this...",
		},
	}

	if err := emitter.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())

	// 5 reasoning + 3 text = 8 events
	if len(evts) != 8 {
		for i, e := range evts {
			t.Logf("event[%d]: %v", i, e["type"])
		}
		t.Fatalf("got %d events, want 8", len(evts))
	}

	assertSSEEventType(t, evts[0], events.EventTypeReasoningStart)
	assertSSEEventType(t, evts[1], events.EventTypeReasoningMessageStart)
	assertSSEEventType(t, evts[2], events.EventTypeReasoningMessageContent)
	assertSSEEventType(t, evts[3], events.EventTypeReasoningMessageEnd)
	assertSSEEventType(t, evts[4], events.EventTypeReasoningEnd)
	assertSSEEventType(t, evts[5], events.EventTypeTextMessageStart)
	assertSSEEventType(t, evts[6], events.EventTypeTextMessageContent)
	assertSSEEventType(t, evts[7], events.EventTypeTextMessageEnd)

	// Verify reasoning content.
	if got := evts[2]["delta"]; got != "Let me think about this..." {
		t.Errorf("reasoning delta = %q, want %q", got, "Let me think about this...")
	}
	// Verify reasoning role.
	if got := evts[1]["role"]; got != "reasoning" {
		t.Errorf("reasoning role = %q, want %q", got, "reasoning")
	}
	// Verify reasoning messageId consistency.
	reasoningID := evts[0]["messageId"]
	for i := 1; i <= 4; i++ {
		if evts[i]["messageId"] != reasoningID {
			t.Errorf("event[%d] messageId = %v, want %v", i, evts[i]["messageId"], reasoningID)
		}
	}
	// Verify text content.
	if got := evts[6]["delta"]; got != "The answer is 42." {
		t.Errorf("text delta = %q, want %q", got, "The answer is 42.")
	}
}

func TestAGUIEmitter_ReasoningWithToolCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	// Assistant message with thinking AND tool calls → reasoning emitted, no text.
	event := domain.MessageEvent{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   "",
			Thinking:  "I need to call a tool.",
			ToolCalls: []domain.ToolCall{{ID: "tc-1", Name: "search"}},
		},
	}

	if err := emitter.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())

	// Only 5 reasoning events, no text events.
	if len(evts) != 5 {
		for i, e := range evts {
			t.Logf("event[%d]: %v", i, e["type"])
		}
		t.Fatalf("got %d events, want 5", len(evts))
	}

	assertSSEEventType(t, evts[0], events.EventTypeReasoningStart)
	assertSSEEventType(t, evts[1], events.EventTypeReasoningMessageStart)
	assertSSEEventType(t, evts[2], events.EventTypeReasoningMessageContent)
	assertSSEEventType(t, evts[3], events.EventTypeReasoningMessageEnd)
	assertSSEEventType(t, evts[4], events.EventTypeReasoningEnd)
}

func TestAGUIEmitter_NoReasoningWhenThinkingEmpty(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	event := domain.MessageEvent{
		Message: domain.Message{
			Role:     domain.RoleAssistant,
			Content:  "Simple response.",
			Thinking: "",
		},
	}

	if err := emitter.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())

	// Only 3 text events, no reasoning.
	if len(evts) != 3 {
		for i, e := range evts {
			t.Logf("event[%d]: %v", i, e["type"])
		}
		t.Fatalf("got %d events, want 3", len(evts))
	}

	assertSSEEventType(t, evts[0], events.EventTypeTextMessageStart)
	assertSSEEventType(t, evts[1], events.EventTypeTextMessageContent)
	assertSSEEventType(t, evts[2], events.EventTypeTextMessageEnd)
}

func TestAGUIEmitter_NoReasoningForUserMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	event := domain.MessageEvent{
		Message: domain.Message{
			Role:     domain.RoleUser,
			Content:  "hi",
			Thinking: "this should not emit anything",
		},
	}

	if err := emitter.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}

	evts := parseSSEData(t, rec.Body.Bytes())
	if len(evts) != 0 {
		t.Errorf("got %d events for user message with thinking, want 0", len(evts))
	}
}

func TestAGUIEmitter_ReasoningCancelledContext(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}

	emitter := NewAGUIEmitter(sse, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	event := domain.MessageEvent{
		Message: domain.Message{
			Role:     domain.RoleAssistant,
			Content:  "answer",
			Thinking: "thinking...",
		},
	}

	if err := emitter.HandleMessageEvent(ctx, event); err != nil {
		t.Fatalf("expected nil error on cancelled context, got: %v", err)
	}

	// No events should be written when context is cancelled.
	evts := parseSSEData(t, rec.Body.Bytes())
	if len(evts) != 0 {
		t.Errorf("got %d events on cancelled context, want 0", len(evts))
	}
}

func assertSSEEventType(t *testing.T, m map[string]any, want events.EventType) {
	t.Helper()
	got, _ := m["type"].(string)
	if got != string(want) {
		t.Errorf("event type = %q, want %q", got, want)
	}
}

func TestAGUIEmitter_HandleMessageEvent_EmitsTokenUsagePerAssistantResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}
	emitter := NewAGUIEmitter(sse, nil)
	model := domain.Model{
		Name:                    "sonnet-4.6",
		ContextWindowTokens:     200_000,
		ProviderMaxOutputTokens: 64_000,
	}

	eventsToEmit := []domain.MessageEvent{
		{
			Message: domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "tc-1", Name: "search"}}},
			Model:   model,
			Kind:    domain.CallKindInitial,
			Usage:   domain.Usage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 10},
		},
		{
			Message: domain.Message{Role: domain.RoleTool, Content: "result"},
			Model:   model,
			Kind:    domain.CallKindToolResult,
		},
		{
			Message: domain.Message{Role: domain.RoleAssistant, Content: "done"},
			Model:   model,
			Kind:    domain.CallKindToolResult,
			Usage:   domain.Usage{InputTokens: 150, OutputTokens: 30, ReasoningTokens: 5},
		},
	}
	for _, event := range eventsToEmit {
		if err := emitter.HandleMessageEvent(context.Background(), event); err != nil {
			t.Fatalf("HandleMessageEvent error: %v", err)
		}
	}

	eventData := parseSSEData(t, rec.Body.Bytes())
	usageEvents := customEventsNamed(eventData, "token_usage")
	if len(usageEvents) != 2 {
		t.Fatalf("got %d token_usage events, want 2", len(usageEvents))
	}
	first := customEventValue(t, usageEvents[0])
	second := customEventValue(t, usageEvents[1])
	if first["input_tokens"] != float64(100) || second["input_tokens"] != float64(150) {
		t.Errorf("per-call input tokens = (%v, %v), want (100, 150)", first["input_tokens"], second["input_tokens"])
	}
	if first["model"] != "sonnet-4.6" || first["context_window_tokens"] != float64(200_000) {
		t.Errorf("first payload model metadata = %v", first)
	}
	if first["output_tokens"] != float64(20) ||
		first["cache_read_tokens"] != float64(10) ||
		first["provider_max_output_tokens"] != float64(64_000) {
		t.Errorf("first payload token details = %v", first)
	}
	if first["context_ratio"] != 0.0005 {
		t.Errorf("context_ratio = %v, want 0.0005", first["context_ratio"])
	}
	if first["output_ratio"] != 0.0003125 {
		t.Errorf("output_ratio = %v, want 0.0003125", first["output_ratio"])
	}
	if second["output_tokens"] != float64(30) || second["reasoning_tokens"] != float64(5) {
		t.Errorf("second payload token details = %v", second)
	}
	if eventData[len(eventData)-1]["name"] != "token_usage" {
		t.Errorf("last event = %v, want token_usage after text events", eventData[len(eventData)-1])
	}
}

func TestAGUIEmitter_HandleTurnEvent_EmitsUsageForEveryStatus(t *testing.T) {
	for _, status := range []string{domain.TurnStatusComplete, domain.TurnStatusIncomplete} {
		t.Run(status, func(t *testing.T) {
			rec := httptest.NewRecorder()
			sse, err := NewSSEWriter(rec, nil)
			if err != nil {
				t.Fatalf("creating SSEWriter: %v", err)
			}
			emitter := NewAGUIEmitter(sse, nil)
			event := domain.TurnEvent{
				Model:      domain.Model{Name: "gpt-5.4"},
				TotalUsage: domain.Usage{InputTokens: 250, OutputTokens: 50, CacheWriteTokens: 4},
				Status:     status,
			}

			if err := emitter.HandleTurnEvent(context.Background(), event); err != nil {
				t.Fatalf("HandleTurnEvent error: %v", err)
			}

			eventData := parseSSEData(t, rec.Body.Bytes())
			usageEvents := customEventsNamed(eventData, "turn_usage")
			if len(usageEvents) != 1 {
				t.Fatalf("got %d turn_usage events, want 1", len(usageEvents))
			}
			value := customEventValue(t, usageEvents[0])
			if value["input_tokens"] != float64(250) || value["output_tokens"] != float64(50) {
				t.Errorf("turn usage = %v, want summed counts", value)
			}
			if _, ok := value["context_ratio"]; ok {
				t.Error("turn_usage unexpectedly contains context_ratio")
			}
		})
	}
}

func TestConversationManager_EmitsAuthoritativeTurnUsage(t *testing.T) {
	tests := []struct {
		name            string
		responses       []*domain.Message
		usages          []domain.Usage
		wantErr         error
		wantTokenEvents int
		wantInput       float64
		wantOutput      float64
	}{
		{
			name: "completed multi-call turn",
			responses: []*domain.Message{
				{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "call-1", Name: "test_tool"}}},
				{Role: domain.RoleAssistant, Content: "done"},
			},
			usages: []domain.Usage{
				{InputTokens: 10, OutputTokens: 5},
				{InputTokens: 20, OutputTokens: 8},
			},
			wantTokenEvents: 2,
			wantInput:       30,
			wantOutput:      13,
		},
		{
			name: "interrupted turn",
			responses: []*domain.Message{
				{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "call-1", Name: "test_tool"}}},
				{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "call-2", Name: "test_tool"}}},
				{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "call-3", Name: "test_tool"}}},
				{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "call-4", Name: "test_tool"}}},
				{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "call-5", Name: "test_tool"}}},
			},
			usages: []domain.Usage{
				{InputTokens: 1, OutputTokens: 1},
				{InputTokens: 2, OutputTokens: 1},
				{InputTokens: 3, OutputTokens: 1},
				{InputTokens: 4, OutputTokens: 1},
				{InputTokens: 5, OutputTokens: 1},
			},
			wantErr:         domain.ErrMaxToolIterations,
			wantTokenEvents: 5,
			wantInput:       15,
			wantOutput:      5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			manager := newUsageTestConversationManager(t, rec, tt.responses, tt.usages)

			_, err := manager.Chat(context.Background(), "test")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Chat error = %v, want %v", err, tt.wantErr)
			}

			eventData := parseSSEData(t, rec.Body.Bytes())
			if got := len(customEventsNamed(eventData, "token_usage")); got != tt.wantTokenEvents {
				t.Errorf("token_usage events = %d, want %d", got, tt.wantTokenEvents)
			}
			turnUsageEvents := customEventsNamed(eventData, "turn_usage")
			if len(turnUsageEvents) != 1 {
				t.Fatalf("turn_usage events = %d, want 1", len(turnUsageEvents))
			}
			value := customEventValue(t, turnUsageEvents[0])
			if value["input_tokens"] != tt.wantInput || value["output_tokens"] != tt.wantOutput {
				t.Errorf("turn_usage = %v, want input=%v output=%v", value, tt.wantInput, tt.wantOutput)
			}
		})
	}
}

type usageTestClient struct {
	responses []*domain.Message
	usages    []domain.Usage
	call      int
}

func (c *usageTestClient) Complete(
	_ context.Context,
	_ string,
	_ []domain.Message,
	_ []domain.Tool,
	_ domain.CompletionOptions,
) (*domain.Message, domain.Usage, error) {
	if c.call >= len(c.responses) {
		return nil, domain.Usage{}, errors.New("no test response available")
	}
	response := c.responses[c.call]
	usage := c.usages[c.call]
	c.call++
	return response, usage, nil
}

type usageTestStore struct {
	messages map[string][]domain.Message
}

func (s *usageTestStore) HandleMessageEvent(_ context.Context, event domain.MessageEvent) error {
	if s.messages == nil {
		s.messages = make(map[string][]domain.Message)
	}
	sessionID := event.SessionScope.SessionID()
	s.messages[sessionID] = append(s.messages[sessionID], event.Message)
	return nil
}

func (*usageTestStore) HandleTurnEvent(context.Context, domain.TurnEvent) error { return nil }
func (*usageTestStore) HandleToolCallStart(context.Context, domain.ToolCallEvent) error {
	return nil
}
func (*usageTestStore) HandleToolCallEnd(context.Context, domain.ToolCallEndEvent) error {
	return nil
}
func (s *usageTestStore) AllMessages(_ context.Context, sessionID string) ([]domain.Message, error) {
	return s.messages[sessionID], nil
}
func (s *usageTestStore) ClearMessages(_ context.Context, sessionID string) error {
	delete(s.messages, sessionID)
	return nil
}
func (*usageTestStore) ListSessions(context.Context, string) ([]domain.SessionSummary, error) {
	return nil, nil
}
func (*usageTestStore) LoadHistoryTurnsFromSession(context.Context, string) ([]domain.HistoryTurn, error) {
	return nil, nil
}
func (*usageTestStore) DeleteSession(context.Context, string) error { return nil }

type usageTestPromptProvider struct{}

func (usageTestPromptProvider) SystemPrompt(context.Context) (string, error) {
	return "system", nil
}

type usageTestTool struct{}

func (usageTestTool) Name() string        { return "test_tool" }
func (usageTestTool) Description() string { return "test tool" }
func (usageTestTool) InputSchema() (map[string]any, error) {
	return map[string]any{"type": "object"}, nil
}
func (usageTestTool) OutputSchema() (map[string]any, error) {
	return map[string]any{"type": "object"}, nil
}
func (usageTestTool) Execute(context.Context, map[string]any) (domain.ToolOutput, error) {
	return domain.ToolOutput{Model: map[string]any{"ok": true}}, nil
}

func newUsageTestConversationManager(
	t *testing.T,
	rec *httptest.ResponseRecorder,
	responses []*domain.Message,
	usages []domain.Usage,
) *domain.ConversationManager {
	t.Helper()
	sse, err := NewSSEWriter(rec, nil)
	if err != nil {
		t.Fatalf("creating SSEWriter: %v", err)
	}
	store := &usageTestStore{}
	emitter := NewAGUIEmitter(sse, nil)
	handlers := domain.NewMessageEventHandlers([][]domain.MessageEventHandler{{store}, {emitter}})
	return domain.NewConversationManager(domain.ConversationManagerConfig{
		Client:              &usageTestClient{responses: responses, usages: usages},
		Model:               domain.Model{Name: "test-model", ContextWindowTokens: 100, ProviderMaxOutputTokens: 50},
		SessionScope:        domain.NewSessionScope("test-session", "anonymous"),
		MessageRepository:   store,
		SessionRepository:   store,
		PromptProvider:      usageTestPromptProvider{},
		Tools:               func() []domain.Tool { return []domain.Tool{usageTestTool{}} },
		MessageEventHandler: handlers,
		MaxConcurrentTools:  1,
		ContextFullTurns:    -1,
	})
}

func customEventsNamed(eventData []map[string]any, name string) []map[string]any {
	matched := make([]map[string]any, 0)
	for _, event := range eventData {
		if event["type"] == "CUSTOM" && event["name"] == name {
			matched = append(matched, event)
		}
	}
	return matched
}

func customEventValue(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	value, ok := event["value"].(map[string]any)
	if !ok {
		t.Fatalf("custom event value = %T, want object", event["value"])
	}
	return value
}
