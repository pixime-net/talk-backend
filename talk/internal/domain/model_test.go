package domain

import (
	"strings"
	"testing"
)

func TestLookup_KnownModel(t *testing.T) {
	d, err := Lookup("sonnet-4.6")
	if err != nil {
		t.Fatalf("Lookup returned unexpected error: %v", err)
	}

	if d.Name != "sonnet-4.6" {
		t.Fatalf("expected ModelID %q, got %q", "sonnet-4.6", d.Name)
	}
	if d.OLTPProvider != OLTPProviderAnthropic {
		t.Fatalf("expected OLTPProvider %q, got %q", OLTPProviderAnthropic, d.OLTPProvider)
	}
	if d.APIClient != APIClientAnthropic {
		t.Fatalf("expected APIClient %q, got %q", APIClientAnthropic, d.APIClient)
	}
	if d.APIModelID != "claude-sonnet-4-5" {
		t.Fatalf("expected APIModelID %q, got %q", "claude-sonnet-4-5", d.APIModelID)
	}
}

func TestLookup_UnknownModel(t *testing.T) {
	_, err := Lookup("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown model, got nil")
	}
	if !strings.Contains(err.Error(), "unknown model") {
		t.Fatalf("expected error to contain %q, got %q", "unknown model", err.Error())
	}
}

func TestSupportedModels(t *testing.T) {
	models := SupportedModels()

	if len(models) != len(registry) {
		t.Fatalf("expected %d models, got %d", len(registry), len(models))
	}

	seen := make(map[string]struct{}, len(models))
	for i, model := range models {
		if model == "" {
			t.Fatalf("model at index %d is empty", i)
		}
		if _, ok := seen[model]; ok {
			t.Fatalf("duplicate model %q in SupportedModels", model)
		}
		seen[model] = struct{}{}
	}

	for _, expected := range registry {
		if _, ok := seen[expected.Name]; !ok {
			t.Fatalf("missing model %q in SupportedModels", expected.Name)
		}
	}
}

func TestModelEffectiveOutputLimit(t *testing.T) {
	tests := []struct {
		name     string
		provider int64
		request  int64
		want     int64
	}{
		{name: "provider only", provider: 100, want: 100},
		{name: "request only", request: 50, want: 50},
		{name: "request below provider", provider: 100, request: 50, want: 50},
		{name: "request equals provider", provider: 100, request: 100, want: 100},
		{name: "request above provider", provider: 100, request: 150, want: 100},
		{name: "zero request", provider: 100, request: 0, want: 100},
		{name: "no limits", want: 0},
		{name: "negative request", request: -1, want: 0},
		{name: "negative provider", provider: -1, request: 100, want: 100},
		{name: "negative provider no request", provider: -1, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := Model{ProviderMaxOutputTokens: tt.provider, RequestMaxOutputTokens: tt.request}
			if got := model.EffectiveOutputLimit(); got != tt.want {
				t.Fatalf("EffectiveOutputLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSupportedModels_DoesNotContainPoolsideAgent(t *testing.T) {
	for _, model := range SupportedModels() {
		if model == "agent" {
			t.Fatal("Poolside agent must not be registered")
		}
	}
}

// Hardcodes the expected alias set so that dropping an existing model (or adding
// an unexpected one) fails loudly, unlike the registry-derived TestSupportedModels.
func TestSupportedModels_PreservesAllAliases(t *testing.T) {
	expected := []string{"haiku-4.5", "sonnet-4.6", "sonnet-5", "opus-4.6", "o4-mini", "gpt-5.4", "mistral-small"}
	got := SupportedModels()

	if len(got) != len(expected) {
		t.Fatalf("SupportedModels() = %v, want exactly %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("SupportedModels()[%d] = %q, want %q (full list %v)", i, got[i], expected[i], expected)
		}
	}
}
