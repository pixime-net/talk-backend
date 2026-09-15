package domain

import (
	"encoding/json"
	"math"
	"testing"
)

func TestNewTokenUsagePayload_FullPayload(t *testing.T) {
	model := Model{
		Name:                    "sonnet-4.6",
		ContextWindowTokens:     200_000,
		ProviderMaxOutputTokens: 64_000,
	}
	usage := Usage{
		InputTokens:      41_230,
		OutputTokens:     854,
		CacheReadTokens:  10_000,
		CacheWriteTokens: 2_000,
		ReasoningTokens:  200,
	}

	payload := NewTokenUsagePayload(model, usage)

	if payload.Model != model.Name {
		t.Errorf("Model = %q, want %q", payload.Model, model.Name)
	}
	if payload.InputTokens != usage.InputTokens || payload.OutputTokens != usage.OutputTokens {
		t.Errorf("token counts = (%d, %d), want (%d, %d)", payload.InputTokens, payload.OutputTokens, usage.InputTokens, usage.OutputTokens)
	}
	if payload.CacheReadTokens != usage.CacheReadTokens || payload.CacheWriteTokens != usage.CacheWriteTokens {
		t.Errorf("cache tokens = (%d, %d), want (%d, %d)", payload.CacheReadTokens, payload.CacheWriteTokens, usage.CacheReadTokens, usage.CacheWriteTokens)
	}
	if payload.ReasoningTokens != usage.ReasoningTokens {
		t.Errorf("ReasoningTokens = %d, want %d", payload.ReasoningTokens, usage.ReasoningTokens)
	}
	if payload.ContextWindowTokens != model.ContextWindowTokens {
		t.Errorf("ContextWindowTokens = %d, want %d", payload.ContextWindowTokens, model.ContextWindowTokens)
	}
	if payload.ProviderMaxOutputTokens != model.ProviderMaxOutputTokens {
		t.Errorf("ProviderMaxOutputTokens = %d, want %d", payload.ProviderMaxOutputTokens, model.ProviderMaxOutputTokens)
	}
	assertRatio(t, payload.ContextRatio, float64(usage.InputTokens)/float64(model.ContextWindowTokens))
	assertRatio(t, payload.OutputRatio, float64(usage.OutputTokens)/float64(model.ProviderMaxOutputTokens))
}

func TestNewTokenUsagePayload_OmitsUnavailableValues(t *testing.T) {
	tests := []struct {
		name             string
		model            Model
		usage            Usage
		wantContextRatio bool
		wantOutputRatio  bool
	}{
		{
			name:            "missing input count",
			model:           Model{Name: "model", ContextWindowTokens: 100, ProviderMaxOutputTokens: 50},
			usage:           Usage{OutputTokens: 5},
			wantOutputRatio: true,
		},
		{
			name:             "missing output count",
			model:            Model{Name: "model", ContextWindowTokens: 100, ProviderMaxOutputTokens: 50},
			usage:            Usage{InputTokens: 10},
			wantContextRatio: true,
		},
		{
			name:            "zero context limit",
			model:           Model{Name: "model", ProviderMaxOutputTokens: 50},
			usage:           Usage{InputTokens: 10, OutputTokens: 5},
			wantOutputRatio: true,
		},
		{
			name:             "zero output limit",
			model:            Model{Name: "model", ContextWindowTokens: 100},
			usage:            Usage{InputTokens: 10, OutputTokens: 5},
			wantContextRatio: true,
		},
		{
			name:  "negative limits",
			model: Model{Name: "model", ContextWindowTokens: -1, ProviderMaxOutputTokens: -1},
			usage: Usage{InputTokens: 10, OutputTokens: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := NewTokenUsagePayload(tt.model, tt.usage)
			if got := payload.ContextRatio != nil; got != tt.wantContextRatio {
				t.Errorf("ContextRatio present = %v, want %v", got, tt.wantContextRatio)
			}
			if got := payload.OutputRatio != nil; got != tt.wantOutputRatio {
				t.Errorf("OutputRatio present = %v, want %v", got, tt.wantOutputRatio)
			}

			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshaling payload: %v", err)
			}
			var values map[string]any
			if err := json.Unmarshal(data, &values); err != nil {
				t.Fatalf("unmarshaling payload: %v", err)
			}
			if _, ok := values["context_ratio"]; ok != tt.wantContextRatio {
				t.Errorf("context_ratio present = %v, want %v", ok, tt.wantContextRatio)
			}
			if _, ok := values["output_ratio"]; ok != tt.wantOutputRatio {
				t.Errorf("output_ratio present = %v, want %v", ok, tt.wantOutputRatio)
			}
			if tt.model.ContextWindowTokens <= 0 {
				if _, ok := values["context_window_tokens"]; ok {
					t.Error("payload unexpectedly contains context_window_tokens")
				}
			}
			if tt.model.ProviderMaxOutputTokens <= 0 {
				if _, ok := values["provider_max_output_tokens"]; ok {
					t.Error("payload unexpectedly contains provider_max_output_tokens")
				}
			}
		})
	}
}

func TestNewTurnUsagePayload_FullPayload(t *testing.T) {
	usage := Usage{
		InputTokens:      30,
		OutputTokens:     13,
		CacheReadTokens:  2,
		CacheWriteTokens: 3,
		ReasoningTokens:  5,
	}

	payload := NewTurnUsagePayload(Model{Name: "sonnet-4.6"}, usage)

	if payload.Model != "sonnet-4.6" {
		t.Errorf("Model = %q, want sonnet-4.6", payload.Model)
	}
	if payload.InputTokens != usage.InputTokens || payload.OutputTokens != usage.OutputTokens {
		t.Errorf("token counts = (%d, %d), want (%d, %d)", payload.InputTokens, payload.OutputTokens, usage.InputTokens, usage.OutputTokens)
	}
	if payload.CacheReadTokens != usage.CacheReadTokens || payload.CacheWriteTokens != usage.CacheWriteTokens {
		t.Errorf("cache tokens = (%d, %d), want (%d, %d)", payload.CacheReadTokens, payload.CacheWriteTokens, usage.CacheReadTokens, usage.CacheWriteTokens)
	}
	if payload.ReasoningTokens != usage.ReasoningTokens {
		t.Errorf("ReasoningTokens = %d, want %d", payload.ReasoningTokens, usage.ReasoningTokens)
	}
}

func TestNewTurnUsagePayload_OmitsZeroCounts(t *testing.T) {
	payload := NewTurnUsagePayload(Model{Name: "mistral-small"}, Usage{InputTokens: 12})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}

	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatalf("unmarshaling payload: %v", err)
	}
	if values["model"] != "mistral-small" || values["input_tokens"] != float64(12) {
		t.Errorf("payload = %v, want model and input_tokens", values)
	}
	for _, key := range []string{"output_tokens", "cache_read_tokens", "cache_write_tokens", "reasoning_tokens"} {
		if _, ok := values[key]; ok {
			t.Errorf("payload unexpectedly contains %q", key)
		}
	}
}

func assertRatio(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatal("ratio is nil")
	}
	if math.Abs(*got-want) > 1e-12 {
		t.Errorf("ratio = %v, want %v", *got, want)
	}
}
