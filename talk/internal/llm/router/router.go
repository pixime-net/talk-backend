package router

import (
	"fmt"

	"github.com/pixime-net/talk/internal/config"
	"github.com/pixime-net/talk/internal/domain"
	"github.com/pixime-net/talk/internal/llm/anthropic"
	"github.com/pixime-net/talk/internal/llm/openai"
	"github.com/pixime-net/talk/internal/llm/openrouter"
)

// Router builds LlmClient instances for model aliases from configuration.
type Router struct {
	cfg *config.Config
}

// NewLLMRouter creates a Router backed by the given configuration.
func NewLLMRouter(cfg *config.Config) *Router {
	return &Router{cfg: cfg}
}

// Get returns an LlmClient for the given model alias, building it from configuration.
func (r *Router) Get(model string) (domain.LlmClient, error) {
	d, err := domain.Lookup(model)
	if err != nil {
		return nil, err
	}

	key, err := config.GetRequiredKeyValue(d.APIKeyName)
	if err != nil {
		return nil, err
	}

	switch d.APIClient {
	case domain.APIClientAnthropic:
		return anthropic.NewAnthropicClient(key, d), nil
	case domain.APIClientOpenAI:
		// Standard OpenAI-compatible provider
		return openai.NewOpenAIClient(key, d), nil
	case domain.APIClientOpenRouter:
		return openrouter.NewClient(key, d), nil
	default:
		return nil, fmt.Errorf("unsupported API client %q", d.APIClient)
	}
}
