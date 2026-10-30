package main

import (
	"testing"

	"github.com/pixime-net/mcp-searxng/internal/config"
)

func TestBuildApp_Minimal(t *testing.T) {
	env := &config.ServerEnv{
		SearxngURL: "http://example.com:8080",
	}
	app := buildApp(env)
	if app == nil {
		t.Fatal("expected non-nil app")
	}
}

func TestBuildApp_WithAPIKey(t *testing.T) {
	env := &config.ServerEnv{
		SearxngURL: "http://example.com:8080",
	}
	env.APIKey = "mcp-api-key"
	app := buildApp(env)
	if app == nil {
		t.Fatal("expected non-nil app")
	}
}

func TestBuildApp_WithOAuth(t *testing.T) {
	env := &config.ServerEnv{
		SearxngURL: "http://example.com:8080",
	}
	env.OAuthAuthorizationServer = "https://auth.example.com"
	env.OAuthAudience = "my-api"
	env.OAuthScopes = "read,write"
	env.BaseURL = "https://mcp.example.com"
	app := buildApp(env)
	if app == nil {
		t.Fatal("expected non-nil app")
	}
}
