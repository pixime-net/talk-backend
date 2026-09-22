package openrouter

import (
	"encoding/json"
	"fmt"

	"github.com/pixime-net/talk/internal/domain"

	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
)

// toSDKMessages converts domain messages to OpenRouter SDK message params.
func toSDKMessages(systemPrompt string, messages []domain.Message, tools []domain.Tool) ([]components.ChatMessages, error) {
	params := make([]components.ChatMessages, 0, len(messages)+1)

	// Add system prompt if present
	if systemPrompt != "" {
		systemContent := components.CreateChatSystemMessageContentStr(systemPrompt)
		params = append(params, components.CreateChatMessagesSystem(components.ChatSystemMessage{
			Content: systemContent,
		}))
	}

	// Convert each message
	for _, msg := range messages {
		switch msg.Role {
		case domain.RoleUser:
			userContent := components.CreateChatUserMessageContentStr(msg.Content)
			params = append(params, components.CreateChatMessagesUser(components.ChatUserMessage{
				Content: userContent,
			}))
		case domain.RoleAssistant:
			assistantMsg := components.ChatAssistantMessage{}
			if msg.Content != "" {
				assistantContent := components.CreateChatAssistantMessageContentStr(msg.Content)
				assistantMsg.Content = optionalnullable.From(&assistantContent)
			}
			for _, tc := range msg.ToolCalls {
				raw, err := json.Marshal(tc.Input)
				if err != nil {
					return nil, fmt.Errorf("marshal tool call %q arguments: %w", tc.Name, err)
				}
				assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, components.ChatToolCall{
					ID:   tc.ID,
					Type: components.ChatToolCallTypeFunction,
					Function: components.ChatToolCallFunction{
						Name:      tc.Name,
						Arguments: string(raw),
					},
				})
			}
			params = append(params, components.CreateChatMessagesAssistant(assistantMsg))
		case domain.RoleTool:
			for _, tr := range msg.ToolResults {
				toolContent := components.CreateChatToolMessageContentStr(tr.Content)
				params = append(params, components.CreateChatMessagesTool(components.ChatToolMessage{
					Content:    toolContent,
					ToolCallID: tr.ToolCallID,
				}))
			}
		default:
			return nil, fmt.Errorf("unsupported message role: %v", msg.Role)
		}
	}

	return params, nil
}

// toSDKTools converts domain tools to OpenRouter SDK tool definitions.
func toSDKTools(tools []domain.Tool) ([]components.ChatFunctionTool, error) {
	sdkTools := make([]components.ChatFunctionTool, 0, len(tools))
	for _, t := range tools {
		inputSchema, err := t.InputSchema()
		if err != nil {
			return nil, fmt.Errorf("unable to get InputSchema for tool %s: %w", t.Name(), err)
		}
		description := t.Description()
		sdkTools = append(sdkTools, components.CreateChatFunctionToolChatFunctionToolFunction(components.ChatFunctionToolFunction{
			Type: components.ChatFunctionToolTypeFunction,
			Function: components.ChatFunctionToolFunctionFunction{
				Name:        t.Name(),
				Description: &description,
				Parameters:  inputSchema,
			},
		}))
	}
	return sdkTools, nil
}

// extractContent extracts text content from ChatAssistantMessage.
// Returns empty string if content is not set.
func extractContent(msg components.ChatAssistantMessage) string {
	if !msg.Content.IsSet() {
		return ""
	}

	contentVal, ok := msg.Content.Get()
	if !ok || contentVal == nil {
		return ""
	}

	// contentVal is *ChatAssistantMessageContent
	if contentVal.Str != nil {
		return *contentVal.Str
	}

	// Some reasoning-capable models return content as multi-part items even for plain text.
	var text string
	for _, item := range contentVal.ArrayOfChatContentItems {
		if item.ChatContentText != nil {
			text += item.ChatContentText.Text
		}
	}
	return text
}

// extractReasoning extracts the reasoning/thinking text from ChatAssistantMessage, if present.
func extractReasoning(msg components.ChatAssistantMessage) string {
	if msg.Reasoning.IsSet() {
		if reasoning, ok := msg.Reasoning.Get(); ok && reasoning != nil && *reasoning != "" {
			return *reasoning
		}
	}
	return ""
}

// fromSDKResponse converts OpenRouter ChatResult to domain types.
func fromSDKResponse(resp *components.ChatResult) (*domain.Message, domain.Usage) {
	if resp == nil || len(resp.Choices) == 0 {
		return nil, domain.Usage{}
	}

	choice := resp.Choices[0]
	content := extractContent(choice.Message)

	msg := &domain.Message{
		Role:     domain.RoleAssistant,
		Content:  content,
		Thinking: extractReasoning(choice.Message),
	}
	for _, tc := range choice.Message.ToolCalls {
		var input map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
		msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	usage := extractUsage(resp.Usage)
	return msg, usage
}

// extractUsage converts OpenRouter ChatUsage to domain.Usage.
func extractUsage(chatUsage *components.ChatUsage) domain.Usage {
	if chatUsage == nil {
		return domain.Usage{}
	}

	usage := domain.Usage{
		InputTokens:  chatUsage.PromptTokens,
		OutputTokens: chatUsage.CompletionTokens,
	}

	// Extract reasoning tokens if available
	if chatUsage.CompletionTokensDetails.IsSet() {
		if details, ok := chatUsage.CompletionTokensDetails.Get(); ok && details != nil {
			reasoningTokens := details.GetReasoningTokens()
			if reasoningTokens.IsSet() {
				if valPtr, ok := reasoningTokens.Get(); ok && valPtr != nil {
					usage.ReasoningTokens = *valPtr
				}
			}
		}
	}

	return usage
}
