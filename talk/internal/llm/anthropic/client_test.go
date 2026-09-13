package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pixime-net/talk/internal/domain"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// roundTripFunc adapts a function into an http.RoundTripper for request capture.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func captureComplete(t *testing.T, model domain.Model) []byte {
	t.Helper()
	return captureCompleteWithOpts(t, model, domain.CompletionOptions{})
}

func captureCompleteWithOpts(t *testing.T, model domain.Model, opts domain.CompletionOptions) []byte {
	t.Helper()

	var captured []byte
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		captured = b
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id": "msg_1",
				"type": "message",
				"role": "assistant",
				"model": "claude-sonnet-4-5",
				"content": [{"type": "text", "text": "ok"}],
				"stop_reason": "end_turn",
				"usage": {"input_tokens": 1, "output_tokens": 1,
					"cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
			}`)),
		}, nil
	})

	sdk := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithHTTPClient(&http.Client{Transport: transport}),
	)
	client := &AnthropicClient{sdk: &sdk, model: model}

	_, _, err := client.Complete(context.Background(), "", []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
	}, nil, opts)
	if err != nil {
		t.Fatalf("Complete returned unexpected error: %v", err)
	}

	if len(captured) == 0 {
		t.Fatal("no request body captured")
	}
	return captured
}

func bodyMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("invalid request JSON: %v", err)
	}
	return body
}

func TestComplete_SendsEffectiveLimitAsMaxTokens(t *testing.T) {
	body := bodyMap(t, captureComplete(t, domain.Model{
		APIModelID:             "claude-sonnet-4-5",
		RequestMaxOutputTokens: 16384,
	}))

	if got, ok := body["max_tokens"].(float64); !ok || got != 16384 {
		t.Errorf("max_tokens = %v, want 16384", body["max_tokens"])
	}
}

func TestComplete_FallsBackTo4096WhenNoLimitConfigured(t *testing.T) {
	body := bodyMap(t, captureComplete(t, domain.Model{APIModelID: "claude-sonnet-4-5"}))

	if got, ok := body["max_tokens"].(float64); !ok || got != 4096 {
		t.Errorf("max_tokens = %v, want 4096", body["max_tokens"])
	}
}

func TestComplete_AdaptiveModel_SendsDisabledWhenThinkingOff(t *testing.T) {
	body := bodyMap(t, captureCompleteWithOpts(t, domain.Model{
		APIModelID:    "claude-sonnet-5",
		ThinkingStyle: domain.ThinkingStyleAdaptive,
	}, domain.CompletionOptions{ThinkingEffort: domain.ThinkingOff}))

	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking field missing from request body, got: %v", body["thinking"])
	}
	if got := thinking["type"]; got != "disabled" {
		t.Errorf("thinking.type = %v, want \"disabled\"", got)
	}
}

func TestComplete_AdaptiveModel_SendsDisabledWhenEffortEmpty(t *testing.T) {
	body := bodyMap(t, captureCompleteWithOpts(t, domain.Model{
		APIModelID:    "claude-sonnet-5",
		ThinkingStyle: domain.ThinkingStyleAdaptive,
	}, domain.CompletionOptions{}))

	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking field missing from request body, got: %v", body["thinking"])
	}
	if got := thinking["type"]; got != "disabled" {
		t.Errorf("thinking.type = %v, want \"disabled\"", got)
	}
}

func TestComplete_AdaptiveModel_SendsAdaptiveAndEffortWhenThinkingEnabled(t *testing.T) {
	tests := []struct {
		name       string
		effort     domain.ThinkingEffort
		wantEffort string
	}{
		{"low", domain.ThinkingLow, "low"},
		{"medium", domain.ThinkingMedium, "medium"},
		{"high", domain.ThinkingHigh, "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bodyMap(t, captureCompleteWithOpts(t, domain.Model{
				APIModelID:    "claude-sonnet-5",
				ThinkingStyle: domain.ThinkingStyleAdaptive,
			}, domain.CompletionOptions{ThinkingEffort: tt.effort}))

			thinking, ok := body["thinking"].(map[string]any)
			if !ok {
				t.Fatalf("thinking field missing from request body, got: %v", body["thinking"])
			}
			if got := thinking["type"]; got != "adaptive" {
				t.Errorf("thinking.type = %v, want \"adaptive\"", got)
			}

			outputConfig, ok := body["output_config"].(map[string]any)
			if !ok {
				t.Fatalf("output_config field missing from request body, got: %v", body["output_config"])
			}
			if got := outputConfig["effort"]; got != tt.wantEffort {
				t.Errorf("output_config.effort = %v, want %q", got, tt.wantEffort)
			}
		})
	}
}

func TestComplete_BudgetModel_NoThinkingFieldWhenEffortEmpty(t *testing.T) {
	body := bodyMap(t, captureCompleteWithOpts(t, domain.Model{
		APIModelID:             "claude-sonnet-4-5",
		ThinkingStyle:          domain.ThinkingStyleBudget,
		RequestMaxOutputTokens: 16384,
	}, domain.CompletionOptions{}))

	if _, exists := body["thinking"]; exists {
		t.Errorf("thinking field should not be present when effort is empty, got: %v", body["thinking"])
	}
}

func TestComplete_BudgetModel_SendsEnabledWithBudgetTokens(t *testing.T) {
	body := bodyMap(t, captureCompleteWithOpts(t, domain.Model{
		APIModelID:             "claude-sonnet-4-5",
		ThinkingStyle:          domain.ThinkingStyleBudget,
		RequestMaxOutputTokens: 16384,
	}, domain.CompletionOptions{ThinkingEffort: domain.ThinkingHigh}))

	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking field missing from request body, got: %v", body["thinking"])
	}
	if got := thinking["type"]; got != "enabled" {
		t.Errorf("thinking.type = %v, want \"enabled\"", got)
	}
	budget := int64(thinking["budget_tokens"].(float64))
	if budget < 1024 {
		t.Errorf("thinking.budget_tokens = %d, want >= 1024", budget)
	}
}
