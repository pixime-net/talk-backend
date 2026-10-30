package config

import (
	"os"
	"path/filepath"
	"testing"
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

// clearEnv unsets all environment variables read by LoadServerEnv.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SEARXNG_URL", "SEARCH_MAX_RESULTS", "SCRAPE_MAX_CHARS",
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
