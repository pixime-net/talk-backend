package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ErrSystemPrompt is returned when the system prompt cannot be loaded.
var ErrSystemPrompt = errors.New("loading system prompt")

// ErrMaxToolIterations is returned when the tool execution loop exhausts its iteration budget.
var ErrMaxToolIterations = errors.New("maximum tool call iterations exceeded")

const maxToolCalls = 5

// Turn status constants
const (
	TurnStatusComplete   = "complete"
	TurnStatusIncomplete = "incomplete"
)

// Interrupt state constants.
const (
	InterruptStateOpen      = "open"
	InterruptStateResolved  = "resolved"
	InterruptStateCancelled = "cancelled"
)

// ConversationManager orchestrates a multi-turn conversation with optional tool calls.
type ConversationManager struct {
	llmClient         LlmClient
	model             Model
	sessionScope      SessionScope
	eventHandlers     EventHandlers
	messageRepository MessageRepository
	promptProvider    PromptProvider
	toolsProvider     func() []Tool
	contextBuilder    *ContextBuilder
	toolExecutor      *ToolExecutor
	thinkingEffort    ThinkingEffort
}

// ConversationManagerConfig groups all parameters for creating a ConversationManager.
type ConversationManagerConfig struct {
	Client             LlmClient
	Model              Model
	SessionScope       SessionScope
	EventHandlers      EventHandlers
	MessageRepository  MessageRepository
	SessionRepository  SessionRepository
	PromptProvider     PromptProvider
	Tools              func() []Tool
	MaxConcurrentTools int
	ContextFullTurns   int
}

// NewConversationManager creates a ConversationManager.
func NewConversationManager(cfg ConversationManagerConfig) *ConversationManager {
	return &ConversationManager{
		sessionScope:      cfg.SessionScope,
		llmClient:         cfg.Client,
		model:             cfg.Model,
		eventHandlers:     cfg.EventHandlers,
		messageRepository: cfg.MessageRepository,
		promptProvider:    cfg.PromptProvider,
		toolsProvider:     cfg.Tools,
		contextBuilder:    NewContextBuilder(cfg.MessageRepository, cfg.SessionRepository, cfg.SessionScope.SessionID(), cfg.ContextFullTurns),
		toolExecutor:      NewToolExecutor(cfg.Tools, cfg.MaxConcurrentTools, &cfg.EventHandlers),
	}
}

// SetScope updates the active session scope for the conversation manager.
func (m *ConversationManager) SetScope(scope SessionScope) {
	m.sessionScope = scope
	m.contextBuilder.sessionID = scope.SessionID()
}

// SetClient replaces the active LLM client and model without resetting the conversation history.
func (m *ConversationManager) SetClient(client LlmClient, model Model) {
	m.llmClient = client
	m.model = model
}

// SetThinkingEffort changes the thinking/reasoning level for subsequent LLM calls.
func (m *ConversationManager) SetThinkingEffort(effort ThinkingEffort) {
	m.thinkingEffort = effort
}

// ThinkingEffort returns the current thinking/reasoning level.
func (m *ConversationManager) ThinkingEffort() ThinkingEffort {
	return m.thinkingEffort
}

// Chat sends a user message and returns the final assistant text response.
// Tool calls are resolved automatically up to maxToolCalls iterations.
func (m *ConversationManager) Chat(ctx context.Context, userInput string) (string, error) {
	systemPrompt, err := m.promptProvider.SystemPrompt(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSystemPrompt, err)
	}

	// tc groups the fields that stay constant for the entire conversation turn.
	tc := turnContext{
		// turnID is used to correlate all messages, API calls, and tool calls for this conversation turn in observability.
		turnID: GenerateTraceID(),
		// turnSpanID is used to correlate all events for this conversation turn in observability. It is the parent span for all API call spans in this turn.
		turnSpanID: GenerateSpanID(),
		// startedAt marks the beginning of this conversation turn for observability and timing purposes.
		startedAt: time.Now(),
		// userInput is the message content provided by the user for this conversation turn.
		userInput: userInput,
	}
	// Store the user message in the conversation history before processing to ensure it's included in the context
	// for the first API call and in observability.
	if err := m.eventHandlers.HandleMessageEvent(ctx, MessageEvent{
		Message:      Message{Role: RoleUser, Content: userInput, TurnID: tc.turnID},
		SessionScope: m.sessionScope,
		Model:        m.model,
		TurnSpanID:   tc.turnSpanID,
		Kind:         CallKindInitial,
		StartedAt:    tc.startedAt,
	}); err != nil {
		return "", fmt.Errorf("handling user message event: %w", err)
	}
	// cumulative usage and tool calls for the entire conversation turn.
	var totalUsage Usage
	// toolCalls keeps track of every tool call made during this conversation turn.
	toolCalls := make([]ToolCall, 0, 8) // pre-allocate to avoid repeated growth

	callCount := 0
	// kind tracks whether the API call is the initial LLM call or a subsequent call after tool results, for observability purposes.
	kind := CallKindInitial

	// Resolve tools once for the entire turn — they don't change between iterations.
	tools := m.toolsProvider()

	for range maxToolCalls {
		// Get the current conversation context for the API call input
		messages := m.contextBuilder.BuildContextMessages(ctx, tc.turnID)
		conversationInput := formatMessagesAsInput(messages, systemPrompt)

		// Send the conversation to the LLM and get the response with token usage.
		// The response may contain tool calls that need to be executed.
		callStartedAt := time.Now()
		response, usage, err := m.llmClient.Complete(ctx, systemPrompt, messages, tools, CompletionOptions{
			ThinkingEffort: m.thinkingEffort,
		})
		if err != nil {
			return "", fmt.Errorf("model completion: %w", err)
		}
		var callEndedAt = time.Now()

		totalUsage = totalUsage.Add(usage)
		toolCalls = append(toolCalls, response.ToolCalls...)
		callCount++

		response.TurnID = tc.turnID
		if err := m.emitAssistantMessageEvent(ctx, tc, assistantResponse{
			response:          response,
			kind:              kind,
			usage:             usage,
			startedAt:         callStartedAt,
			endedAt:           callEndedAt,
			conversationInput: conversationInput,
		}); err != nil {
			return "", fmt.Errorf("handling assistant message event: %w", err)
		}

		kind = CallKindToolResult

		// If the model responded without tool calls, the turn is complete.
		if len(response.ToolCalls) == 0 {
			return m.emitTurnCompleted(ctx, tc, totalUsage, callCount, toolCalls, response.Content)
		}

		// The model responded with tool calls, we need to execute them before the next iteration.
		toolExecutions, err := m.toolExecutor.Execute(ctx, tc.turnID, response.ToolCalls)
		if err != nil {
			return "", err
		}
		if err := m.emitToolResultEvents(ctx, tc, toolExecutions); err != nil {
			return "", err
		}
	}

	// Persist the incomplete turn before returning the iteration limit error.
	if _, err := m.emitTurnNotCompleted(ctx, tc, totalUsage, callCount, toolCalls); err != nil {
		slog.Error("persisting incomplete turn on max iterations", slog.String("turn_id", tc.turnID), slog.String("error", err.Error()))
	}

	return "", ErrMaxToolIterations
}

// turnContext groups the fields that stay constant for the entire conversation turn.
type turnContext struct {
	turnID     string
	turnSpanID string
	startedAt  time.Time
	userInput  string
}

// assistantResponse groups the fields that vary on each LLM completion call within a turn.
type assistantResponse struct {
	response          *Message
	kind              CallKind
	usage             Usage
	startedAt         time.Time
	endedAt           time.Time
	conversationInput string
}

// emitAssistantMessageEvent stores the assistant message in the conversation history.
// When the model responds with only tool calls (empty text content), it substitutes
// the content with a human-readable tool-call summary for observability.
func (m *ConversationManager) emitAssistantMessageEvent(ctx context.Context, tc turnContext, response assistantResponse) error {
	stored := *response.response
	if strings.TrimSpace(stored.Content) == "" && len(stored.ToolCalls) > 0 {
		stored.Content = formatToolCallSummary(stored.ToolCalls)
	}
	return m.eventHandlers.HandleMessageEvent(ctx, MessageEvent{
		Message:      stored,
		SessionScope: m.sessionScope,
		Model:        m.model,
		TurnSpanID:   tc.turnSpanID,
		Kind:         response.kind,
		Usage:        response.usage,
		StartedAt:    response.startedAt,
		EndedAt:      response.endedAt,
		Input:        response.conversationInput,
		Output:       formatOutput(response.response.Content, response.response.ToolCalls),
	})
}

// emitToolResultEvents stores one message event per tool execution in the conversation history.
func (m *ConversationManager) emitToolResultEvents(ctx context.Context, tc turnContext, executions []ToolExecutionResult) error {
	for _, exec := range executions {
		if err := m.eventHandlers.HandleMessageEvent(ctx, MessageEvent{
			Message:      exec.Message,
			SessionScope: m.sessionScope,
			Model:        m.model,
			TurnSpanID:   tc.turnSpanID,
			Kind:         CallKindToolResult,
			StartedAt:    exec.StartedAt,
			EndedAt:      exec.EndedAt,
		}); err != nil {
			return fmt.Errorf("handling tool result event: %w", err)
		}
	}
	return nil
}

// emitTurnCompleted emits the final turn event and returns the assistant's response content.
func (m *ConversationManager) emitTurnCompleted(ctx context.Context, tc turnContext, totalUsage Usage, callCount int, toolCalls []ToolCall, output string) (string, error) {
	event := m.buildTurnEndEvent(tc, totalUsage, callCount, toolCalls, output, TurnStatusComplete)
	if err := m.eventHandlers.HandleTurnEndEvent(ctx, event); err != nil {
		return "", fmt.Errorf("handling turn event: %w", err)
	}
	return event.Output, nil
}

// emitTurnNotCompleted emits the turn event for a turn interrupted by the max tool call iteration limit.
func (m *ConversationManager) emitTurnNotCompleted(ctx context.Context, tc turnContext, totalUsage Usage, callCount int, toolCalls []ToolCall) (string, error) {
	event := m.buildTurnEndEvent(tc, totalUsage, callCount, toolCalls, "", TurnStatusIncomplete)
	if err := m.eventHandlers.HandleTurnEndEvent(ctx, event); err != nil {
		return "", fmt.Errorf("handling turn not completed event: %w", err)
	}
	return event.Output, nil
}

func (m *ConversationManager) buildTurnEndEvent(tc turnContext, totalUsage Usage, callCount int, toolCalls []ToolCall, output, status string) TurnEndEvent {
	return TurnEndEvent{
		TurnID:       tc.turnID,
		TurnSpanID:   tc.turnSpanID,
		StartedAt:    tc.startedAt,
		EndedAt:      time.Now(),
		SessionScope: m.sessionScope,
		Model:        m.model,
		TotalUsage:   totalUsage,
		CallCount:    callCount,
		Input:        tc.userInput,
		Output:       output,
		ToolCalls:    toolCalls,
		Status:       status,
	}
}

// formatMessagesAsInput formats the conversation messages as a readable input string for observability
// formatMessagesAsInput returns the most relevant input for observability.
// For the initial call it returns the last user message.
// For tool_result calls it returns the tool results that were fed back to the LLM.
func formatMessagesAsInput(messages []Message, systemPrompt string) string {
	if len(messages) == 0 {
		return systemPrompt
	}
	last := messages[len(messages)-1]
	if last.Role == RoleTool && len(last.ToolResults) > 0 {
		return collectContiguousToolResults(messages)
	}
	if content := findLastUserContent(messages); content != "" {
		return content
	}
	return systemPrompt
}

// collectContiguousToolResults concatenates the content of all contiguous tool
// messages at the tail of the slice, separated by newlines.
func collectContiguousToolResults(messages []Message) string {
	start := len(messages) - 1
	for start > 0 && messages[start-1].Role == RoleTool {
		start--
	}
	var b strings.Builder
	isFirst := true
	for i := start; i < len(messages); i++ {
		for _, tr := range messages[i].ToolResults {
			if !isFirst {
				b.WriteString("\n")
			}
			b.WriteString(tr.Content)
			isFirst = false
		}
	}
	return b.String()
}

// findLastUserContent returns the content of the last user message, or "" if none.
func findLastUserContent(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleUser {
			return messages[i].Content
		}
	}
	return ""
}

// formatOutput returns a human-readable output for an API call.
// When the LLM responds with tool calls instead of text, the content is empty;
// in that case we format the tool calls as JSON so Langfuse shows a meaningful output.
func formatOutput(content string, toolCalls []ToolCall) string {
	if content != "" {
		return content
	}
	if len(toolCalls) == 0 {
		return ""
	}
	var b strings.Builder
	for i, tc := range toolCalls {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, `{"tool_name": %q, "input": %s}`, tc.Name, marshalInput(tc.Input))
	}
	return b.String()
}

func marshalInput(input map[string]any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func formatToolCallSummary(toolCalls []ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	var b strings.Builder
	if len(toolCalls) == 1 {
		b.WriteString("Calling tool ")
		b.WriteString(toolCalls[0].Name)
		b.WriteByte('.')
		return b.String()
	}
	b.WriteString("Calling tools ")
	for i, tc := range toolCalls {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tc.Name)
	}
	b.WriteByte('.')
	return b.String()
}
