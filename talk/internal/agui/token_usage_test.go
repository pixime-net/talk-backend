package agui

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/pixime-net/talk/internal/domain"
)

func TestNewTokenUsagePayload(t *testing.T) {
	model := domain.Model{Name: "sonnet-4.6", ContextWindowTokens: 200_000, ProviderMaxOutputTokens: 64_000}
	usage := domain.Usage{InputTokens: 41_230, OutputTokens: 854, CacheReadTokens: 10_000, CacheWriteTokens: 2_000, ReasoningTokens: 200}

	payload := NewTokenUsagePayload(model, usage)
	if payload.Model != model.Name || payload.InputTokens != usage.InputTokens || payload.OutputTokens != usage.OutputTokens {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.ContextRatio == nil || math.Abs(*payload.ContextRatio-float64(usage.InputTokens)/float64(model.ContextWindowTokens)) > 1e-12 {
		t.Errorf("ContextRatio = %v", payload.ContextRatio)
	}
	if payload.OutputRatio == nil || math.Abs(*payload.OutputRatio-float64(usage.OutputTokens)/float64(model.ProviderMaxOutputTokens)) > 1e-12 {
		t.Errorf("OutputRatio = %v", payload.OutputRatio)
	}
}

func TestNewTokenUsagePayloadOmitsUnavailableValues(t *testing.T) {
	payload := NewTokenUsagePayload(domain.Model{Name: "model"}, domain.Usage{InputTokens: 10})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"context_window_tokens", "provider_max_output_tokens", "context_ratio", "output_ratio"} {
		if _, ok := values[key]; ok {
			t.Errorf("payload unexpectedly contains %q", key)
		}
	}
}

func TestNewTurnUsagePayload(t *testing.T) {
	payload := NewTurnUsagePayload(domain.Model{Name: "mistral-small"}, domain.Usage{InputTokens: 12})
	if payload.Model != "mistral-small" || payload.InputTokens != 12 {
		t.Fatalf("payload = %+v", payload)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"output_tokens", "cache_read_tokens", "cache_write_tokens", "reasoning_tokens"} {
		if _, ok := values[key]; ok {
			t.Errorf("payload unexpectedly contains %q", key)
		}
	}
}
