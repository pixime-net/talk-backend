package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pixime-net/talk/internal/domain"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func TestApplyOutputLimit(t *testing.T) {
	tests := []struct {
		name              string
		parameter         domain.OutputLimitParameter
		providerLimit     int64
		requestLimit      int64
		wantTokens        int64
		wantTokensSet     bool
		wantCompletion    int64
		wantCompletionSet bool
	}{
		{
			name:          "max tokens",
			parameter:     domain.OutputLimitParameterMaxTokens,
			requestLimit:  100,
			wantTokens:    100,
			wantTokensSet: true,
		},
		{
			name:              "max completion tokens",
			parameter:         domain.OutputLimitParameterMaxCompletionTokens,
			requestLimit:      200,
			wantCompletion:    200,
			wantCompletionSet: true,
		},
		{
			name:          "provider limit wins",
			parameter:     domain.OutputLimitParameterMaxTokens,
			providerLimit: 100,
			requestLimit:  200,
			wantTokens:    100,
			wantTokensSet: true,
		},
		{
			name:      "no effective limit",
			parameter: domain.OutputLimitParameterMaxTokens,
		},
		{
			name:         "configured limit without parameter",
			requestLimit: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := openai.ChatCompletionNewParams{}
			applyOutputLimit(&params, domain.Model{
				ProviderMaxOutputTokens: tt.providerLimit,
				RequestMaxOutputTokens:  tt.requestLimit,
				OutputLimitParameter:    tt.parameter,
			})

			if got, want := params.MaxTokens.Valid(), tt.wantTokensSet; got != want {
				t.Errorf("MaxTokens.Valid() = %v, want %v", got, want)
			}
			if got := params.MaxTokens.Value; got != tt.wantTokens {
				t.Errorf("MaxTokens.Value = %d, want %d", got, tt.wantTokens)
			}
			if got, want := params.MaxCompletionTokens.Valid(), tt.wantCompletionSet; got != want {
				t.Errorf("MaxCompletionTokens.Valid() = %v, want %v", got, want)
			}
			if got := params.MaxCompletionTokens.Value; got != tt.wantCompletion {
				t.Errorf("MaxCompletionTokens.Value = %d, want %d", got, tt.wantCompletion)
			}
		})
	}
}

// roundTripFunc adapts a function into an http.RoundTripper for request capture.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestComplete_MaxCompletionTokensWithReasoningEffort(t *testing.T) {
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
				"id": "chatcmpl-1",
				"object": "chat.completion",
				"created": 0,
				"model": "o4-mini",
				"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
				"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2,
					"prompt_tokens_details": {"cached_tokens": 0},
					"completion_tokens_details": {"reasoning_tokens": 0}}
			}`)),
		}, nil
	})

	sdk := openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithHTTPClient(&http.Client{Transport: transport}),
	)
	client := &OpenAIClient{sdk: &sdk, model: domain.Model{
		APIModelID:             "o4-mini",
		ThinkingStyle:          domain.ThinkingStyleEffort,
		RequestMaxOutputTokens: 16384,
		OutputLimitParameter:   domain.OutputLimitParameterMaxCompletionTokens,
	}}

	_, _, err := client.Complete(context.Background(), "", []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
	}, nil, domain.CompletionOptions{ThinkingEffort: domain.ThinkingMedium})
	if err != nil {
		t.Fatalf("Complete returned unexpected error: %v", err)
	}

	if len(captured) == 0 {
		t.Fatal("no request body captured")
	}
	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("invalid request JSON: %v", err)
	}

	if got, ok := body["max_completion_tokens"].(float64); !ok || got != 16384 {
		t.Errorf("max_completion_tokens = %v, want 16384", body["max_completion_tokens"])
	}
	if got, ok := body["reasoning_effort"].(string); !ok || got != string(domain.ThinkingMedium) {
		t.Errorf("reasoning_effort = %v, want %q", body["reasoning_effort"], domain.ThinkingMedium)
	}
}
