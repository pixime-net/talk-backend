package agui

import "github.com/pixime-net/talk/internal/domain"

// TokenUsagePayload is the confirmed token usage for one completed LLM call.
type TokenUsagePayload struct {
	Model                   string   `json:"model"`
	InputTokens             int64    `json:"input_tokens,omitempty"`
	OutputTokens            int64    `json:"output_tokens,omitempty"`
	CacheReadTokens         int64    `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens        int64    `json:"cache_write_tokens,omitempty"`
	ReasoningTokens         int64    `json:"reasoning_tokens,omitempty"`
	ContextWindowTokens     int64    `json:"context_window_tokens,omitempty"`
	ProviderMaxOutputTokens int64    `json:"provider_max_output_tokens,omitempty"`
	ContextRatio            *float64 `json:"context_ratio,omitempty"`
	OutputRatio             *float64 `json:"output_ratio,omitempty"`
}

// TurnUsagePayload is the authoritative token total for one completed turn.
type TurnUsagePayload struct {
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens,omitempty"`
	OutputTokens     int64  `json:"output_tokens,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int64  `json:"reasoning_tokens,omitempty"`
}

// NewTokenUsagePayload builds the AG-UI payload for one LLM call.
func NewTokenUsagePayload(model domain.Model, usage domain.Usage) TokenUsagePayload {
	return TokenUsagePayload{
		Model:                   model.Name,
		InputTokens:             usage.InputTokens,
		OutputTokens:            usage.OutputTokens,
		CacheReadTokens:         usage.CacheReadTokens,
		CacheWriteTokens:        usage.CacheWriteTokens,
		ReasoningTokens:         usage.ReasoningTokens,
		ContextWindowTokens:     positiveValue(model.ContextWindowTokens),
		ProviderMaxOutputTokens: positiveValue(model.ProviderMaxOutputTokens),
		ContextRatio:            tokenRatio(usage.InputTokens, model.ContextWindowTokens),
		OutputRatio:             tokenRatio(usage.OutputTokens, model.ProviderMaxOutputTokens),
	}
}

// NewTurnUsagePayload builds the AG-UI payload for one completed turn.
func NewTurnUsagePayload(model domain.Model, usage domain.Usage) TurnUsagePayload {
	return TurnUsagePayload{
		Model:            model.Name,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
		ReasoningTokens:  usage.ReasoningTokens,
	}
}

func tokenRatio(tokens, limit int64) *float64 {
	if tokens <= 0 || limit <= 0 {
		return nil
	}

	ratio := float64(tokens) / float64(limit)
	return &ratio
}

func positiveValue(value int64) int64 {
	if value > 0 {
		return value
	}
	return 0
}
