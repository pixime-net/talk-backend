package openrouter

import (
	"context"
	"testing"

	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/pixime-net/talk/internal/domain"
)

func TestToSDKMessages_ToolCallsAndResults(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "What is the weather?"},
		{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{{
				ID:    "call-1",
				Name:  "get_weather",
				Input: map[string]any{"city": "Paris"},
			}},
		},
		{
			Role:        domain.RoleTool,
			ToolResults: []domain.ToolResult{{ToolCallID: "call-1", Content: `{"temperature":"20C"}`}},
		},
	}

	result, err := toSDKMessages("You are helpful.", messages, nil)
	if err != nil {
		t.Fatalf("toSDKMessages error: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected system, user, assistant, and tool messages, got %d", len(result))
	}

	assistant := result[2].ChatAssistantMessage
	if assistant == nil {
		t.Fatal("expected assistant message")
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistant.ToolCalls))
	}
	if assistant.ToolCalls[0].ID != "call-1" {
		t.Errorf("tool call ID = %q, want %q", assistant.ToolCalls[0].ID, "call-1")
	}
	if assistant.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool name = %q, want %q", assistant.ToolCalls[0].Function.Name, "get_weather")
	}
	if assistant.ToolCalls[0].Function.Arguments != `{"city":"Paris"}` {
		t.Errorf("tool arguments = %q, want %q", assistant.ToolCalls[0].Function.Arguments, `{"city":"Paris"}`)
	}

	tool := result[3].ChatToolMessage
	if tool == nil {
		t.Fatal("expected tool message")
	}
	if tool.ToolCallID != "call-1" {
		t.Errorf("tool result call ID = %q, want %q", tool.ToolCallID, "call-1")
	}
}

func TestToSDKTools(t *testing.T) {
	tools := []domain.Tool{testTool{
		name:        "get_weather",
		description: "Get the weather for a city",
		inputSchema: map[string]any{"type": "object"},
	}}

	result, err := toSDKTools(tools)
	if err != nil {
		t.Fatalf("toSDKTools error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	function := result[0].ChatFunctionToolFunction
	if function == nil {
		t.Fatal("expected function tool definition")
	}
	if function.Function.Name != "get_weather" {
		t.Errorf("tool name = %q, want %q", function.Function.Name, "get_weather")
	}
	if function.Function.Description == nil || *function.Function.Description != "Get the weather for a city" {
		t.Errorf("unexpected tool description: %v", function.Function.Description)
	}
}

func TestFromSDKResponse_ToolCall(t *testing.T) {
	response := &components.ChatResult{
		Choices: []components.ChatChoice{{
			Message: components.ChatAssistantMessage{
				ToolCalls: []components.ChatToolCall{{
					ID:   "call-1",
					Type: components.ChatToolCallTypeFunction,
					Function: components.ChatToolCallFunction{
						Name:      "get_weather",
						Arguments: `{"city":"Paris"}`,
					},
				}},
			},
		}},
		Usage: &components.ChatUsage{PromptTokens: 12, CompletionTokens: 7},
	}

	message, usage := fromSDKResponse(response)
	if message == nil {
		t.Fatal("expected assistant message")
	}
	if len(message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(message.ToolCalls))
	}
	toolCall := message.ToolCalls[0]
	if toolCall.ID != "call-1" || toolCall.Name != "get_weather" {
		t.Errorf("unexpected tool call: %+v", toolCall)
	}
	if toolCall.Input["city"] != "Paris" {
		t.Errorf("tool input city = %v, want Paris", toolCall.Input["city"])
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 7 {
		t.Errorf("unexpected usage: %+v", usage)
	}
}

type testTool struct {
	name        string
	description string
	inputSchema map[string]any
}

func (t testTool) Name() string { return t.name }

func (t testTool) Description() string { return t.description }

func (t testTool) InputSchema() (map[string]any, error) { return t.inputSchema, nil }

func (t testTool) OutputSchema() (map[string]any, error) { return nil, nil }

func (t testTool) Execute(context.Context, map[string]any) (domain.ToolOutput, error) {
	return domain.ToolOutput{}, nil
}
