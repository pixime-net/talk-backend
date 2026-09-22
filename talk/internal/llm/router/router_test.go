package router

import (
	"testing"

	"github.com/pixime-net/talk/internal/config"
)

func TestRouter_UnknownModelReturnsError(t *testing.T) {
	r := NewLLMRouter(&config.Config{})
	_, err := r.Get("unknown-model")
	if err == nil {
		t.Error("expected error for unknown model, got nil")
	}
}

func TestRouter_MissingAPIKeyReturnsError(t *testing.T) {
	r := NewLLMRouter(&config.Config{})
	_, err := r.Get("sonnet-4.6")
	if err == nil {
		t.Error("expected error for missing ANTHROPIC_API_KEY, got nil")
	}
}

func TestRouter_ValidConfigReturnsClient(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	r := NewLLMRouter(&config.Config{})
	client, err := r.Get("sonnet-4.6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client")
	}
}

func TestRouter_OpenAIProviderReturnsClient(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	r := NewLLMRouter(&config.Config{})
	client, err := r.Get("gpt-5.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client")
	}
}

func TestRouter_MistralProviderReturnsClient(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "test-key")
	r := NewLLMRouter(&config.Config{})
	client, err := r.Get("mistral-small")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client")
	}
}

func TestRouter_OpenRouterProviderReturnsClient(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	r := NewLLMRouter(&config.Config{})
	client, err := r.Get("deepseek-v3.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client")
	}
}
