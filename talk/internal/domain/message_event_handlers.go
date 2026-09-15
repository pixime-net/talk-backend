package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// GenerateTraceID generates a random 16-byte trace ID as a hex string.
func GenerateTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateSpanID generates a random 8-byte span ID as a hex string.
func GenerateSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateSessionID generates a random UUID v4 as a formatted string.
func GenerateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122

	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}

// Usage holds token consumption for a single LLM API call.
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64 // tokens served from prompt cache
	CacheWriteTokens int64 // tokens written to prompt cache (Anthropic only)
	ReasoningTokens  int64 // tokens used for reasoning/thinking
}

// Add returns the sum of two Usage values.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:      u.InputTokens + other.InputTokens,
		OutputTokens:     u.OutputTokens + other.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens + other.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + other.CacheWriteTokens,
		ReasoningTokens:  u.ReasoningTokens + other.ReasoningTokens,
	}
}

// CallKind classifies the type of LLM call within a conversation turn.
type CallKind string

const (
	// CallKindInitial is the first call of a turn (user → assistant).
	CallKindInitial CallKind = "initial"
	// CallKindToolResult is a subsequent call after tool execution.
	CallKindToolResult CallKind = "tool_result"
)

// APICallEvent describes a single LLM call payload.
type APICallEvent struct {
	StartedAt time.Time // When the API call started
	EndedAt   time.Time // When the API call completed
	Input     string    // The input prompt for this API call
	Output    string    // The response content from the model
}

// MessageEvent is emitted for each message produced during a turn.
type MessageEvent struct {
	Message
	SessionScope SessionScope // Session and user identifier shared across the CLI session
	Model        Model        // model identifier
	TurnSpanID   string       // 8-byte turn span identifier
	Kind         CallKind     // initial, tool_result
	Usage        Usage        // usage metrics
	StartedAt    time.Time    // When the API call started
	EndedAt      time.Time    // When the API call completed
	APICall      APICallEvent // Input/output payload for this API call
}

// TurnEvent is emitted once at the end of a full Chat() turn.
type TurnEvent struct {
	TurnID          string       // Trace ID shared with child API call spans
	TurnSpanID      string       // Span ID for this turn (parent of API call spans)
	StartedAt       time.Time    // When the conversation turn started
	EndedAt         time.Time    // When the conversation turn completed
	SessionScope    SessionScope // Session and user identifier shared across the CLI session
	Model           Model
	TotalUsage      Usage
	CallCount       int
	Input           string     // The original user question
	Output          string     // The final assistant response
	ToolCalls       []ToolCall // All tool calls made during this turn
	Status          string     // "complete" (default) or "incomplete" (max iterations reached)
	InterruptID     string     // Unique ID of the emitted interrupt (set by handler)
	InterruptReason string     // Reason for the interrupt (e.g. "talk:max_iterations")
	InterruptState  string     // Lifecycle state: "open", "resolved", "cancelled"
}

// ToolCallEvent is emitted when a tool call is about to be executed.
type ToolCallEvent struct {
	TurnID    string
	ToolCall  ToolCall
	StartedAt time.Time
}

// ToolCallEndEvent is emitted when a tool call has completed execution.
type ToolCallEndEvent struct {
	TurnID    string
	ToolCall  ToolCall
	Result    ToolResult
	StartedAt time.Time
	EndedAt   time.Time
}

// MessageEventHandler receives all conversation lifecycle events:
// messages, turns, and tool calls.
type MessageEventHandler interface {
	HandleMessageEvent(ctx context.Context, event MessageEvent) error
	HandleTurnEvent(ctx context.Context, event TurnEvent) error
	HandleToolCallStart(ctx context.Context, event ToolCallEvent) error
	HandleToolCallEnd(ctx context.Context, event ToolCallEndEvent) error
}

// MessageEventHandlers executes handlers by sequential phases, with parallel
// execution inside each phase.
type MessageEventHandlers struct {
	phases [][]MessageEventHandler
}

// NewMessageEventHandlers creates a phased event handler pipeline.
func NewMessageEventHandlers(phases [][]MessageEventHandler) *MessageEventHandlers {
	return &MessageEventHandlers{phases: phases}
}

// HandleMessageEvent dispatches one message event through all phases.
func (h *MessageEventHandlers) HandleMessageEvent(ctx context.Context, event MessageEvent) error {
	return h.runPhases(func(handler MessageEventHandler) error {
		return handler.HandleMessageEvent(ctx, event)
	})
}

// HandleTurnEvent dispatches one turn event through all phases.
func (h *MessageEventHandlers) HandleTurnEvent(ctx context.Context, event TurnEvent) error {
	return h.runPhases(func(handler MessageEventHandler) error {
		return handler.HandleTurnEvent(ctx, event)
	})
}

// HandleToolCallStart dispatches one tool call start event through all phases.
func (h *MessageEventHandlers) HandleToolCallStart(ctx context.Context, event ToolCallEvent) error {
	return h.runPhases(func(handler MessageEventHandler) error {
		return handler.HandleToolCallStart(ctx, event)
	})
}

// HandleToolCallEnd dispatches one tool call end event through all phases.
func (h *MessageEventHandlers) HandleToolCallEnd(ctx context.Context, event ToolCallEndEvent) error {
	return h.runPhases(func(handler MessageEventHandler) error {
		return handler.HandleToolCallEnd(ctx, event)
	})
}

func (h *MessageEventHandlers) runPhases(call func(handler MessageEventHandler) error) error {
	if h == nil || len(h.phases) == 0 {
		return nil
	}
	for _, phase := range h.phases {
		if err := h.runPhase(phase, call); err != nil {
			return err
		}
	}
	return nil
}

// runPhase executes call against all handlers in a single phase concurrently.
// Panics in handlers are recovered and converted to errors.
func (h *MessageEventHandlers) runPhase(phase []MessageEventHandler, call func(MessageEventHandler) error) error {
	if len(phase) == 0 {
		return nil
	}
	var (
		mu  sync.Mutex
		err error
		wg  sync.WaitGroup
	)
	for _, handler := range phase {
		hdl := handler
		wg.Go(func() {
			defer func() {
				if recover() != nil {
					mu.Lock()
					err = errors.Join(err, errors.New("message event handler panic"))
					mu.Unlock()
				}
			}()
			if callErr := call(hdl); callErr != nil {
				mu.Lock()
				err = errors.Join(err, callErr)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return err
}

// NoOpMessageEventHandler silently discards all events.
type NoOpMessageEventHandler struct{}

func (NoOpMessageEventHandler) HandleMessageEvent(context.Context, MessageEvent) error   { return nil }
func (NoOpMessageEventHandler) HandleTurnEvent(context.Context, TurnEvent) error         { return nil }
func (NoOpMessageEventHandler) HandleToolCallStart(context.Context, ToolCallEvent) error { return nil }
func (NoOpMessageEventHandler) HandleToolCallEnd(context.Context, ToolCallEndEvent) error {
	return nil
}
