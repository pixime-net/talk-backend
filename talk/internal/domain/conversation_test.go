package domain

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// stubClient is a controllable LlmClient for tests.
type stubClient struct {
	responses []*Message
	usages    []Usage // parallel slice; zero value used when shorter
	callCount int
	inputs    [][]Message
}

func (s *stubClient) Complete(_ context.Context, _ string, messages []Message, _ []Tool, _ CompletionOptions) (*Message, Usage, error) {
	if s.callCount >= len(s.responses) {
		return nil, Usage{}, errors.New("stub: no more responses")
	}
	cloned := make([]Message, len(messages))
	copy(cloned, messages)
	s.inputs = append(s.inputs, cloned)
	resp := s.responses[s.callCount]
	var usage Usage
	if s.callCount < len(s.usages) {
		usage = s.usages[s.callCount]
	}
	s.callCount++
	return resp, usage, nil
}

// stubUsageReporter records every event emitted by ConversationManager.
type stubUsageReporter struct {
	messageEvents []MessageEvent
	turns         []TurnEvent
}

func (r *stubUsageReporter) HandleMessageEvent(_ context.Context, e MessageEvent) error {
	r.messageEvents = append(r.messageEvents, e)
	return nil
}

func (r *stubUsageReporter) HandleTurnEvent(_ context.Context, e TurnEvent) error {
	r.turns = append(r.turns, e)
	return nil
}

func (r *stubUsageReporter) HandleToolCallStart(_ context.Context, _ ToolCallEvent) error {
	return nil
}

func (r *stubUsageReporter) HandleToolCallEnd(_ context.Context, _ ToolCallEndEvent) error {
	return nil
}

// stubStore is a simple in-memory MessageStore for tests.
type stubStore struct {
	messages map[string][]Message
	history  map[string][]HistoryTurn
}

func (s *stubStore) HandleMessageEvent(_ context.Context, event MessageEvent) error {
	msg := event.Message
	scope := event.SessionScope

	if s.messages == nil {
		s.messages = make(map[string][]Message)
	}
	s.messages[scope.SessionID()] = append(s.messages[scope.SessionID()], msg)
	return nil
}

func (s *stubStore) HandleToolCallStart(_ context.Context, _ ToolCallEvent) error {
	return nil
}

func (s *stubStore) HandleToolCallEnd(_ context.Context, _ ToolCallEndEvent) error {
	return nil
}

func (s *stubStore) HandleTurnEvent(_ context.Context, event TurnEvent) error {
	if event.TurnID == "" {
		return nil
	}
	if s.history == nil {
		s.history = make(map[string][]HistoryTurn)
	}
	turns := s.history[event.SessionScope.SessionID()]
	for i := range turns {
		if turns[i].TurnID == event.TurnID {
			turns[i].Question = event.Input
			turns[i].Answer = event.Output
			turns[i].At = event.EndedAt
			s.history[event.SessionScope.SessionID()] = turns
			return nil
		}
	}

	s.history[event.SessionScope.SessionID()] = append(turns, HistoryTurn{
		TurnID:   event.TurnID,
		Question: event.Input,
		Answer:   event.Output,
		At:       event.EndedAt,
	})
	return nil
}
func (s *stubStore) AllMessages(_ context.Context, sessionID string) ([]Message, error) {
	if s.messages == nil {
		return nil, nil
	}
	return s.messages[sessionID], nil
}
func (s *stubStore) ClearMessages(_ context.Context, sessionID string) error {
	if s.messages != nil {
		delete(s.messages, sessionID)
	}
	return nil
}
func (s *stubStore) ListSessions(_ context.Context, _ string) ([]SessionSummary, error) {
	return nil, nil
}
func (s *stubStore) LoadHistoryTurnsFromSession(_ context.Context, sessionID string) ([]HistoryTurn, error) {
	if s.history == nil {
		return nil, nil
	}
	return s.history[sessionID], nil
}
func (s *stubStore) DeleteSession(_ context.Context, _ string) error {
	return nil
}

// stubPromptProvider returns a fixed system prompt.
type stubPromptProvider struct{ text string }

func (p *stubPromptProvider) SystemPrompt(_ context.Context) (string, error) {
	return p.text, nil
}

// stubTool is a Tool that records calls and returns a fixed result.
type stubTool struct {
	name   string
	result map[string]any
	err    error
	called atomic.Int32
}

func (t *stubTool) Name() string               { return t.name }
func (t *stubTool) Description() string        { return "stub tool" }
func (t *stubTool) Parameters() map[string]any { return map[string]any{} }
func (t *stubTool) Execute(_ context.Context, _ map[string]any) (ToolOutput, error) {
	t.called.Add(1)
	return ToolOutput{Model: t.result}, t.err
}
func (t *stubTool) InputSchema() (map[string]any, error) {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, nil
}
func (t *stubTool) OutputSchema() (map[string]any, error) {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, nil
}

// --- tests ---

func newManager(client *stubClient, tools []Tool) (*ConversationManager, *stubUsageReporter) {
	reporter := &stubUsageReporter{}
	store := &stubStore{}
	handlers := NewMessageEventHandlers([][]MessageEventHandler{{store}, {reporter}})
	mgr := NewConversationManager(ConversationManagerConfig{
		Client: client,
		Model: Model{
			Name:                    "test-model",
			OTLPProvider:            OTLPProviderAnthropic,
			ContextWindowTokens:     200_000,
			ProviderMaxOutputTokens: 64_000,
		},
		Scope:              NewSessionScope("test-session", "anonymous"),
		Store:              store,
		SessionBrowser:     store,
		PromptProvider:     &stubPromptProvider{"system"},
		Tools:              func() []Tool { return tools },
		EventHandlers:      handlers,
		MaxConcurrentTools: 2,
		ContextFullTurns:   -1,
	})
	return mgr, reporter
}

func TestConversation_NoToolCall(t *testing.T) {
	client := &stubClient{responses: []*Message{
		{Role: RoleAssistant, Content: "Hello!"},
	}}
	mgr, _ := newManager(client, nil)

	answer, err := mgr.Chat(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", answer)
	}
}

func TestConversation_SingleToolCall(t *testing.T) {
	tool := &stubTool{name: "get_current_weather", result: map[string]any{"temperature": "20°C", "condition": "sunny"}}
	client := &stubClient{responses: []*Message{
		{
			Role:      RoleAssistant,
			ToolCalls: []ToolCall{{ID: "1", Name: "get_current_weather", Input: map[string]any{"city": "Paris"}}},
		},
		{Role: RoleAssistant, Content: "It is 20°C and sunny in Paris."},
	}}
	mgr, _ := newManager(client, []Tool{tool})

	answer, err := mgr.Chat(context.Background(), "Weather in Paris?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "It is 20°C and sunny in Paris." {
		t.Errorf("unexpected answer: %q", answer)
	}
	if tool.called.Load() != 1 {
		t.Errorf("expected tool called once, got %d", tool.called.Load())
	}
}

func TestConversation_MaxToolCallsExceeded(t *testing.T) {
	tool := &stubTool{name: "loop_tool", result: map[string]any{"looping": true}}
	responses := make([]*Message, maxToolCalls+1)
	for i := range responses {
		responses[i] = &Message{
			Role:      RoleAssistant,
			ToolCalls: []ToolCall{{ID: "x", Name: "loop_tool", Input: map[string]any{}}},
		}
	}
	client := &stubClient{responses: responses}
	mgr, _ := newManager(client, []Tool{tool})

	_, err := mgr.Chat(context.Background(), "loop?")
	if err == nil {
		t.Error("expected error when max tool calls exceeded")
	}
}

func TestConversation_UnknownToolReturnsError(t *testing.T) {
	client := &stubClient{responses: []*Message{
		{
			Role:      RoleAssistant,
			ToolCalls: []ToolCall{{ID: "1", Name: "nonexistent", Input: map[string]any{}}},
		},
	}}
	mgr, _ := newManager(client, nil)

	_, err := mgr.Chat(context.Background(), "call unknown tool")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

// --- usage reporter tests ---

func TestUsage_OnAPICallFiredPerComplete(t *testing.T) {
	tool := &stubTool{name: "weather", result: map[string]any{"condition": "sunny"}}
	client := &stubClient{
		responses: []*Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1", Name: "weather", Input: map[string]any{}}}},
			{Role: RoleAssistant, Content: "It is sunny."},
		},
		usages: []Usage{
			{InputTokens: 10, OutputTokens: 5},
			{InputTokens: 20, OutputTokens: 8},
		},
	}
	mgr, reporter := newManager(client, []Tool{tool})

	_, err := mgr.Chat(context.Background(), "weather?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assistantEvents := make([]MessageEvent, 0, len(reporter.messageEvents))
	for _, event := range reporter.messageEvents {
		if event.Role == RoleAssistant {
			assistantEvents = append(assistantEvents, event)
		}
	}
	if len(assistantEvents) != 2 {
		t.Fatalf("expected 2 assistant message events, got %d", len(assistantEvents))
	}
	if assistantEvents[0].Kind != CallKindInitial {
		t.Errorf("first call kind: got %q, want %q", assistantEvents[0].Kind, CallKindInitial)
	}
	if assistantEvents[1].Kind != CallKindToolResult {
		t.Errorf("second call kind: got %q, want %q", assistantEvents[1].Kind, CallKindToolResult)
	}
	if assistantEvents[0].Usage.InputTokens != 10 {
		t.Errorf("first call input tokens: got %d, want 10", assistantEvents[0].Usage.InputTokens)
	}
	if assistantEvents[1].Usage.InputTokens != 20 {
		t.Errorf("second call input tokens: got %d, want 20", assistantEvents[1].Usage.InputTokens)
	}
	if assistantEvents[0].Model.ContextWindowTokens != 200_000 {
		t.Errorf("context window: got %d, want 200000", assistantEvents[0].Model.ContextWindowTokens)
	}
	if assistantEvents[0].Model.ProviderMaxOutputTokens != 64_000 {
		t.Errorf("provider output limit: got %d, want 64000", assistantEvents[0].Model.ProviderMaxOutputTokens)
	}
}

func TestUsage_OnConversationTurnAggregatesUsage(t *testing.T) {
	tool := &stubTool{name: "weather", result: map[string]any{"condition": "sunny"}}
	client := &stubClient{
		responses: []*Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1", Name: "weather", Input: map[string]any{}}}},
			{Role: RoleAssistant, Content: "It is sunny."},
		},
		usages: []Usage{
			{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 2, CacheWriteTokens: 3},
			{InputTokens: 20, OutputTokens: 8},
		},
	}
	mgr, reporter := newManager(client, []Tool{tool})

	_, err := mgr.Chat(context.Background(), "weather?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reporter.turns) != 1 {
		t.Fatalf("expected 1 OnConversationTurn event, got %d", len(reporter.turns))
	}
	turn := reporter.turns[0]
	if turn.CallCount != 2 {
		t.Errorf("call count: got %d, want 2", turn.CallCount)
	}
	if turn.TotalUsage.InputTokens != 30 {
		t.Errorf("total input tokens: got %d, want 30", turn.TotalUsage.InputTokens)
	}
	if turn.TotalUsage.OutputTokens != 13 {
		t.Errorf("total output tokens: got %d, want 13", turn.TotalUsage.OutputTokens)
	}
	if turn.TotalUsage.CacheReadTokens != 2 {
		t.Errorf("total cache read tokens: got %d, want 2", turn.TotalUsage.CacheReadTokens)
	}
	if turn.Model.Name != "test-model" {
		t.Errorf("model: got %q, want %q", turn.Model.Name, "test-model")
	}
}

func TestUsage_NoToolCall_SingleAPICallEvent(t *testing.T) {
	client := &stubClient{
		responses: []*Message{{Role: RoleAssistant, Content: "Hi!"}},
		usages:    []Usage{{InputTokens: 5, OutputTokens: 3}},
	}
	mgr, reporter := newManager(client, nil)

	_, err := mgr.Chat(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assistantEvents := make([]MessageEvent, 0, len(reporter.messageEvents))
	for _, event := range reporter.messageEvents {
		if event.Role == RoleAssistant {
			assistantEvents = append(assistantEvents, event)
		}
	}

	if len(assistantEvents) != 1 {
		t.Fatalf("expected 1 assistant message event, got %d", len(assistantEvents))
	}
	if assistantEvents[0].Kind != CallKindInitial {
		t.Errorf("expected CallKindInitial, got %q", assistantEvents[0].Kind)
	}
	if len(reporter.turns) != 1 {
		t.Fatalf("expected 1 turn event, got %d", len(reporter.turns))
	}
	if reporter.turns[0].TotalUsage.InputTokens != 5 {
		t.Errorf("turn input tokens: got %d, want 5", reporter.turns[0].TotalUsage.InputTokens)
	}
}

func TestConversationManager_SetClientUpdatesModelDescriptor(t *testing.T) {
	manager, _ := newManager(&stubClient{}, nil)
	model := Model{Name: "gpt-5.4", ContextWindowTokens: 128_000, ProviderMaxOutputTokens: 16_384}

	manager.SetClient(&stubClient{}, model)

	if manager.model != model {
		t.Errorf("model = %+v, want %+v", manager.model, model)
	}
}

func TestUsage_Add(t *testing.T) {
	a := Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 2, CacheWriteTokens: 3}
	b := Usage{InputTokens: 7, OutputTokens: 4, CacheReadTokens: 1, CacheWriteTokens: 0}
	got := a.Add(b)
	expected := Usage{InputTokens: 17, OutputTokens: 9, CacheReadTokens: 3, CacheWriteTokens: 3}
	if got != expected {
		t.Errorf("Usage.Add: got %+v, want %+v", got, expected)
	}
}

func TestConversation_ParallelToolExecution(t *testing.T) {
	// Setup multiple tools to test parallel execution
	tool1 := &stubTool{name: "tool1", result: map[string]any{"result": "result1"}}
	tool2 := &stubTool{name: "tool2", result: map[string]any{"result": "result2"}}
	tool3 := &stubTool{name: "tool3", result: map[string]any{"result": "result3"}}

	client := &stubClient{responses: []*Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{ID: "1", Name: "tool1", Input: map[string]any{}},
				{ID: "2", Name: "tool2", Input: map[string]any{}},
				{ID: "3", Name: "tool3", Input: map[string]any{}},
			},
		},
		{Role: RoleAssistant, Content: "All tools executed."},
	}}

	// Use maxConcurrentTools = 2 to test concurrency limiting
	reporter := &stubUsageReporter{}
	store := &stubStore{}
	mgr := NewConversationManager(ConversationManagerConfig{
		Client:             client,
		Model:              Model{Name: "test-model", OTLPProvider: OTLPProviderAnthropic},
		Scope:              NewSessionScope("test-session", "anonymous"),
		Store:              store,
		SessionBrowser:     store,
		PromptProvider:     &stubPromptProvider{"system"},
		Tools:              func() []Tool { return []Tool{tool1, tool2, tool3} },
		EventHandlers:      NewMessageEventHandlers([][]MessageEventHandler{{store}, {reporter}}),
		MaxConcurrentTools: 2,
		ContextFullTurns:   -1,
	})

	answer, err := mgr.Chat(context.Background(), "run parallel tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "All tools executed." {
		t.Errorf("unexpected answer: %q", answer)
	}

	// Verify all tools were called exactly once
	if tool1.called.Load() != 1 {
		t.Errorf("tool1 should be called once, got %d", tool1.called.Load())
	}
	if tool2.called.Load() != 1 {
		t.Errorf("tool2 should be called once, got %d", tool2.called.Load())
	}
	if tool3.called.Load() != 1 {
		t.Errorf("tool3 should be called once, got %d", tool3.called.Load())
	}
}

func TestConversation_SequentialWhenMaxConcurrentIsOne(t *testing.T) {
	tool1 := &stubTool{name: "tool1", result: map[string]any{"result": "result1"}}
	tool2 := &stubTool{name: "tool2", result: map[string]any{"result": "result2"}}

	client := &stubClient{responses: []*Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{ID: "1", Name: "tool1", Input: map[string]any{}},
				{ID: "2", Name: "tool2", Input: map[string]any{}},
			},
		},
		{Role: RoleAssistant, Content: "Sequential execution."},
	}}

	// Force sequential execution with maxConcurrentTools = 1
	reporter := &stubUsageReporter{}
	store := &stubStore{}
	mgr := NewConversationManager(ConversationManagerConfig{
		Client:             client,
		Model:              Model{Name: "test-model", OTLPProvider: OTLPProviderAnthropic},
		Scope:              NewSessionScope("test-session", "anonymous"),
		Store:              store,
		SessionBrowser:     store,
		PromptProvider:     &stubPromptProvider{"system"},
		Tools:              func() []Tool { return []Tool{tool1, tool2} },
		EventHandlers:      NewMessageEventHandlers([][]MessageEventHandler{{store}, {reporter}}),
		MaxConcurrentTools: 1,
		ContextFullTurns:   -1,
	})

	answer, err := mgr.Chat(context.Background(), "run sequential tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "Sequential execution." {
		t.Errorf("unexpected answer: %q", answer)
	}

	// Verify tools were called
	if tool1.called.Load() != 1 || tool2.called.Load() != 1 {
		t.Errorf("both tools should be called once, got tool1=%d tool2=%d", tool1.called.Load(), tool2.called.Load())
	}
}

func TestConversation_OneToolMessagePerExecution(t *testing.T) {
	tool1 := &stubTool{name: "tool1", result: map[string]any{"result": "r1"}}
	tool2 := &stubTool{name: "tool2", result: map[string]any{"result": "r2"}}

	client := &stubClient{responses: []*Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{ID: "1", Name: "tool1", Input: map[string]any{"x": 1}},
				{ID: "2", Name: "tool2", Input: map[string]any{"x": 2}},
			},
		},
		{Role: RoleAssistant, Content: "done"},
	}}

	store := &stubStore{}
	mgr := NewConversationManager(ConversationManagerConfig{
		Client:             client,
		Model:              Model{Name: "test-model", OTLPProvider: OTLPProviderAnthropic},
		Scope:              NewSessionScope("test-session", "anonymous"),
		Store:              store,
		SessionBrowser:     store,
		PromptProvider:     &stubPromptProvider{"system"},
		Tools:              func() []Tool { return []Tool{tool1, tool2} },
		EventHandlers:      NewMessageEventHandlers([][]MessageEventHandler{{store}}),
		MaxConcurrentTools: 1,
		ContextFullTurns:   -1,
	})

	_, err := mgr.Chat(context.Background(), "run tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.messages["test-session"]) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(store.messages["test-session"]))
	}

	turnID := store.messages["test-session"][0].TurnID
	if turnID == "" {
		t.Fatal("expected non-empty turn ID on user message")
	}

	toolMsg1 := store.messages["test-session"][2]
	toolMsg2 := store.messages["test-session"][3]

	if toolMsg1.Role != RoleTool || toolMsg2.Role != RoleTool {
		t.Fatalf("expected tool messages at index 2 and 3, got roles %q and %q", toolMsg1.Role, toolMsg2.Role)
	}
	if toolMsg1.TurnID != turnID || toolMsg2.TurnID != turnID {
		t.Fatalf("expected tool messages to carry turn ID %q, got %q and %q", turnID, toolMsg1.TurnID, toolMsg2.TurnID)
	}
	if len(toolMsg1.ToolCalls) != 1 || len(toolMsg1.ToolResults) != 1 {
		t.Fatalf("expected first tool message to contain 1 call and 1 result, got %d calls and %d results", len(toolMsg1.ToolCalls), len(toolMsg1.ToolResults))
	}
	if len(toolMsg2.ToolCalls) != 1 || len(toolMsg2.ToolResults) != 1 {
		t.Fatalf("expected second tool message to contain 1 call and 1 result, got %d calls and %d results", len(toolMsg2.ToolCalls), len(toolMsg2.ToolResults))
	}
	if toolMsg1.Content != "" || toolMsg2.Content != "" {
		t.Fatalf("expected empty tool message content, got %q and %q", toolMsg1.Content, toolMsg2.Content)
	}
}

func TestConversation_AssistantToolOnlyResponseGetsSummaryAndTurnID(t *testing.T) {
	tool := &stubTool{name: "geocode", result: map[string]any{"ok": true}}
	client := &stubClient{responses: []*Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{ID: "1", Name: "geocode", Input: map[string]any{"q": "Orleans"}},
				{ID: "2", Name: "geocode", Input: map[string]any{"q": "Lille"}},
			},
		},
		{Role: RoleAssistant, Content: "Done."},
	}}

	store := &stubStore{}
	mgr := NewConversationManager(ConversationManagerConfig{
		Client:             client,
		Model:              Model{Name: "test-model", OTLPProvider: OTLPProviderAnthropic},
		Scope:              NewSessionScope("test-session", "anonymous"),
		Store:              store,
		SessionBrowser:     store,
		PromptProvider:     &stubPromptProvider{"system"},
		Tools:              func() []Tool { return []Tool{tool} },
		EventHandlers:      NewMessageEventHandlers([][]MessageEventHandler{{store}}),
		MaxConcurrentTools: 2,
		ContextFullTurns:   -1,
	})

	_, err := mgr.Chat(context.Background(), "route")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.messages["test-session"]) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(store.messages["test-session"]))
	}

	assistant := store.messages["test-session"][1]
	if assistant.Role != RoleAssistant {
		t.Fatalf("expected second message to be assistant, got %q", assistant.Role)
	}
	if assistant.TurnID == "" {
		t.Fatal("expected assistant message to carry turn ID")
	}
	if assistant.Content == "" {
		t.Fatal("expected assistant content summary for tool-only response")
	}
	if assistant.Content != "Calling tools geocode, geocode." {
		t.Fatalf("unexpected assistant fallback content: %q", assistant.Content)
	}
}

func TestBuildContextMessages_LeanModeUsesHistoryTurns(t *testing.T) {
	store := &stubStore{
		messages: map[string][]Message{
			"test-session": {
				{Role: RoleUser, Content: "Q1 detailed", TurnID: "t1"},
				{Role: RoleAssistant, Content: "A1 detailed", TurnID: "t1"},
				{Role: RoleUser, Content: "Q2 detailed", TurnID: "t2"},
				{Role: RoleAssistant, Content: "A2 detailed", TurnID: "t2"},
				{Role: RoleUser, Content: "Q3 current", TurnID: "t3"},
			},
		},
		history: map[string][]HistoryTurn{
			"test-session": {
				{TurnID: "t1", Question: "Q1", Answer: "A1", At: time.Now()},
				{TurnID: "t2", Question: "Q2", Answer: "A2", At: time.Now()},
			},
		},
	}

	mgr := NewConversationManager(ConversationManagerConfig{
		Client:             &stubClient{},
		Model:              Model{Name: "test-model", OTLPProvider: OTLPProviderAnthropic},
		Scope:              NewSessionScope("test-session", "anonymous"),
		Store:              store,
		SessionBrowser:     store,
		PromptProvider:     &stubPromptProvider{"system"},
		Tools:              func() []Tool { return nil },
		MaxConcurrentTools: 1,
		ContextFullTurns:   0,
	})

	got := mgr.contextBuilder.BuildContextMessages(context.Background(), "t3")
	if len(got) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(got))
	}
	if got[0].Content != "Q1" || got[1].Content != "A1" || got[2].Content != "Q2" || got[3].Content != "A2" {
		t.Fatalf("expected history question/answer context first, got %#v", got[:4])
	}
	if got[4].TurnID != "t3" {
		t.Fatalf("expected current turn detailed messages kept, got turn %q", got[4].TurnID)
	}
}

func TestBuildContextMessages_HybridKeepsLastNDetailedTurns(t *testing.T) {
	store := &stubStore{
		messages: map[string][]Message{
			"test-session": {
				{Role: RoleUser, Content: "Q1 detailed", TurnID: "t1"},
				{Role: RoleAssistant, Content: "A1 detailed", TurnID: "t1"},
				{Role: RoleUser, Content: "Q2 detailed", TurnID: "t2"},
				{Role: RoleAssistant, Content: "A2 detailed", TurnID: "t2"},
				{Role: RoleUser, Content: "Q3 current", TurnID: "t3"},
			},
		},
		history: map[string][]HistoryTurn{
			"test-session": {
				{TurnID: "t1", Question: "Q1", Answer: "A1", At: time.Now()},
				{TurnID: "t2", Question: "Q2", Answer: "A2", At: time.Now()},
			},
		},
	}

	mgr := NewConversationManager(ConversationManagerConfig{
		Client:             &stubClient{},
		Model:              Model{Name: "test-model", OTLPProvider: OTLPProviderAnthropic},
		Scope:              NewSessionScope("test-session", "anonymous"),
		Store:              store,
		SessionBrowser:     store,
		PromptProvider:     &stubPromptProvider{"system"},
		Tools:              func() []Tool { return nil },
		MaxConcurrentTools: 1,
		ContextFullTurns:   1,
	})

	got := mgr.contextBuilder.BuildContextMessages(context.Background(), "t3")
	if len(got) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(got))
	}
	if got[0].Content != "Q1" || got[1].Content != "A1" {
		t.Fatalf("expected older turn summarized from history, got %#v", got[:2])
	}
	if got[2].TurnID != "t2" || got[3].TurnID != "t2" || got[4].TurnID != "t3" {
		t.Fatalf("expected detailed t2 and current t3, got turns %q %q %q", got[2].TurnID, got[3].TurnID, got[4].TurnID)
	}
}

func TestConversation_ChatUsesExpectedContextSizesByMode(t *testing.T) {
	responses := []*Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1", Name: "weather", Input: map[string]any{"city": "Paris"}}}},
		{Role: RoleAssistant, Content: "A1 final"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "2", Name: "weather", Input: map[string]any{"city": "Lyon"}}}},
		{Role: RoleAssistant, Content: "A2 final"},
		{Role: RoleAssistant, Content: "A3 final"},
	}

	newClient := func() *stubClient {
		client := &stubClient{}
		client.responses = append(client.responses, responses...)
		return client
	}

	assertThirdTurnInputLen := func(t *testing.T, mode int, want int) {
		t.Helper()

		client := newClient()
		tool := &stubTool{name: "weather", result: map[string]any{"ok": true}}
		store := &stubStore{}
		mgr := NewConversationManager(ConversationManagerConfig{
			Client:             client,
			Model:              Model{Name: "test-model", OTLPProvider: OTLPProviderAnthropic},
			Scope:              NewSessionScope("test-session", "anonymous"),
			Store:              store,
			SessionBrowser:     store,
			PromptProvider:     &stubPromptProvider{"system"},
			Tools:              func() []Tool { return []Tool{tool} },
			EventHandlers:      NewMessageEventHandlers([][]MessageEventHandler{{store}}),
			MaxConcurrentTools: 1,
			ContextFullTurns:   mode,
		})

		for _, input := range []string{"Q1", "Q2", "Q3"} {
			if _, err := mgr.Chat(context.Background(), input); err != nil {
				t.Fatalf("chat failed for mode %d: %v", mode, err)
			}
		}

		if len(client.inputs) != 5 {
			t.Fatalf("expected 5 model calls for mode %d, got %d", mode, len(client.inputs))
		}
		if got := len(client.inputs[4]); got != want {
			t.Fatalf("third turn input length for mode %d: got %d, want %d", mode, got, want)
		}
	}

	assertThirdTurnInputLen(t, -1, 9)
	assertThirdTurnInputLen(t, 1, 7)
	assertThirdTurnInputLen(t, 0, 5)
}

func TestConversation_MaxIterations_PersistsIncompleteTurn(t *testing.T) {
	tool := &stubTool{name: "loop_tool", result: map[string]any{"looping": true}}
	responses := make([]*Message, maxToolCalls)
	for i := range responses {
		responses[i] = &Message{
			Role:      RoleAssistant,
			ToolCalls: []ToolCall{{ID: fmt.Sprintf("c%d", i), Name: "loop_tool", Input: map[string]any{}}},
		}
	}
	client := &stubClient{responses: responses}
	mgr, reporter := newManager(client, []Tool{tool})

	_, err := mgr.Chat(context.Background(), "loop?")
	if !errors.Is(err, ErrMaxToolIterations) {
		t.Fatalf("expected ErrMaxToolIterations, got %v", err)
	}

	if len(reporter.turns) != 1 {
		t.Fatalf("expected 1 turn event, got %d", len(reporter.turns))
	}
	turn := reporter.turns[0]
	if turn.Status != TurnStatusIncomplete {
		t.Errorf("turn status = %q, want %q", turn.Status, TurnStatusIncomplete)
	}
	if turn.Input != "loop?" {
		t.Errorf("turn input = %q, want %q", turn.Input, "loop?")
	}
	if turn.Output != "" {
		t.Errorf("turn output = %q, want empty", turn.Output)
	}
	if turn.CallCount != maxToolCalls {
		t.Errorf("turn call count = %d, want %d", turn.CallCount, maxToolCalls)
	}
	if len(turn.ToolCalls) != maxToolCalls {
		t.Errorf("turn tool calls = %d, want %d", len(turn.ToolCalls), maxToolCalls)
	}
}

func TestBuildContextMessages_IncompleteTurnForceIncluded_LeanMode(t *testing.T) {
	store := &stubStore{
		messages: map[string][]Message{
			"test-session": {
				{Role: RoleUser, Content: "Q1 details", TurnID: "t1"},
				{Role: RoleAssistant, Content: "Calling tools fetch.", TurnID: "t1"},
				{Role: RoleTool, Content: "", TurnID: "t1"},
				{Role: RoleUser, Content: "Q2 current", TurnID: "t2"},
			},
		},
		history: map[string][]HistoryTurn{
			"test-session": {
				{TurnID: "t1", Question: "Q1", Answer: "", At: time.Now(), Status: TurnStatusIncomplete},
			},
		},
	}

	cb := NewContextBuilder(store, store, "test-session", 0)
	got := cb.BuildContextMessages(context.Background(), "t2")

	// Lean mode (contextFull=0): normally t1 would be summarized to Q/A.
	// But since t1 is incomplete, it should be force-included in detail.
	var t1Messages []Message
	for _, msg := range got {
		if msg.TurnID == "t1" {
			t1Messages = append(t1Messages, msg)
		}
	}
	if len(t1Messages) != 3 {
		t.Fatalf("expected 3 detailed messages for incomplete turn t1, got %d", len(t1Messages))
	}
}

func TestBuildContextMessages_CompleteTurnSummarized_LeanMode(t *testing.T) {
	store := &stubStore{
		messages: map[string][]Message{
			"test-session": {
				{Role: RoleUser, Content: "Q1 details", TurnID: "t1"},
				{Role: RoleAssistant, Content: "A1 details", TurnID: "t1"},
				{Role: RoleUser, Content: "Q2 current", TurnID: "t2"},
			},
		},
		history: map[string][]HistoryTurn{
			"test-session": {
				{TurnID: "t1", Question: "Q1", Answer: "A1", At: time.Now(), Status: TurnStatusComplete},
			},
		},
	}

	cb := NewContextBuilder(store, store, "test-session", 0)
	got := cb.BuildContextMessages(context.Background(), "t2")

	// Lean mode: complete turn t1 should be summarized (only Q/A from history).
	var t1Messages []Message
	for _, msg := range got {
		if msg.TurnID == "t1" {
			t1Messages = append(t1Messages, msg)
		}
	}
	if len(t1Messages) != 2 {
		t.Fatalf("expected 2 summary messages (Q/A) for complete turn t1, got %d", len(t1Messages))
	}
	if t1Messages[0].Content != "Q1" {
		t.Errorf("first summary message content = %q, want %q", t1Messages[0].Content, "Q1")
	}
	if t1Messages[1].Content != "A1" {
		t.Errorf("second summary message content = %q, want %q", t1Messages[1].Content, "A1")
	}
}
