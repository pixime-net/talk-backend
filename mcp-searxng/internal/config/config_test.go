package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadServerEnv_Success(t *testing.T) {
	clearEnv(t)
	t.Setenv("SEARXNG_URL", "http://example.com:8080")

	env, err := LoadServerEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.SearxngURL != "http://example.com:8080" {
		t.Errorf("expected SearxngUrl %q, got %q", "http://example.com:8080", env.SearxngURL)
	}

}

func TestLoadServerEnv_WithEnvFile(t *testing.T) {
	clearEnv(t)

	envFile := writeEnvFile(t, `SEARXNG_URL=http://example.com:8080
BASE_URL=http://localhost:8080
`)

	env, err := LoadServerEnv(envFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.SearxngURL != "http://example.com:8080" {
		t.Errorf("expected SearxngUrl %q, got %q", "http://example.com:8080", env.SearxngURL)
	}
	if env.BaseURL != "http://localhost:8080" {
		t.Errorf("expected BaseURL %q, got %q", "http://localhost:8080", env.BaseURL)
	}
}

func TestLoadServerEnv_EnvVarsNotOverridden(t *testing.T) {
	clearEnv(t)
	t.Setenv("SEARXNG_URL", "pre-existing")

	envFile := writeEnvFile(t, `SEARXNG_URL=from-file
`)

	env, err := LoadServerEnv(envFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.SearxngURL != "pre-existing" {
		t.Errorf("expected SearxngUrl %q, got %q", "pre-existing", env.SearxngURL)
	}
}

func TestLoadServerEnv_MissingFileDoesNotError(t *testing.T) {
	clearEnv(t)
	t.Setenv("SEARXNG_URL", "key")

	env, err := LoadServerEnv("/nonexistent/path/.env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.SearxngURL != "key" {
		t.Errorf("expected SEARXNG_URL %q, got %q", "key", env.SearxngURL)
	}
}

func TestLoadServerEnv_BaseEnvOAuthFields(t *testing.T) {
	clearEnv(t)

	envFile := writeEnvFile(t, `SEARXNG_URL=key
OAUTH_AUTHORIZATION_SERVER=https://auth.example.com
OAUTH_AUDIENCE=my-api
OAUTH_SCOPES=read,write
`)

	env, err := LoadServerEnv(envFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.OAuthAuthorizationServer != "https://auth.example.com" {
		t.Errorf("expected OAuthAuthorizationServer %q, got %q", "https://auth.example.com", env.OAuthAuthorizationServer)
	}
	if env.OAuthAudience != "my-api" {
		t.Errorf("expected OAuthAudience %q, got %q", "my-api", env.OAuthAudience)
	}
	if env.OAuthScopes != "read,write" {
		t.Errorf("expected OAuthScopes %q, got %q", "read,write", env.OAuthScopes)
	}
}

func TestLoadServerEnv_SearchMaxResultsDefault(t *testing.T) {
	clearEnv(t)

	env, err := LoadServerEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.SearchMaxResults != DefaultSearchMaxResults {
		t.Errorf("expected SearchMaxResults %d, got %d", DefaultSearchMaxResults, env.SearchMaxResults)
	}
}

func TestLoadServerEnv_SearchMaxResultsFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("SEARCH_MAX_RESULTS", "5")

	env, err := LoadServerEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.SearchMaxResults != 5 {
		t.Errorf("expected SearchMaxResults 5, got %d", env.SearchMaxResults)
	}
}

func TestLoadServerEnv_SearchMaxResultsInvalid(t *testing.T) {
	for _, v := range []string{"abc", "0", "-3", "2.5"} {
		t.Run(v, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("SEARCH_MAX_RESULTS", v)

			if _, err := LoadServerEnv(); err == nil {
				t.Fatalf("expected error for SEARCH_MAX_RESULTS=%q", v)
			}
		})
	}
}

func TestLoadServerEnv_ScrapeMaxChars(t *testing.T) {
	clearEnv(t)

	env, err := LoadServerEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.ScrapeMaxChars != DefaultScrapeMaxChars {
		t.Errorf("expected default ScrapeMaxChars %d, got %d", DefaultScrapeMaxChars, env.ScrapeMaxChars)
	}

	t.Setenv("SCRAPE_MAX_CHARS", "1200")
	env, err = LoadServerEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.ScrapeMaxChars != 1200 {
		t.Errorf("expected ScrapeMaxChars 1200, got %d", env.ScrapeMaxChars)
	}
}

func TestLoadServerEnv_ScrapeMaxCharsInvalid(t *testing.T) {
	for _, v := range []string{"abc", "0", "-3", "2.5"} {
		t.Run(v, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("SCRAPE_MAX_CHARS", v)

			if _, err := LoadServerEnv(); err == nil {
				t.Fatalf("expected error for SCRAPE_MAX_CHARS=%q", v)
			}
		})
	}
}

func TestLoadServerEnv_ScrapeRenderDefaults(t *testing.T) {
	clearEnv(t)

	env, err := LoadServerEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.ScrapeRenderURL != "" {
		t.Errorf("expected empty ScrapeRenderURL, got %q", env.ScrapeRenderURL)
	}
	if env.ScrapeRenderToken != "" {
		t.Errorf("expected empty ScrapeRenderToken, got %q", env.ScrapeRenderToken)
	}
	if env.ScrapeRenderMode != RenderOff {
		t.Errorf("expected RenderOff, got %q", env.ScrapeRenderMode)
	}
	if env.ScrapeRenderTimeout != DefaultScrapeRenderTimeout {
		t.Errorf("expected timeout %s, got %s", DefaultScrapeRenderTimeout, env.ScrapeRenderTimeout)
	}
	if env.ScrapeRenderConcurrency != DefaultScrapeRenderConcurrency {
		t.Errorf("expected concurrency %d, got %d", DefaultScrapeRenderConcurrency, env.ScrapeRenderConcurrency)
	}
}

func TestLoadServerEnv_ScrapeRenderFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("SCRAPE_RENDER_URL", "ws://browser:3000")
	t.Setenv("BROWSERLESS_TOKEN", " secret ")
	t.Setenv("SCRAPE_RENDER_MODE", " Fallback ")
	t.Setenv("SCRAPE_RENDER_TIMEOUT", "45s")
	t.Setenv("SCRAPE_RENDER_CONCURRENCY", "3")

	env, err := LoadServerEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.ScrapeRenderURL != "ws://browser:3000" {
		t.Errorf("unexpected ScrapeRenderURL %q", env.ScrapeRenderURL)
	}
	if env.ScrapeRenderToken != "secret" {
		t.Errorf("expected ScrapeRenderToken %q, got %q", "secret", env.ScrapeRenderToken)
	}
	if env.ScrapeRenderMode != RenderFallback {
		t.Errorf("expected RenderFallback, got %q", env.ScrapeRenderMode)
	}
	if env.ScrapeRenderTimeout != 45*time.Second {
		t.Errorf("expected 45s, got %s", env.ScrapeRenderTimeout)
	}
	if env.ScrapeRenderConcurrency != 3 {
		t.Errorf("expected concurrency 3, got %d", env.ScrapeRenderConcurrency)
	}
}

func TestLoadServerEnv_ScrapeRenderDefaultsToFallbackWhenURLSet(t *testing.T) {
	clearEnv(t)
	t.Setenv("SCRAPE_RENDER_URL", "ws://browser:3000")

	env, err := LoadServerEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.ScrapeRenderMode != RenderFallback {
		t.Errorf("expected RenderFallback when URL is set, got %q", env.ScrapeRenderMode)
	}
}

func TestLoadServerEnv_ScrapeRenderInvalid(t *testing.T) {
	tests := map[string]string{
		"bad url scheme":   "http://browser:3000",
		"bad url":          "not a url",
		"bad mode":         "sometimes",
		"bad timeout":      "soon",
		"zero timeout":     "0s",
		"bad concurrency":  "0",
		"mode without url": "always",
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			switch name {
			case "bad url scheme", "bad url":
				t.Setenv("SCRAPE_RENDER_URL", value)
			case "bad mode", "mode without url":
				t.Setenv("SCRAPE_RENDER_MODE", value)
			case "bad timeout", "zero timeout":
				t.Setenv("SCRAPE_RENDER_TIMEOUT", value)
			case "bad concurrency":
				t.Setenv("SCRAPE_RENDER_CONCURRENCY", value)
			}

			if _, err := LoadServerEnv(); err == nil {
				t.Fatalf("expected error for %s (%q)", name, value)
			}
		})
	}
}

// clearEnv unsets all environment variables read by LoadServerEnv.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SEARXNG_URL", "SEARCH_MAX_RESULTS", "SCRAPE_MAX_CHARS",
		"SCRAPE_RENDER_URL", "SCRAPE_RENDER_MODE", "SCRAPE_RENDER_TIMEOUT",
		"SCRAPE_RENDER_CONCURRENCY", "BROWSERLESS_TOKEN",
		"X_API_KEY", "BASE_URL",
		"OAUTH_AUTHORIZATION_SERVER", "OAUTH_AUDIENCE",
		"OAUTH_SCOPES", "OAUTH_CLIENT_SECRET",
	} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
}

// writeEnvFile creates a temporary .env file and returns its path.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write env file: %v", err)
	}
	return path
}
