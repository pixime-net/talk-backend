package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/pixime-net/talk/internal/mcp"
)

const cancelled = "Cancelled."

// labelledLine formats a coloured label followed by its detail.
const labelledLine = "%s %s\n"

func (a *App) cmdMCP(ctx context.Context, args string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		a.cmdMCPList()
		return
	}
	switch parts[0] {
	case "list":
		a.cmdMCPList()
	case "add":
		a.cmdMCPAdd(ctx)
	case "remove":
		a.cmdMCPRemove(ctx)
	case "refresh":
		a.cmdMCPRefresh(ctx)
	default:
		a.Printf("Unknown /mcp subcommand %s. Available: %s, %s, %s, %s\n",
			red(parts[0]), yellow("list"), yellow("add"), yellow("remove"), yellow("refresh"))
	}
}

func (a *App) cmdMCPList() {
	statuses := a.MCPManager.Statuses()
	if len(statuses) == 0 {
		a.Println(faint("(no MCP servers registered)"))
		return
	}
	a.Println("\n" + emphasize("MCP Servers:"))
	for _, st := range statuses {
		stateLabel := green("connected")
		if !st.Connected {
			stateLabel = red("disconnected")
		}
		serverInfo := ""
		if st.ServerName != "" {
			serverInfo = faint(" (" + st.ServerName)
			if st.ServerVersion != "" {
				serverInfo += " " + st.ServerVersion
			}
			serverInfo += ")"
		}
		a.Printf("  %s  %s  %s%s\n", cyan(st.Config.Name), faint(st.Config.URL), stateLabel, serverInfo)
		if st.Error != "" {
			a.Printf("    %s\n", red(st.Error))
		}
		for _, toolName := range st.Tools {
			a.Printf("    • %s\n", toolName)
		}
	}
}

func (a *App) cmdMCPAdd(ctx context.Context) {
	name, err := a.Reader.ReadLine("Server name (lowercase letters, digits, hyphens; 24 max): ")
	if err != nil || strings.TrimSpace(name) == "" {
		a.Println(yellow(cancelled))
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))

	if err := mcp.ValidateServerName(name); err != nil {
		a.Printf(labelledLine, red("Invalid server name:"), err.Error())
		return
	}

	taken, err := a.serverNameTaken(ctx, name)
	if err != nil {
		a.Printf(labelledLine, red("Failed to check existing servers:"), err.Error())
		return
	}
	if taken {
		a.Printf(labelledLine, red("A server with this name already exists:"), cyan(name))
		return
	}

	url, err := a.Reader.ReadLine("Server URL: ")
	if err != nil || strings.TrimSpace(url) == "" {
		a.Println(yellow(cancelled))
		return
	}
	url = strings.TrimSpace(url)

	authChoice, err := a.Reader.ReadLine("Auth type [none/apikey/oauth] (default: apikey): ")
	if err != nil {
		a.Println(yellow(cancelled))
		return
	}
	authChoice = strings.TrimSpace(strings.ToLower(authChoice))
	if authChoice == "" {
		authChoice = "apikey"
	}

	cfg := mcp.ServerConfig{
		ID:       uuid.NewString(),
		Name:     name,
		URL:      url,
		AuthType: mcp.AuthType(authChoice),
	}

	if !a.buildMCPAuthConfig(&cfg) {
		return
	}

	// Test connection before persisting.
	a.Printf("Testing connection to %s...\n", faint(cfg.URL))
	status, err := a.MCPManager.Connect(ctx, cfg)
	if err != nil {
		a.Printf(labelledLine, red("Connection failed:"), err.Error())
		return
	}

	// Persist.
	if err := a.MCPRegistry.Add(ctx, cfg); err != nil {
		a.Printf(labelledLine, red("Failed to save:"), err.Error())
		return
	}

	a.Printf("%s %s", green("✓ Server added:"), cyan(name))
	if status.ServerName != "" {
		a.Printf(" %s", faint("("+status.ServerName+" "+status.ServerVersion+")"))
	}
	a.Printf(" — %d tools\n", len(status.Tools))
}

// serverNameTaken reports whether a server is already registered under name.
// The check is case-insensitive to also cover rows predating name validation.
func (a *App) serverNameTaken(ctx context.Context, name string) (bool, error) {
	configs, err := a.MCPRegistry.List(ctx)
	if err != nil {
		return false, err
	}
	for _, cfg := range configs {
		if strings.EqualFold(cfg.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

// buildMCPAuthConfig prompts for credentials and fills cfg accordingly.
// Returns false if the user cancels or selects an invalid auth type.
func (a *App) buildMCPAuthConfig(cfg *mcp.ServerConfig) bool {
	switch cfg.AuthType {
	case mcp.AuthTypeNone:
		// No credentials needed.
	case mcp.AuthTypeAPIKey:
		key, err := a.Reader.ReadLineRaw("API Key: ")
		if err != nil {
			a.Println(yellow(cancelled))
			return false
		}
		cfg.APIKey = strings.TrimSpace(key)
	case mcp.AuthTypeOAuth:
		clientID, _ := a.Reader.ReadLineRaw("Client ID: ")
		clientSecret, _ := a.Reader.ReadLineRaw("Client Secret: ")
		tokenURL, _ := a.Reader.ReadLine("Token URL: ")
		scopes, _ := a.Reader.ReadLine("Scopes (comma-separated): ")
		cfg.OAuth = &mcp.OAuthConfig{
			ClientID:     strings.TrimSpace(clientID),
			ClientSecret: strings.TrimSpace(clientSecret),
			TokenURL:     strings.TrimSpace(tokenURL),
		}
		if s := strings.TrimSpace(scopes); s != "" {
			for _, sc := range strings.Split(s, ",") {
				cfg.OAuth.Scopes = append(cfg.OAuth.Scopes, strings.TrimSpace(sc))
			}
		}
	default:
		a.Printf("%s\n", yellow("Invalid auth type. Use 'none', 'apikey', or 'oauth'."))
		return false
	}
	return true
}

func (a *App) cmdMCPRemove(ctx context.Context) {
	servers, err := a.MCPRegistry.List(ctx)
	if err != nil {
		a.Errorf("%s%s\n", red("Error: "), err.Error())
		return
	}
	if len(servers) == 0 {
		a.Println(faint("(no MCP servers registered)"))
		return
	}

	a.Println("\n" + emphasize("Registered MCP servers:"))
	for i, s := range servers {
		a.Printf("  [%d] %s  %s\n", i+1, cyan(s.Name), faint(s.URL))
	}

	choice, err := a.Reader.ReadLine(fmt.Sprintf("Remove [1-%d] (Enter to cancel): ", len(servers)))
	if err != nil || strings.TrimSpace(choice) == "" {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || n < 1 || n > len(servers) {
		a.Println(yellow("Invalid choice."))
		return
	}

	selected := servers[n-1]
	if err := a.MCPRegistry.Remove(ctx, selected.ID); err != nil {
		a.Errorf("%s%s\n", red("Error: "), err.Error())
		return
	}
	a.MCPManager.Disconnect(selected.ID)
	a.Printf("Removed %s.\n", green(selected.Name))
}

func (a *App) cmdMCPRefresh(ctx context.Context) {
	count := a.MCPManager.Refresh(ctx)
	a.Printf("%s %d tools available.\n", green("✓ Tools refreshed:"), count)
}
