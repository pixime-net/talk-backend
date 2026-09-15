package main

import (
	"context"
	"strings"
	"testing"

	"github.com/pixime-net/talk/internal/mcp"
)

func TestCmdMCPList_Empty(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg

	app.cmdMCPList()

	out := p.Output()
	if !strings.Contains(out, "no MCP servers registered") {
		t.Errorf("expected 'no MCP servers registered', got: %s", out)
	}
}

func TestCmdMCPList_WithServers(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{
		servers: []mcp.ServerConfig{
			{ID: "1", Name: "weather", URL: "http://localhost:8080"},
		},
	}
	mgr := mcp.NewManager(reg)
	mgr.ConnectAll(context.Background()) // will fail to connect but registers status
	app.MCPManager = mgr
	app.MCPRegistry = reg

	app.cmdMCPList()

	out := p.Output()
	if !strings.Contains(out, "weather") {
		t.Errorf("expected server name 'weather' in output, got: %s", out)
	}
	if !strings.Contains(out, "MCP Servers") {
		t.Errorf("expected 'MCP Servers' header, got: %s", out)
	}
}

func TestCmdMCPRefresh(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg

	app.cmdMCPRefresh(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Tools refreshed") {
		t.Errorf("expected 'Tools refreshed' message, got: %s", out)
	}
}

func TestCmdMCP_UnknownSubcommand(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg

	app.cmdMCP(context.Background(), "badcmd")

	out := p.Output()
	if !strings.Contains(out, "Unknown /mcp subcommand") {
		t.Errorf("expected unknown subcommand message, got: %s", out)
	}
	if !strings.Contains(out, "badcmd") {
		t.Errorf("expected 'badcmd' in output, got: %s", out)
	}
}

func TestCmdMCP_DefaultToList(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg

	app.cmdMCP(context.Background(), "")

	out := p.Output()
	if !strings.Contains(out, "no MCP servers registered") {
		t.Errorf("expected list output on empty args, got: %s", out)
	}
}

func TestCmdMCPRemove_EmptyList(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg

	app.cmdMCPRemove(context.Background())

	out := p.Output()
	if !strings.Contains(out, "no MCP servers registered") {
		t.Errorf("expected 'no MCP servers registered', got: %s", out)
	}
}

func TestCmdMCPRemove_WithChoice(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{
		servers: []mcp.ServerConfig{
			{ID: "srv-1", Name: "test-srv", URL: "http://localhost:9090"},
		},
	}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	app.Reader = newScriptReader("1")

	app.cmdMCPRemove(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Removed") {
		t.Errorf("expected 'Removed' confirmation, got: %s", out)
	}
	if len(reg.servers) != 0 {
		t.Errorf("expected server to be removed, got %d servers", len(reg.servers))
	}
}

func TestCmdMCPRemove_InvalidChoice(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{
		servers: []mcp.ServerConfig{
			{ID: "srv-1", Name: "test-srv", URL: "http://localhost:9090"},
		},
	}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	app.Reader = newScriptReader("99")

	app.cmdMCPRemove(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Invalid choice") {
		t.Errorf("expected 'Invalid choice', got: %s", out)
	}
}

func TestCmdMCPRemove_Cancel(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{
		servers: []mcp.ServerConfig{
			{ID: "srv-1", Name: "test-srv", URL: "http://localhost:9090"},
		},
	}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	app.Reader = newScriptReader("") // empty → cancel

	app.cmdMCPRemove(context.Background())

	if len(reg.servers) != 1 {
		t.Error("expected server to NOT be removed on cancel")
	}
}

func TestCmdMCPAdd_CancelledAtName(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	app.Reader = newScriptReader("") // empty name → cancel

	app.cmdMCPAdd(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Cancelled") {
		t.Errorf("expected 'Cancelled', got: %s", out)
	}
}

func TestCmdMCPAdd_CancelledAtURL(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	app.Reader = newScriptReader("myserver", "") // name ok, empty URL → cancel

	app.cmdMCPAdd(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Cancelled") {
		t.Errorf("expected 'Cancelled', got: %s", out)
	}
}

func TestCmdMCPAdd_InvalidAuthType(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	app.Reader = newScriptReader("myserver", "http://localhost", "badauth")

	app.cmdMCPAdd(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Invalid auth type") {
		t.Errorf("expected 'Invalid auth type', got: %s", out)
	}
}

func TestCmdMCPAdd_NoneAuth_ConnectionFails(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	// name, url, auth=none → will try to connect and fail
	app.Reader = newScriptReader("myserver", "http://127.0.0.1:1", "none")

	app.cmdMCPAdd(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Testing connection") {
		t.Errorf("expected 'Testing connection' message, got: %s", out)
	}
	if !strings.Contains(out, "Connection failed") {
		t.Errorf("expected 'Connection failed' message, got: %s", out)
	}
}

func TestCmdMCPAdd_APIKeyAuth_ConnectionFails(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	// name, url, auth=apikey, key → connect fails
	app.Reader = newScriptReader("myserver", "http://127.0.0.1:1", "apikey", "secret123")

	app.cmdMCPAdd(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Connection failed") {
		t.Errorf("expected 'Connection failed', got: %s", out)
	}
}

func TestCmdMCPAdd_APIKeyAuth_CancelledAtKey(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	// name, url, auth=apikey → then scriptReader exhausted → error on ReadLine
	app.Reader = newScriptReader("myserver", "http://localhost", "apikey")

	app.cmdMCPAdd(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Cancelled") {
		t.Errorf("expected 'Cancelled', got: %s", out)
	}
}

func TestCmdMCPAdd_OAuthAuth_ConnectionFails(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	// name, url, auth=oauth, clientID, secret, tokenURL, scopes
	app.Reader = newScriptReader("myserver", "http://127.0.0.1:1", "oauth", "cid", "csecret", "http://token", "read,write")

	app.cmdMCPAdd(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Connection failed") {
		t.Errorf("expected 'Connection failed', got: %s", out)
	}
}

func TestCmdMCPAdd_DefaultAuth(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg
	// name, url, auth="" (empty → defaults to apikey), key → connect fails
	app.Reader = newScriptReader("myserver", "http://127.0.0.1:1", "", "mykey")

	app.cmdMCPAdd(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Connection failed") {
		t.Errorf("expected 'Connection failed', got: %s", out)
	}
}
