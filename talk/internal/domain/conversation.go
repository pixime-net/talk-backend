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
	llmClient           LlmClient
	model               Model
	sessionScope        SessionScope
	messageEventHandler MessageEventHandler
	messageRepository   MessageRepository
	promptProvider      PromptProvider
	toolsProvider       func() []Tool
	contextBuilder      *ContextBuilder
	toolExecutor        *ToolExecutor
	thinkingEffort      ThinkingEffort
}

// ConversationManagerConfig groups all parameters for creating a ConversationManager.
type ConversationManagerConfig struct {
	Client              LlmClient
	Model               Model
	SessionScope        SessionScope
	MessageEventHandler MessageEventHandler
	MessageRepository   MessageRepository
	SessionRepository   SessionRepository
	PromptProvider      PromptProvider
	Tools               func() []Tool
	MaxConcurrentTools  int
	ContextFullTurns    int
}

// NewConversationManager creates a ConversationManager.
func NewConversationManager(cfg ConversationManagerConfig) *ConversationManager {
	messageHandler := cfg.MessageEventHandler
	if messageHandler == nil {
		messageHandler = NoOpMessageEventHandler{}
	}

	return &ConversationManager{
		sessionScope:        cfg.SessionScope,
		llmClient:           cfg.Client,
		model:               cfg.Model,
		messageEventHandler: messageHandler,
		messageRepository:   cfg.MessageRepository,
		promptProvider:      cfg.PromptProvider,
		toolsProvider:       cfg.Tools,
		contextBuilder:      NewContextBuilder(cfg.MessageRepository, cfg.SessionRepository, cfg.SessionScope.SessionID(), cfg.ContextFullTurns),
		toolExecutor:        NewToolExecutor(cfg.Tools, cfg.MaxConcurrentTools, messageHandler),
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

	// turnID is used to correlate all messages, API calls, and tool calls for this conversation turn in observability.
	turnID := GenerateTraceID()
	// turnSpanID is used to correlate all events for this conversation turn in observability. It is the parent span for all API call spans in this turn.
	turnSpanID := GenerateSpanID()
	turnStartedAt := time.Now()
	model := m.model
	// Store the user message in the conversation history before processing to ensure it's included in the context
	// for the first API call and in observability.
	if err := m.messageEventHandler.HandleMessageEvent(ctx, MessageEvent{
		Message:      Message{Role: RoleUser, Content: userInput, TurnID: turnID},
		SessionScope: m.sessionScope,
		Model:        model,
		TurnSpanID:   turnSpanID,
		Kind:         CallKindInitial,
		StartedAt:    turnStartedAt,
	}); err != nil {
		return "", fmt.Errorf("handling user message event: %w", err)
	}
	var totalUsage Usage
	allToolCalls := make([]ToolCall, 0, 8) // pre-allocate to avoid repeated growth
	var lastCallEndedAt time.Time
	callCount := 0
	// kind tracks whether the API call is the initial LLM call or a subsequent call after tool results, for observability purposes.
	kind := CallKindInitial

	// Resolve tools once for the entire turn — they don't change between iterations.
	tools := m.toolsProvider()

	for range maxToolCalls {
		// Get the current conversation context for the API call input
		messages := m.contextBuilder.BuildContextMessages(ctx, turnID)
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
		lastCallEndedAt = time.Now()

		totalUsage = totalUsage.Add(usage)
		allToolCalls = append(allToolCalls, response.ToolCalls...)
		callCount++

		response.TurnID = turnID
		if err := m.storeAssistantResponse(ctx, assistantResponse{
			response:          response,
			model:             model,
			turnSpanID:        turnSpanID,
			kind:              kind,
			usage:             usage,
			callStartedAt:     callStartedAt,
			callEndedAt:       lastCallEndedAt,
			conversationInput: conversationInput,
		}); err != nil {
			return "", fmt.Errorf("handling assistant message event: %w", err)
		}

		kind = CallKindToolResult

		// If the model responded without tool calls, the turn is complete.
		if len(response.ToolCalls) == 0 {
			return m.finishTurn(ctx, TurnEvent{
				TurnID:       turnID,
				TurnSpanID:   turnSpanID,
				StartedAt:    turnStartedAt,
				EndedAt:      time.Now(),
				SessionScope: m.sessionScope,
				Model:        model,
				TotalUsage:   totalUsage,
				CallCount:    callCount,
				Input:        userInput,
				Output:       response.Content,
				ToolCalls:    allToolCalls,
			})
		}

		toolExecutions, err := m.toolExecutor.Execute(ctx, turnID, response.ToolCalls)
		if err != nil {
			return "", err
		}
		if err := m.storeToolResultEvents(ctx, toolExecutions, model, turnSpanID); err != nil {
			return "", err
		}
	}

	// Persist the incomplete turn before returning the iteration limit error.
	if err := m.messageEventHandler.HandleTurnEvent(ctx, TurnEvent{
		TurnID:       turnID,
		TurnSpanID:   turnSpanID,
		StartedAt:    turnStartedAt,
		EndedAt:      time.Now(),
		SessionScope: m.sessionScope,
		Model:        model,
		TotalUsage:   totalUsage,
		CallCount:    callCount,
		Input:        userInput,
		Output:       "",
		ToolCalls:    allToolCalls,
		Status:       TurnStatusIncomplete,
	}); err != nil {
		slog.Error("persisting incomplete turn on max iterations", slog.String("turn_id", turnID), slog.String("error", err.Error()))
	}

	return "", ErrMaxToolIterations
}

type assistantResponse struct {
	response          *Message
	model             Model
	turnSpanID        string
	kind              CallKind
	usage             Usage
	callStartedAt     time.Time
	callEndedAt       time.Time
	conversationInput string
}

// storeAssistantResponse stores the assistant message in the conversation history.
// When the model responds with only tool calls (empty text content), it substitutes
// the content with a human-readable tool-call summary for observability.
func (m *ConversationManager) storeAssistantResponse(ctx context.Context, response assistantResponse) error {
	stored := *response.response
	if strings.TrimSpace(stored.Content) == "" && len(stored.ToolCalls) > 0 {
		stored.Content = formatToolCallSummary(stored.ToolCalls)
	}
	return m.messageEventHandler.HandleMessageEvent(ctx, MessageEvent{
		Message:      stored,
		SessionScope: m.sessionScope,
		Model:        response.model,
		TurnSpanID:   response.turnSpanID,
		Kind:         response.kind,
		Usage:        response.usage,
		StartedAt:    response.callStartedAt,
		EndedAt:      response.callEndedAt,
		APICall: APICallEvent{
			StartedAt: response.callStartedAt,
			EndedAt:   response.callEndedAt,
			Input:     response.conversationInput,
			Output:    formatAPICallOutput(response.response.Content, response.response.ToolCalls),
		},
	})
}

// storeToolResultEvents stores one message event per tool execution in the conversation history.
func (m *ConversationManager) storeToolResultEvents(ctx context.Context, executions []ToolExecutionResult, model Model, turnSpanID string) error {
	for _, exec := range executions {
		if err := m.messageEventHandler.HandleMessageEvent(ctx, MessageEvent{
			Message:      exec.Message,
			SessionScope: m.sessionScope,
			Model:        model,
			TurnSpanID:   turnSpanID,
			Kind:         CallKindToolResult,
			StartedAt:    exec.StartedAt,
			EndedAt:      exec.EndedAt,
		}); err != nil {
			return fmt.Errorf("handling tool result event: %w", err)
		}
	}
	return nil
}

// finishTurn emits the final turn event and returns the assistant's response content.
func (m *ConversationManager) finishTurn(ctx context.Context, event TurnEvent) (string, error) {
	if err := m.messageEventHandler.HandleTurnEvent(ctx, event); err != nil {
		return "", fmt.Errorf("handling turn event: %w", err)
	}
	return event.Output, nil
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

// formatAPICallOutput returns a human-readable output for an API call.
// When the LLM responds with tool calls instead of text, the content is empty;
// in that case we format the tool calls as JSON so Langfuse shows a meaningful output.
func formatAPICallOutput(content string, toolCalls []ToolCall) string {
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
