package openrouter

import (
	"context"
	"fmt"

	"github.com/pixime-net/talk/internal/domain"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
)

// OpenRouterClient implements domain.LlmClient using the OpenRouter API.
type OpenRouterClient struct {
	sdk   *openrouter.OpenRouter
	model domain.Model
}

// NewClient creates an OpenRouter Client.
func NewClient(apiKey string, model domain.Model) *OpenRouterClient {
	sdkOptions := []openrouter.SDKOption{openrouter.WithSecurity(apiKey)}
	sdk := openrouter.New(sdkOptions...)
	return &OpenRouterClient{sdk: sdk, model: model}
}

var _ domain.LlmClient = (*OpenRouterClient)(nil) // ensure Client implements domain.LlmClient

// Complete sends the conversation to the OpenRouter API and returns the response with token usage.
func (c *OpenRouterClient) Complete(ctx context.Context, systemPrompt string, messages []domain.Message, tools []domain.Tool, options domain.CompletionOptions) (*domain.Message, domain.Usage, error) {
	// Convert domain messages to OpenRouter messages
	sdkMessages, err := toSDKMessages(systemPrompt, messages, tools)
	if err != nil {
		return nil, domain.Usage{}, fmt.Errorf("openrouter completion: %w", err)
	}

	// Create chat request - Model is *string, so we need to get pointer
	modelID := c.model.APIModelID
	req := components.ChatRequest{
		Model:    &modelID,
		Messages: sdkMessages,
	}

	thinkingActive := c.model.ThinkingStyle == domain.ThinkingStyleEffort && options.ThinkingEffort != "" && options.ThinkingEffort != domain.ThinkingOff

	if len(tools) > 0 {
		sdkTools, err := toSDKTools(tools)
		if err != nil {
			return nil, domain.Usage{}, fmt.Errorf("openrouter completion: %w", err)
		}
		req.Tools = sdkTools
		// Restrict routing to providers that natively support tool calling; otherwise some
		// providers silently emulate tool calls as unparsed text in the message content.
		// Skipped when thinking is active: requiring both tools and reasoning support
		// narrows routing to providers that silently drop the reasoning output.
		if !thinkingActive {
			requireParams := true
			req.Provider = optionalnullable.From(&components.ProviderPreferences{
				RequireParameters: optionalnullable.From(&requireParams),
			})
			effort := components.ChatRequestEffort(components.ChatRequestEffortNone)
			req.Reasoning = &components.ChatRequestReasoning{
				Effort: optionalnullable.From(&effort),
			}
		}
	}

	if thinkingActive {
		effort := components.ChatRequestEffort(thinkingEffort(options.ThinkingEffort))
		summary := components.ChatReasoningSummaryVerbosityEnumAuto
		req.Reasoning = &components.ChatRequestReasoning{
			Effort:  optionalnullable.From(&effort),
			Summary: optionalnullable.From(&summary),
		}

	}

	// Apply output limit if specified
	applyOutputLimit(&req, c.model)

	// Make API call using client.Chat.Send
	// Signature: Send(ctx, req, metadata, ...options)
	resp, err := c.sdk.Chat.Send(ctx, req, nil)
	if err != nil {
		return nil, domain.Usage{}, fmt.Errorf("openrouter completion: %w", err)
	}

	// Check response type
	if resp.ChatResult == nil {
		return nil, domain.Usage{}, fmt.Errorf("openrouter completion: expected ChatResult response")
	}

	// Extract response from ChatResult
	msg, usage := fromSDKResponse(resp.ChatResult)
	return msg, usage, nil
}

// applyOutputLimit applies output token limits to the request based on model configuration.
func applyOutputLimit(req *components.ChatRequest, model domain.Model) {
	limit := model.EffectiveOutputLimit()
	if limit <= 0 {
		return
	}

	// Create OptionalNullable from limit value
	limitValue := limit // Create a copy to get address
	limitOpt := optionalnullable.From(&limitValue)

	switch model.OutputLimitParameter {
	case domain.OutputLimitParameterMaxCompletionTokens:
		req.MaxCompletionTokens = limitOpt
	case domain.OutputLimitParameterMaxTokens:
		req.MaxTokens = limitOpt
	default:
		req.MaxTokens = limitOpt
	}
}

// thinkingEffort maps a domain thinking effort to the OpenRouter ChatRequestEffort.
func thinkingEffort(effort domain.ThinkingEffort) components.ChatRequestEffort {
	switch effort {
	case domain.ThinkingOff:
		return components.ChatRequestEffortNone
	case domain.ThinkingLow:
		return components.ChatRequestEffortMedium
	case domain.ThinkingMedium:
		return components.ChatRequestEffortHigh
	case domain.ThinkingHigh:
		return components.ChatRequestEffortXhigh
	default:
		return components.ChatRequestEffortNone
	}
	// Not managed:
	// components.ChatRequestEffortMax
	// components.ChatRequestEffortXhigh
	// components.ChatRequestEffortMinimal
}
