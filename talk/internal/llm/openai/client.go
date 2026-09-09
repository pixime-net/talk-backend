package openai

import (
	"context"
	"fmt"

	"github.com/pixime-net/talk/internal/domain"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAIClient implements domain.LlmClient using an OpenAI-compatible API.
// It works with OpenAI, Mistral, and any other provider that exposes the
// OpenAI chat completions API.
type OpenAIClient struct {
	sdk   *openai.Client
	model domain.Model
}

// NewOpenAIClient creates an OpenAI-compatible Client.
// The base URL is read from the model descriptor; pass a model with a non-empty
// URL to target Mistral / Devstral / local endpoints.
func NewOpenAIClient(apiKey string, model domain.Model) *OpenAIClient {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if model.URL != "" {
		opts = append(opts, option.WithBaseURL(model.URL))
	}
	sdk := openai.NewClient(opts...)
	return &OpenAIClient{sdk: &sdk, model: model}
}

var _ domain.LlmClient = (*OpenAIClient)(nil) // ensure Client implements domain.LlmClient

// Complete sends the conversation to the OpenAI-compatible API and returns the response with token usage.
func (c *OpenAIClient) Complete(ctx context.Context, systemPrompt string, messages []domain.Message, tools []domain.Tool, opts domain.CompletionOptions) (*domain.Message, domain.Usage, error) {
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(c.model.APIModelID),
		Messages: toSDKMessages(systemPrompt, messages),
	}

	applyOutputLimit(&params, c.model)

	if c.model.ThinkingStyle == domain.ThinkingStyleEffort && opts.ThinkingEffort != "" && opts.ThinkingEffort != domain.ThinkingOff {
		params.ReasoningEffort = openai.ReasoningEffort(opts.ThinkingEffort)
	}

	if len(tools) > 0 {
		var err error
		params.Tools, err = toSDKTools(tools)
		if err != nil {
			return nil, domain.Usage{}, fmt.Errorf("openai completion: %w", err)
		}
	}

	resp, err := c.sdk.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, domain.Usage{}, fmt.Errorf("openai completion: %w", err)
	}

	msg, usage := fromSDKResponse(resp)
	return msg, usage, nil
}

func applyOutputLimit(params *openai.ChatCompletionNewParams, model domain.Model) {
	limit := model.EffectiveOutputLimit()
	if limit <= 0 {
		return
	}

	switch model.OutputLimitParameter {
	case domain.OutputLimitParameterMaxTokens:
		params.MaxTokens = openai.Int(limit)
	case domain.OutputLimitParameterMaxCompletionTokens:
		params.MaxCompletionTokens = openai.Int(limit)
	}
}
