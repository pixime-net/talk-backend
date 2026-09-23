package domain

import "context"

// MCPAuthType identifies how an MCP server authenticates requests.
type MCPAuthType string

const (
	MCPAuthTypeNone   MCPAuthType = "none"
	MCPAuthTypeAPIKey MCPAuthType = "apikey"
	MCPAuthTypeOAuth  MCPAuthType = "oauth"
)

// MCPOAuthConfig contains OAuth 2.0 credentials for an MCP server.
type MCPOAuthConfig struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	Scopes       []string
}

// MCPServerConfig represents a registered MCP server.
type MCPServerConfig struct {
	ID       string
	Name     string
	URL      string
	AuthType MCPAuthType
	APIKey   string
	OAuth    *MCPOAuthConfig
}

// MCPRegistry provides CRUD operations for MCP server configurations.
type MCPRegistry interface {
	Add(ctx context.Context, cfg MCPServerConfig) error
	Remove(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (MCPServerConfig, error)
	List(ctx context.Context) ([]MCPServerConfig, error)
}
