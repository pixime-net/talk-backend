package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ToolExecutionResult captures a single tool execution with its timing metadata.
type ToolExecutionResult struct {
	Message   Message
	StartedAt time.Time
	EndedAt   time.Time
}

// ToolExecutor handles the execution of tool calls and returns messages
// that can be added to the conversation store.
type ToolExecutor struct {
	toolsProvider func() []Tool
	maxConcurrent int
	eventHandler  EventHandler
}

// NewToolExecutor creates a new ToolExecutor with the given tools provider and concurrency limit.
func NewToolExecutor(toolsProvider func() []Tool, maxConcurrent int, eventHandler EventHandler) *ToolExecutor {
	return &ToolExecutor{toolsProvider: toolsProvider, maxConcurrent: maxConcurrent, eventHandler: eventHandler}
}

// Execute runs the given tool calls and returns the resulting messages.
// It chooses sequential or parallel execution based on the concurrency configuration.
func (e *ToolExecutor) Execute(ctx context.Context, turnID string, calls []ToolCall) ([]ToolExecutionResult, error) {
	available := e.toolsProvider()
	if len(available) == 0 {
		return nil, fmt.Errorf("model requested tool calls but no tools are registered")
	}

	if len(calls) == 1 || e.maxConcurrent <= 1 {
		return e.executeSequential(ctx, turnID, calls)
	}
	return e.executeParallel(ctx, turnID, calls)
}

// ExecuteTool executes a single tool call and returns the result.
func (e *ToolExecutor) ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	for _, t := range e.toolsProvider() {
		if t.Name() != call.Name {
			continue
		}
		out, err := t.Execute(ctx, call.Input)
		if err != nil {
			return ToolResult{}, fmt.Errorf("tool %q execution: %w", call.Name, err)
		}
		modelBytes, err := json.Marshal(out.Model)
		if err != nil {
			return ToolResult{}, fmt.Errorf("marshalling tool output for tool %q: %w", call.Name, err)
		}
		result := ToolResult{ToolCallID: call.ID, Content: string(modelBytes)}
		if out.Client != nil {
			clientBytes, err := json.Marshal(out.Client)
			if err != nil {
				return ToolResult{}, fmt.Errorf("marshalling client tool output for tool %q: %w", call.Name, err)
			}
			result.ClientContent = string(clientBytes)
		}
		return result, nil
	}
	return ToolResult{}, fmt.Errorf("unknown tool %q", call.Name)
}

func (e *ToolExecutor) executeSequential(ctx context.Context, turnID string, calls []ToolCall) ([]ToolExecutionResult, error) {
	executions := make([]ToolExecutionResult, 0, len(calls))
	for _, call := range calls {
		ex, err := e.executeSingleTool(ctx, turnID, call)
		if err != nil {
			return nil, err
		}
		executions = append(executions, ex)
	}
	return executions, nil
}

// executeSingleTool fires start/end events and runs one tool call.
func (e *ToolExecutor) executeSingleTool(ctx context.Context, turnID string, call ToolCall) (ToolExecutionResult, error) {
	startedAt := time.Now()
	if e.eventHandler != nil {
		if err := e.eventHandler.HandleToolStartEvent(ctx, ToolStartEvent{
			TurnID:    turnID,
			ToolCall:  call,
			StartedAt: startedAt,
		}); err != nil {
			return ToolExecutionResult{}, fmt.Errorf("handling tool call start: %w", err)
		}
	}
	result, execErr := e.ExecuteTool(ctx, call)
	endedAt := time.Now()
	if execErr != nil {
		result = ToolResult{ToolCallID: call.ID, Content: formatToolError(execErr)}
	}
	if e.eventHandler != nil {
		if err := e.eventHandler.HandleToolEndEvent(ctx, ToolEndEvent{
			TurnID:    turnID,
			ToolCall:  call,
			Result:    result,
			StartedAt: startedAt,
			EndedAt:   endedAt,
		}); err != nil {
			return ToolExecutionResult{}, fmt.Errorf("handling tool call end: %w", err)
		}
	}
	return ToolExecutionResult{
		Message: Message{
			Role:        RoleTool,
			ToolCalls:   []ToolCall{call},
			ToolResults: []ToolResult{result},
			TurnID:      turnID,
		},
		StartedAt: startedAt,
		EndedAt:   endedAt,
	}, nil
}

func (e *ToolExecutor) executeParallel(ctx context.Context, turnID string, calls []ToolCall) ([]ToolExecutionResult, error) {
	executions := make([]ToolExecutionResult, len(calls))
	errs := make([]error, len(calls))

	sem := make(chan struct{}, e.maxConcurrent)
	var wg sync.WaitGroup

	for i, call := range calls {
		idx, toolCall := i, call
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			ex, err := e.executeSingleTool(ctx, turnID, toolCall)
			errs[idx] = err
			executions[idx] = ex
		})
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("tool %q failed: %w", calls[i].Name, err)
		}
	}

	return executions, nil
}

// formatToolError produces a JSON error payload for a failed tool execution.
func formatToolError(err error) string {
	raw, _ := json.Marshal(map[string]any{
		"ok":    false,
		"error": map[string]string{"kind": "tool_error", "message": err.Error()},
	})
	return string(raw)
}
