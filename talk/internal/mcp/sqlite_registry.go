package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite" // registers the SQLite driver with database/sql

	"github.com/pixime-net/talk/internal/domain"
)

const mcpSchema = `
CREATE TABLE IF NOT EXISTS mcp_servers (
	id        TEXT PRIMARY KEY,
	name      TEXT NOT NULL UNIQUE,
	url       TEXT NOT NULL,
	auth_type TEXT NOT NULL DEFAULT 'apikey',
	api_key   TEXT NOT NULL DEFAULT '',
	oauth     TEXT NOT NULL DEFAULT ''
);
`

// SqliteMCPRegistry is a SQLite-backed implementation of Registry.
type SqliteMCPRegistry struct {
	db *sql.DB
}

var _ domain.MCPRegistry = (*SqliteMCPRegistry)(nil)

// NewSqliteMCPRegistry opens an existing database connection and ensures the mcp_servers table exists.
func NewSqliteMCPRegistry(db *sql.DB) (*SqliteMCPRegistry, error) {
	if _, err := db.Exec(mcpSchema); err != nil {
		return nil, fmt.Errorf("creating mcp_servers table: %w", err)
	}
	return &SqliteMCPRegistry{db: db}, nil
}

// Add inserts a new MCP server configuration.
func (r *SqliteMCPRegistry) Add(_ context.Context, cfg ServerConfig) error {
	oauthJSON := ""
	if cfg.OAuth != nil {
		b, err := json.Marshal(cfg.OAuth)
		if err != nil {
			return fmt.Errorf("marshalling oauth config: %w", err)
		}
		oauthJSON = string(b)
	}
	_, err := r.db.Exec(
		"INSERT INTO mcp_servers (id, name, url, auth_type, api_key, oauth) VALUES (?, ?, ?, ?, ?, ?)",
		cfg.ID, cfg.Name, cfg.URL, string(cfg.AuthType), cfg.APIKey, oauthJSON,
	)
	if err != nil {
		return fmt.Errorf("inserting mcp server: %w", err)
	}
	return nil
}

// Remove deletes an MCP server configuration by ID.
func (r *SqliteMCPRegistry) Remove(_ context.Context, id string) error {
	res, err := r.db.Exec("DELETE FROM mcp_servers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting mcp server: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mcp server %q not found", id)
	}
	return nil
}

// Get retrieves an MCP server configuration by ID.
func (r *SqliteMCPRegistry) Get(_ context.Context, id string) (ServerConfig, error) {
	row := r.db.QueryRow(
		"SELECT id, name, url, auth_type, api_key, oauth FROM mcp_servers WHERE id = ?", id,
	)
	return scanServerConfig(row)
}

// List returns all registered MCP server configurations.
func (r *SqliteMCPRegistry) List(_ context.Context) ([]ServerConfig, error) {
	rows, err := r.db.Query("SELECT id, name, url, auth_type, api_key, oauth FROM mcp_servers ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing mcp servers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var configs []ServerConfig
	for rows.Next() {
		cfg, err := scanServerConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanServerConfig(s scanner) (ServerConfig, error) {
	var cfg ServerConfig
	var authType, oauthJSON string
	if err := s.Scan(&cfg.ID, &cfg.Name, &cfg.URL, &authType, &cfg.APIKey, &oauthJSON); err != nil {
		if err == sql.ErrNoRows {
			return cfg, fmt.Errorf("mcp server not found")
		}
		return cfg, fmt.Errorf("scanning mcp server: %w", err)
	}
	cfg.AuthType = AuthType(authType)
	if oauthJSON != "" {
		var oc OAuthConfig
		if err := json.Unmarshal([]byte(oauthJSON), &oc); err != nil {
			return cfg, fmt.Errorf("unmarshalling oauth config: %w", err)
		}
		cfg.OAuth = &oc
	}
	return cfg, nil
}
