package anthropic

import (
	"context"
	"fmt"

	"github.com/pixime-net/talk/internal/domain"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient implements domain.LlmClient using the Anthropic API.
type AnthropicClient struct {
	sdk   *anthropic.Client
	model domain.Model
}

var _ domain.LlmClient = (*AnthropicClient)(nil) // ensure AnthropicClient implements domain.LlmClient

// NewAnthropicClient creates an Anthropic Client.
func NewAnthropicClient(apiKey string, model domain.Model) *AnthropicClient {
	sdk := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicClient{sdk: &sdk, model: model}
}

// Complete sends the conversation to Anthropic and returns the assistant response with token usage.
func (c *AnthropicClient) Complete(ctx context.Context, systemPrompt string, messages []domain.Message, tools []domain.Tool, opts domain.CompletionOptions) (*domain.Message, domain.Usage, error) {
	maxTokens := c.model.EffectiveOutputLimit()
	if maxTokens == 0 {
		// The Messages API requires max_tokens (SDK field is api:"required"), so there
		// is no provider default to preserve when the effective rule yields zero.
		// AC#5's "omit the parameter" path is unachievable here; fall back to a
		// conservative ceiling instead (deviation documented in story 10.1 review).
		maxTokens = 4096
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model.APIModelID),
		MaxTokens: maxTokens,
		Messages:  toSDKMessages(messages),
	}

	switch c.model.ThinkingStyle {
	// Sonnet 5+/Opus 5+
	case domain.ThinkingStyleAdaptive:
		if opts.ThinkingEffort == "" || opts.ThinkingEffort == domain.ThinkingOff {
			params.Thinking = anthropic.ThinkingConfigParamUnion{
				OfDisabled: &anthropic.ThinkingConfigDisabledParam{},
			}
		} else {
			// Sonnet 5+/Opus 5+ default display to "omitted"; force "summarized" so thinking text is returned.
			params.Thinking = anthropic.ThinkingConfigParamUnion{
				OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
					Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized,
				},
			}
			params.OutputConfig.Effort = effortToAnthropicEffort(opts.ThinkingEffort)
		}
	// haiku-4.5/sonnet-4.5
	case domain.ThinkingStyleBudget:
		if opts.ThinkingEffort != "" && opts.ThinkingEffort != domain.ThinkingOff {
			budgetTokens := thinkingEffortToAnthropicBudget(opts.ThinkingEffort, maxTokens)
			params.Thinking = anthropic.ThinkingConfigParamUnion{
				OfEnabled: &anthropic.ThinkingConfigEnabledParam{
					BudgetTokens: budgetTokens,
				},
			}
		}
	}

	if systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{toSystemPrompt(systemPrompt)}
	}

	if len(tools) > 0 {
		var err error
		params.Tools, err = toSDKTools(tools)
		if err != nil {
			return nil, domain.Usage{}, fmt.Errorf("anthropic completion: %w", err)
		}
	}

	resp, err := c.sdk.Messages.New(ctx, params)
	if err != nil {
		return nil, domain.Usage{}, fmt.Errorf("anthropic completion: %w", err)
	}

	msg, usage := fromSDKResponse(resp)
	return msg, usage, nil
}

// effortToAnthropicEffort maps a domain thinking effort to the Anthropic output_config effort level.
func effortToAnthropicEffort(effort domain.ThinkingEffort) anthropic.OutputConfigEffort {
	switch effort {
	case domain.ThinkingLow:
		return anthropic.OutputConfigEffortLow
	case domain.ThinkingMedium:
		return anthropic.OutputConfigEffortMedium
	case domain.ThinkingHigh:
		return anthropic.OutputConfigEffortHigh
	default:
		return anthropic.OutputConfigEffortLow
	}
}

// thinkingEffortToAnthropicBudget computes budget_tokens as a proportion of the model's max output tokens.
func thinkingEffortToAnthropicBudget(effort domain.ThinkingEffort, maxOutputTokens int64) int64 {
	var ratio float64
	switch effort {
	case domain.ThinkingLow:
		ratio = 0.25
	case domain.ThinkingMedium:
		ratio = 0.50
	case domain.ThinkingHigh:
		ratio = 0.75
	default:
		ratio = 0.25
	}
	budget := int64(float64(maxOutputTokens) * ratio)
	if budget < 1024 {
		budget = 1024
	}
	return budget
}
