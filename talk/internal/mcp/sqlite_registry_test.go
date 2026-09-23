package mcp

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Remove(dbPath)
	})
	return db
}

func TestValidateServerName(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"owm", true},
		{"ign-nav", true},
		{"a", true},
		{"a1", true},
		{"", false},
		{"OWM", false},
		{"my_srv", false},
		{"-owm", false},
		{"owm-", false},
		{"météo", false},
		{"has space", false},
		{"aaaaaaaaaaaaaaaaaaaaaaaaa", false}, // 25 characters
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateServerName(tc.name)
			if tc.valid && err != nil {
				t.Errorf("expected %q to be valid, got %v", tc.name, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("expected %q to be rejected", tc.name)
			}
		})
	}
}

func TestSQLiteRegistry_AddAndList(t *testing.T) {
	db := testDB(t)
	reg, err := NewSqliteMCPRegistry(db)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	ctx := context.Background()

	// Initially empty.
	servers, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(servers) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(servers))
	}

	// Add an API key server.
	cfg := ServerConfig{
		ID:       "srv-1",
		Name:     "weather",
		URL:      "https://weather.example.com/mcp",
		AuthType: AuthTypeAPIKey,
		APIKey:   "secret-key-123",
	}
	if err := reg.Add(ctx, cfg); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Add an OAuth server.
	cfg2 := ServerConfig{
		ID:       "srv-2",
		Name:     "navigation",
		URL:      "https://nav.example.com/mcp",
		AuthType: AuthTypeOAuth,
		OAuth: &OAuthConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			TokenURL:     "https://auth.example.com/token",
			Scopes:       []string{"read", "write"},
		},
	}
	if err := reg.Add(ctx, cfg2); err != nil {
		t.Fatalf("add oauth: %v", err)
	}

	// List returns both, ordered by name.
	servers, err = reg.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	// Ordered by name: navigation, weather.
	if servers[0].Name != "navigation" {
		t.Errorf("first server: expected 'navigation', got %q", servers[0].Name)
	}
	if servers[1].Name != "weather" {
		t.Errorf("second server: expected 'weather', got %q", servers[1].Name)
	}
	if servers[1].APIKey != "secret-key-123" {
		t.Errorf("apikey: expected 'secret-key-123', got %q", servers[1].APIKey)
	}
	if servers[0].OAuth == nil {
		t.Fatal("oauth config is nil")
	}
	if servers[0].OAuth.ClientID != "client-id" {
		t.Errorf("oauth client_id: expected 'client-id', got %q", servers[0].OAuth.ClientID)
	}
	if len(servers[0].OAuth.Scopes) != 2 {
		t.Errorf("oauth scopes: expected 2, got %d", len(servers[0].OAuth.Scopes))
	}
}

func TestSQLiteRegistry_Get(t *testing.T) {
	db := testDB(t)
	reg, err := NewSqliteMCPRegistry(db)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	ctx := context.Background()
	cfg := ServerConfig{
		ID:       "srv-1",
		Name:     "weather",
		URL:      "https://weather.example.com/mcp",
		AuthType: AuthTypeAPIKey,
		APIKey:   "key",
	}
	_ = reg.Add(ctx, cfg)

	got, err := reg.Get(ctx, "srv-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "weather" {
		t.Errorf("name: expected 'weather', got %q", got.Name)
	}

	// Not found.
	_, err = reg.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent server")
	}
}

func TestSQLiteRegistry_Remove(t *testing.T) {
	db := testDB(t)
	reg, err := NewSqliteMCPRegistry(db)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	ctx := context.Background()
	cfg := ServerConfig{
		ID:       "srv-1",
		Name:     "weather",
		URL:      "https://weather.example.com/mcp",
		AuthType: AuthTypeAPIKey,
		APIKey:   "key",
	}
	_ = reg.Add(ctx, cfg)

	if err := reg.Remove(ctx, "srv-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	servers, _ := reg.List(ctx)
	if len(servers) != 0 {
		t.Fatalf("expected 0 after remove, got %d", len(servers))
	}

	// Remove nonexistent returns error.
	if err := reg.Remove(ctx, "nonexistent"); err == nil {
		t.Fatal("expected error for removing nonexistent server")
	}
}

func TestSQLiteRegistry_DuplicateName(t *testing.T) {
	db := testDB(t)
	reg, err := NewSqliteMCPRegistry(db)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	ctx := context.Background()
	cfg := ServerConfig{
		ID:       "srv-1",
		Name:     "weather",
		URL:      "https://weather.example.com/mcp",
		AuthType: AuthTypeAPIKey,
	}
	_ = reg.Add(ctx, cfg)

	cfg2 := ServerConfig{
		ID:       "srv-2",
		Name:     "weather", // duplicate name
		URL:      "https://other.example.com/mcp",
		AuthType: AuthTypeAPIKey,
	}
	if err := reg.Add(ctx, cfg2); err == nil {
		t.Fatal("expected error for duplicate name")
	}
}
