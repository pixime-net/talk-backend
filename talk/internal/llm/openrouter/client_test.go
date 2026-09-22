package openrouter

import (
	"testing"

	"github.com/pixime-net/talk/internal/domain"

	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
)

func TestNewClient(t *testing.T) {
	model := domain.Model{
		Name:       "openrouter-deepseek-chat",
		APIModelID: "openrouter/deepseek/deepseek-chat",
		APIClient:  domain.APIClientOpenRouter,
	}

	client := NewClient("test-api-key", model)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.model.Name != "openrouter-deepseek-chat" {
		t.Errorf("client.model.Name = %q, want 'openrouter-deepseek-chat'", client.model.Name)
	}
}

func TestExtractContent_String(t *testing.T) {
	content := components.CreateChatAssistantMessageContentStr("Hello, world!")
	msg := components.ChatAssistantMessage{
		Content: optionalnullable.From(&content),
	}

	result := extractContent(msg)
	if result != "Hello, world!" {
		t.Errorf("extractContent = %q, want 'Hello, world!'", result)
	}
}

func TestExtractContent_ArrayOfChatContentItems(t *testing.T) {
	// Create an ArrayOfChatContentItems
	items := []components.ChatContentItems{}
	content := components.CreateChatAssistantMessageContentArrayOfChatContentItems(items)
	msg := components.ChatAssistantMessage{
		Content: optionalnullable.From(&content),
	}

	result := extractContent(msg)
	// Should return empty string for array (not implemented)
	if result != "" {
		t.Errorf("extractContent with array = %q, want empty string", result)
	}
}

func TestExtractContent_NotSet(t *testing.T) {
	var msg components.ChatAssistantMessage
	result := extractContent(msg)
	if result != "" {
		t.Errorf("extractContent with no content = %q, want empty string", result)
	}
}

func TestExtractUsage_Basic(t *testing.T) {
	chatUsage := &components.ChatUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	usage := extractUsage(chatUsage)
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", usage.OutputTokens)
	}
	if usage.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens = %d, want 0", usage.ReasoningTokens)
	}
}

func TestExtractUsage_WithReasoningTokens(t *testing.T) {
	details := components.ChatUsageCompletionTokensDetails{
		ReasoningTokens: optionalnullable.From[int64](int64Ptr(25)),
	}

	chatUsage := &components.ChatUsage{
		PromptTokens:            100,
		CompletionTokens:        50,
		TotalTokens:             150,
		CompletionTokensDetails: optionalnullable.From(&details),
	}

	usage := extractUsage(chatUsage)
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", usage.OutputTokens)
	}
	if usage.ReasoningTokens != 25 {
		t.Errorf("ReasoningTokens = %d, want 25", usage.ReasoningTokens)
	}
}

func TestExtractUsage_Nil(t *testing.T) {
	usage := extractUsage(nil)
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.ReasoningTokens != 0 {
		t.Errorf("Usage should be zero for nil input, got %+v", usage)
	}
}

func TestExtractUsage_EmptyDetails(t *testing.T) {
	var details components.ChatUsageCompletionTokensDetails
	chatUsage := &components.ChatUsage{
		PromptTokens:            100,
		CompletionTokens:        50,
		CompletionTokensDetails: optionalnullable.From(&details),
	}

	usage := extractUsage(chatUsage)
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", usage.OutputTokens)
	}
	if usage.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens should be 0 when not set, got %d", usage.ReasoningTokens)
	}
}

func TestToSDKMessages_Empty(t *testing.T) {
	messages := []domain.Message{}
	sdkMessages, err := toSDKMessages("", messages, nil)
	if err != nil {
		t.Fatalf("toSDKMessages error: %v", err)
	}
	if len(sdkMessages) != 0 {
		t.Errorf("expected 0 messages for empty input, got %d", len(sdkMessages))
	}
}

func TestToSDKMessages_SystemPromptOnly(t *testing.T) {
	messages := []domain.Message{}
	sdkMessages, err := toSDKMessages("You are helpful", messages, nil)
	if err != nil {
		t.Fatalf("toSDKMessages error: %v", err)
	}
	if len(sdkMessages) != 1 {
		t.Errorf("expected 1 message (system), got %d", len(sdkMessages))
	}
}

func TestToSDKMessages_AllRoles(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "User"},
		{Role: domain.RoleAssistant, Content: "Assistant"},
		{Role: domain.RoleTool, Content: "Tool result"},
	}

	sdkMessages, err := toSDKMessages("", messages, nil)
	if err != nil {
		t.Fatalf("toSDKMessages error: %v", err)
	}

	// Tool messages are skipped
	// So we expect 2 messages (user + assistant)
	if len(sdkMessages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(sdkMessages))
	}
}

func TestToSDKMessages_UnknownRole(t *testing.T) {
	messages := []domain.Message{
		{Role: "invalid-role", Content: "test"},
	}

	_, err := toSDKMessages("", messages, nil)
	if err == nil {
		t.Error("expected error for unknown role, got nil")
	}
}

func TestFromSDKResponse_Valid(t *testing.T) {
	content := components.CreateChatAssistantMessageContentStr("Assistant says hi")
	chatResult := &components.ChatResult{
		Choices: []components.ChatChoice{
			{
				Message: components.ChatAssistantMessage{
					Content: optionalnullable.From(&content),
				},
			},
		},
		Usage: &components.ChatUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	msg, usage := fromSDKResponse(chatResult)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.Role != domain.RoleAssistant {
		t.Errorf("message role = %v, want %v", msg.Role, domain.RoleAssistant)
	}
	if msg.Content != "Assistant says hi" {
		t.Errorf("message content = %q, want 'Assistant says hi'", msg.Content)
	}
	if usage.InputTokens != 10 {
		t.Errorf("usage.InputTokens = %d, want 10", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Errorf("usage.OutputTokens = %d, want 20", usage.OutputTokens)
	}
}

func TestFromSDKResponse_Nil(t *testing.T) {
	msg, usage := fromSDKResponse(nil)
	if msg != nil {
		t.Error("expected nil message for nil response")
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Error("expected zero usage for nil response")
	}
}

func TestFromSDKResponse_NoChoices(t *testing.T) {
	chatResult := &components.ChatResult{
		Choices: []components.ChatChoice{},
		Usage: &components.ChatUsage{
			PromptTokens:     5,
			CompletionTokens: 5,
		},
	}

	msg, usage := fromSDKResponse(chatResult)
	if msg != nil {
		t.Error("expected nil message for empty choices")
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Error("expected zero usage for empty choices")
	}
}

func TestApplyOutputLimit_MaxTokens(t *testing.T) {
	req := components.ChatRequest{}
	model := domain.Model{
		ProviderMaxOutputTokens: 1000,
		RequestMaxOutputTokens:  500,
		OutputLimitParameter:    domain.OutputLimitParameterMaxTokens,
	}

	applyOutputLimit(&req, model)

	// Check that MaxTokens is set
	if !req.MaxTokens.IsSet() {
		t.Error("MaxTokens should be set")
	}

	// Get the value
	if val, ok := req.MaxTokens.Get(); ok && val != nil {
		if *val != 500 {
			t.Errorf("MaxTokens value = %d, want 500", *val)
		}
	} else {
		t.Error("Failed to get MaxTokens value")
	}
}

func TestApplyOutputLimit_MaxCompletionTokens(t *testing.T) {
	req := components.ChatRequest{}
	model := domain.Model{
		ProviderMaxOutputTokens: 1000,
		RequestMaxOutputTokens:  500,
		OutputLimitParameter:    domain.OutputLimitParameterMaxCompletionTokens,
	}

	applyOutputLimit(&req, model)

	// Check that MaxCompletionTokens is set
	if !req.MaxCompletionTokens.IsSet() {
		t.Error("MaxCompletionTokens should be set")
	}

	// Get the value
	if val, ok := req.MaxCompletionTokens.Get(); ok && val != nil {
		if *val != 500 {
			t.Errorf("MaxCompletionTokens value = %d, want 500", *val)
		}
	} else {
		t.Error("Failed to get MaxCompletionTokens value")
	}
}

func TestApplyOutputLimit_NoLimit(t *testing.T) {
	req := components.ChatRequest{}
	model := domain.Model{
		RequestMaxOutputTokens: 0, // No limit
		OutputLimitParameter:   domain.OutputLimitParameterMaxTokens,
	}

	applyOutputLimit(&req, model)

	// Check that MaxTokens is not set
	if req.MaxTokens.IsSet() {
		t.Error("MaxTokens should not be set when limit is 0")
	}
}

func int64Ptr(x int64) *int64 {
	return &x
}
