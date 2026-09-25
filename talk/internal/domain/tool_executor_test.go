package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// mockTool is a mock implementation of the Tool interface for testing.
type mockTool struct {
	name        string
	description string
	executeFunc func(ctx context.Context, input map[string]any) (map[string]any, error)
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.description }
func (m *mockTool) InputSchema() (map[string]any, error) {
	return map[string]any{"type": "object"}, nil
}
func (m *mockTool) OutputSchema() (map[string]any, error) {
	return map[string]any{"type": "object"}, nil
}
func (m *mockTool) Execute(ctx context.Context, input map[string]any) (ToolOutput, error) {
	if m.executeFunc != nil {
		out, err := m.executeFunc(ctx, input)
		return ToolOutput{Model: out}, err
	}
	return ToolOutput{Model: map[string]any{"result": "success"}}, nil
}

func TestToolExecutor_ExecuteTool(t *testing.T) {
	tests := []struct {
		name        string
		tools       []Tool
		call        ToolCall
		wantErr     bool
		errContains string
	}{
		{
			name: "successful tool execution",
			tools: []Tool{&mockTool{
				name: "test-tool",
				executeFunc: func(ctx context.Context, input map[string]any) (map[string]any, error) {
					return map[string]any{"result": "hello"}, nil
				},
			}},
			call:    ToolCall{ID: "call-1", Name: "test-tool", Input: map[string]any{"param": "value"}},
			wantErr: false,
		},
		{
			name: "unknown tool returns error",
			tools: []Tool{&mockTool{
				name: "other-tool",
			}},
			call:        ToolCall{ID: "call-1", Name: "unknown-tool", Input: map[string]any{}},
			wantErr:     true,
			errContains: "unknown tool",
		},
		{
			name: "tool execution error",
			tools: []Tool{&mockTool{
				name: "error-tool",
				executeFunc: func(ctx context.Context, input map[string]any) (map[string]any, error) {
					return nil, context.Canceled
				},
			}},
			call:        ToolCall{ID: "call-1", Name: "error-tool", Input: map[string]any{}},
			wantErr:     true,
			errContains: "execution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewToolExecutor(func() []Tool { return tt.tools }, 1, nil)
			result, err := executor.ExecuteTool(context.Background(), tt.call)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error should contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.ToolCallID != tt.call.ID {
				t.Errorf("ToolCallID mismatch: got %q, want %q", result.ToolCallID, tt.call.ID)
			}
		})
	}
}

func TestToolExecutor_Execute_NoToolsRegistered(t *testing.T) {
	executor := NewToolExecutor(func() []Tool { return nil }, 1, nil)
	calls := []ToolCall{
		{ID: "call-1", Name: "ghost-tool", Input: map[string]any{}},
	}

	_, err := executor.Execute(context.Background(), "turn-1", calls)
	if err == nil {
		t.Fatal("expected error when no tools are registered")
	}
	if !strings.Contains(err.Error(), "no tools are registered") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestToolExecutor_Execute_Sequential(t *testing.T) {
	tools := []Tool{&mockTool{
		name: "test-tool",
		executeFunc: func(ctx context.Context, input map[string]any) (map[string]any, error) {
			return map[string]any{"result": input["value"]}, nil
		},
	}}

	executor := NewToolExecutor(func() []Tool { return tools }, 1, nil)
	calls := []ToolCall{
		{ID: "call-1", Name: "test-tool", Input: map[string]any{"value": "first"}},
		{ID: "call-2", Name: "test-tool", Input: map[string]any{"value": "second"}},
	}

	executions, err := executor.Execute(context.Background(), "turn-123", calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executions) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(executions))
	}

	for i, exec := range executions {
		msg := exec.Message
		if exec.StartedAt.IsZero() {
			t.Errorf("execution %d: StartedAt should not be zero", i)
		}
		if exec.EndedAt.IsZero() {
			t.Errorf("execution %d: EndedAt should not be zero", i)
		}
		if exec.EndedAt.Before(exec.StartedAt) {
			t.Errorf("execution %d: EndedAt should be >= StartedAt", i)
		}
		if msg.Role != RoleTool {
			t.Errorf("message %d: expected RoleTool, got %v", i, msg.Role)
		}
		if msg.TurnID != "turn-123" {
			t.Errorf("message %d: expected TurnID 'turn-123', got %q", i, msg.TurnID)
		}
		if len(msg.ToolCalls) != 1 {
			t.Errorf("message %d: expected 1 ToolCall, got %d", i, len(msg.ToolCalls))
		}
		if len(msg.ToolCalls) == 1 {
			if msg.ToolCalls[0].ID != calls[i].ID {
				t.Errorf("message %d: expected ToolCall ID %q, got %q", i, calls[i].ID, msg.ToolCalls[0].ID)
			}
			if msg.ToolCalls[0].Name != calls[i].Name {
				t.Errorf("message %d: expected ToolCall name %q, got %q", i, calls[i].Name, msg.ToolCalls[0].Name)
			}
		}
		if len(msg.ToolResults) != 1 {
			t.Errorf("message %d: expected 1 ToolResult, got %d", i, len(msg.ToolResults))
		}
	}
}

func TestToolExecutor_Execute_Error(t *testing.T) {
	tools := []Tool{&mockTool{
		name: "error-tool",
		executeFunc: func(ctx context.Context, input map[string]any) (map[string]any, error) {
			return nil, context.Canceled
		},
	}}

	executor := NewToolExecutor(func() []Tool { return tools }, 1, nil)
	calls := []ToolCall{
		{ID: "call-1", Name: "error-tool", Input: map[string]any{}},
	}

	executions, err := executor.Execute(context.Background(), "turn-123", calls)
	if err != nil {
		t.Fatalf("expected resilient execution, got error: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(executions))
	}
	content := executions[0].Message.ToolResults[0].Content
	if !strings.Contains(content, "tool_error") {
		t.Errorf("expected tool_error in result, got: %s", content)
	}
}

func TestToolExecutor_Execute_Parallel(t *testing.T) {
	tools := []Tool{&mockTool{
		name: "test-tool",
		executeFunc: func(ctx context.Context, input map[string]any) (map[string]any, error) {
			return map[string]any{"result": input["value"]}, nil
		},
	}}

	executor := NewToolExecutor(func() []Tool { return tools }, 2, nil)
	calls := []ToolCall{
		{ID: "call-1", Name: "test-tool", Input: map[string]any{"value": "parallel-1"}},
		{ID: "call-2", Name: "test-tool", Input: map[string]any{"value": "parallel-2"}},
		{ID: "call-3", Name: "test-tool", Input: map[string]any{"value": "parallel-3"}},
	}

	executions, err := executor.Execute(context.Background(), "turn-456", calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executions) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(executions))
	}

	for i, exec := range executions {
		msg := exec.Message
		if exec.StartedAt.IsZero() {
			t.Errorf("execution %d: StartedAt should not be zero", i)
		}
		if exec.EndedAt.IsZero() {
			t.Errorf("execution %d: EndedAt should not be zero", i)
		}
		if exec.EndedAt.Before(exec.StartedAt) {
			t.Errorf("execution %d: EndedAt should be >= StartedAt", i)
		}
		if msg.Role != RoleTool {
			t.Errorf("message %d: expected RoleTool, got %v", i, msg.Role)
		}
		if msg.TurnID != "turn-456" {
			t.Errorf("message %d: expected TurnID 'turn-456', got %q", i, msg.TurnID)
		}
		if len(msg.ToolCalls) != 1 {
			t.Errorf("message %d: expected 1 ToolCall, got %d", i, len(msg.ToolCalls))
		}
		if len(msg.ToolCalls) == 1 {
			if msg.ToolCalls[0].ID != calls[i].ID {
				t.Errorf("message %d: expected ToolCall ID %q, got %q", i, calls[i].ID, msg.ToolCalls[0].ID)
			}
			if msg.ToolCalls[0].Name != calls[i].Name {
				t.Errorf("message %d: expected ToolCall name %q, got %q", i, calls[i].Name, msg.ToolCalls[0].Name)
			}
		}
	}
}

func TestToolExecutor_Execute_Parallel_Error(t *testing.T) {
	tools := []Tool{&mockTool{
		name: "error-tool",
		executeFunc: func(ctx context.Context, input map[string]any) (map[string]any, error) {
			return nil, context.Canceled
		},
	}}

	executor := NewToolExecutor(func() []Tool { return tools }, 2, nil)
	calls := []ToolCall{
		{ID: "call-1", Name: "error-tool", Input: map[string]any{}},
		{ID: "call-2", Name: "error-tool", Input: map[string]any{}},
	}

	executions, err := executor.Execute(context.Background(), "turn-789", calls)
	if err != nil {
		t.Fatalf("expected resilient execution, got error: %v", err)
	}
	if len(executions) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(executions))
	}
	for i, exec := range executions {
		content := exec.Message.ToolResults[0].Content
		if !strings.Contains(content, "tool_error") {
			t.Errorf("execution %d: expected tool_error in result, got: %s", i, content)
		}
	}
}

func TestToolExecutor_ResultContentIsJSON(t *testing.T) {
	tools := []Tool{&mockTool{
		name: "complex-tool",
		executeFunc: func(ctx context.Context, input map[string]any) (map[string]any, error) {
			return map[string]any{
				"nested": map[string]any{"key": "value"},
				"array":  []string{"a", "b", "c"},
			}, nil
		},
	}}

	executor := NewToolExecutor(func() []Tool { return tools }, 1, nil)
	calls := []ToolCall{
		{ID: "call-1", Name: "complex-tool", Input: map[string]any{}},
	}

	executions, err := executor.Execute(context.Background(), "turn-1", calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(executions))
	}

	// Verify the content is valid JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(executions[0].Message.ToolResults[0].Content), &result); err != nil {
		t.Errorf("ToolResult.Content is not valid JSON: %v", err)
	}
}

// unmarshalableTool returns a value that cannot be JSON marshaled
type unmarshalableTool struct {
	mockTool
}

func (m *unmarshalableTool) InputSchema() (map[string]any, error) {
	return map[string]any{"type": "object"}, nil
}

func (m *unmarshalableTool) OutputSchema() (map[string]any, error) {
	return map[string]any{"type": "object"}, nil
}

func (m *unmarshalableTool) Execute(ctx context.Context, input map[string]any) (ToolOutput, error) {
	// Return a value with a channel which cannot be JSON marshaled
	return ToolOutput{Model: map[string]any{"channel": make(chan int)}}, nil
}

func TestToolExecutor_JSONMarshalError(t *testing.T) {
	tools := []Tool{&unmarshalableTool{mockTool{name: "bad-tool"}}}

	executor := NewToolExecutor(func() []Tool { return tools }, 1, nil)
	call := ToolCall{ID: "call-1", Name: "bad-tool", Input: map[string]any{}}

	_, err := executor.ExecuteTool(context.Background(), call)
	if err == nil {
		t.Error("expected error for unmarshalable result, got nil")
	}
	if !strings.Contains(err.Error(), "marshalling tool output") {
		t.Errorf("expected marshalling error, got: %v", err)
	}
}

type recordingToolCallHandler struct {
	mu        sync.Mutex
	events    []ToolStartEvent
	endEvents []ToolEndEvent
}

func (h *recordingToolCallHandler) HandleMessageEvent(_ context.Context, _ MessageEvent) error {
	return nil
}

func (h *recordingToolCallHandler) HandleTurnEndEvent(_ context.Context, _ TurnEndEvent) error {
	return nil
}

func (h *recordingToolCallHandler) HandleToolStartEvent(_ context.Context, event ToolStartEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
	return nil
}

func (h *recordingToolCallHandler) HandleToolEndEvent(_ context.Context, event ToolEndEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.endEvents = append(h.endEvents, event)
	return nil
}

func TestToolExecutor_EmitToolCallEvent(t *testing.T) {
	tools := []Tool{&mockTool{name: "test-tool"}}
	handler := &recordingToolCallHandler{}
	executor := NewToolExecutor(func() []Tool { return tools }, 1, handler)

	calls := []ToolCall{
		{ID: "call-1", Name: "test-tool", Input: map[string]any{"x": 1}},
		{ID: "call-2", Name: "test-tool", Input: map[string]any{"x": 2}},
	}

	_, err := executor.Execute(context.Background(), "turn-42", calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.events) != 2 {
		t.Fatalf("expected 2 tool call events, got %d", len(handler.events))
	}

	for i, evt := range handler.events {
		if evt.TurnID != "turn-42" {
			t.Errorf("event %d: expected turn id turn-42, got %q", i, evt.TurnID)
		}
		if evt.ToolCall.ID != calls[i].ID {
			t.Errorf("event %d: expected tool call id %q, got %q", i, calls[i].ID, evt.ToolCall.ID)
		}
		if evt.StartedAt.IsZero() {
			t.Errorf("event %d: expected non-zero StartedAt", i)
		}
	}

	// Assert end events are emitted for each tool call.
	if len(handler.endEvents) != 2 {
		t.Fatalf("expected 2 tool call end events, got %d", len(handler.endEvents))
	}
	for i, endEvt := range handler.endEvents {
		if endEvt.TurnID != "turn-42" {
			t.Errorf("end event %d: expected turn id turn-42, got %q", i, endEvt.TurnID)
		}
		if endEvt.ToolCall.ID != calls[i].ID {
			t.Errorf("end event %d: expected tool call id %q, got %q", i, calls[i].ID, endEvt.ToolCall.ID)
		}
		if endEvt.Result.ToolCallID != calls[i].ID {
			t.Errorf("end event %d: result tool call id = %q, want %q", i, endEvt.Result.ToolCallID, calls[i].ID)
		}
		if endEvt.StartedAt.IsZero() || endEvt.EndedAt.IsZero() {
			t.Errorf("end event %d: expected non-zero timing", i)
		}
		if endEvt.EndedAt.Before(endEvt.StartedAt) {
			t.Errorf("end event %d: EndedAt before StartedAt", i)
		}
	}
}

func TestToolExecutor_EmitToolCallEvent_Parallel(t *testing.T) {
	tools := []Tool{&mockTool{name: "test-tool"}}
	handler := &recordingToolCallHandler{}
	executor := NewToolExecutor(func() []Tool { return tools }, 4, handler)

	calls := []ToolCall{
		{ID: "p-1", Name: "test-tool", Input: map[string]any{"x": 1}},
		{ID: "p-2", Name: "test-tool", Input: map[string]any{"x": 2}},
		{ID: "p-3", Name: "test-tool", Input: map[string]any{"x": 3}},
	}

	_, err := executor.Execute(context.Background(), "turn-p", calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.events) != 3 {
		t.Fatalf("expected 3 start events, got %d", len(handler.events))
	}
	if len(handler.endEvents) != 3 {
		t.Fatalf("expected 3 end events, got %d", len(handler.endEvents))
	}

	// Verify each end event has valid timing and matching tool call ID.
	endByID := make(map[string]ToolEndEvent)
	for _, e := range handler.endEvents {
		endByID[e.ToolCall.ID] = e
	}
	for _, call := range calls {
		endEvt, ok := endByID[call.ID]
		if !ok {
			t.Errorf("missing end event for tool call %q", call.ID)
			continue
		}
		if endEvt.Result.ToolCallID != call.ID {
			t.Errorf("end event for %q: result id = %q", call.ID, endEvt.Result.ToolCallID)
		}
		if endEvt.EndedAt.Before(endEvt.StartedAt) {
			t.Errorf("end event for %q: EndedAt before StartedAt", call.ID)
		}
	}
}

func TestToolExecutor_ToolErrorProducesResult(t *testing.T) {
	failTool := &mockTool{
		name: "fail-tool",
		executeFunc: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	tools := []Tool{failTool}
	handler := &recordingToolCallHandler{}
	executor := NewToolExecutor(func() []Tool { return tools }, 1, handler)

	calls := []ToolCall{{ID: "call-err", Name: "fail-tool", Input: map[string]any{}}}

	results, err := executor.Execute(context.Background(), "turn-err", calls)
	if err != nil {
		t.Fatalf("expected no error (resilient), got: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Verify error result content.
	content := results[0].Message.ToolResults[0].Content
	if !strings.Contains(content, "tool_error") {
		t.Errorf("expected error result to contain tool_error, got: %s", content)
	}

	// End event must still be emitted.
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if len(handler.endEvents) != 1 {
		t.Fatalf("expected 1 end event on error, got %d", len(handler.endEvents))
	}
	if handler.endEvents[0].ToolCall.ID != "call-err" {
		t.Errorf("end event tool call id = %q, want %q", handler.endEvents[0].ToolCall.ID, "call-err")
	}
}
