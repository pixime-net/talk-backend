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
	}, nil, domain.CompletionOptions{})
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
