package main

import (
	"context"
	"strings"
	"testing"

	"github.com/pixime-net/talk/internal/mcp"
)

func TestHandleSlashCommand_Help(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.handleSlashCommand(context.Background(), "/help")

	out := p.Output()
	if !strings.Contains(out, "/help") {
		t.Error("expected /help to appear in help output")
	}
	if !strings.Contains(out, "/model") {
		t.Error("expected /model to appear in help output")
	}
	if !strings.Contains(out, "/q") {
		t.Error("expected /q to appear in help output")
	}
}

func TestHandleSlashCommand_Unknown(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.handleSlashCommand(context.Background(), "/foo")

	out := p.Output()
	if !strings.Contains(out, "Unknown command") {
		t.Errorf("expected unknown command message, got: %s", out)
	}
	if !strings.Contains(out, "/foo") {
		t.Errorf("expected /foo in output, got: %s", out)
	}
}

func TestHandleSlashCommand_Model(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.Reader = newScriptReader("999") // invalid → "Invalid choice" path

	app.handleSlashCommand(context.Background(), "/model")

	out := p.Output()
	if !strings.Contains(out, "Available models") {
		t.Error("expected model listing output")
	}
}

func TestHandleSlashCommand_Memory(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.handleSlashCommand(context.Background(), "/memory")

	out := p.Output()
	// With empty session browser, no history exists
	if !strings.Contains(out, "no history for current session") {
		t.Errorf("expected fallback message, got: %s", out)
	}
}

func TestHandleSlashCommand_SessionUnknownSubcommand(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.handleSlashCommand(context.Background(), "/sessions")

	out := p.Output()
	if !strings.Contains(out, "Unknown command") {
		t.Errorf("expected unknown command message, got: %s", out)
	}
}

func TestHandleSlashCommand_Session(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.handleSlashCommand(context.Background(), "/session new")

	out := p.Output()
	if !strings.Contains(out, "New session created") {
		t.Errorf("expected new session message, got: %s", out)
	}
}

func TestHandleSlashCommand_Prompt(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.handleSlashCommand(context.Background(), "/prompt")

	out := p.Output()
	if !strings.Contains(out, "You are a helpful assistant.") {
		t.Errorf("expected prompt text, got: %s", out)
	}
}

func TestHandleSlashCommand_MCP(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	reg := &fakeRegistry{}
	mgr := mcp.NewManager(reg)
	app.MCPManager = mgr
	app.MCPRegistry = reg

	app.handleSlashCommand(context.Background(), "/mcp list")

	out := p.Output()
	if !strings.Contains(out, "no MCP servers registered") {
		t.Errorf("expected empty list message, got: %s", out)
	}
}

func TestCmdPrompt_ShowsPromptText(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.PromptProvider = &stubPromptProvider{text: "Be concise."}

	app.cmdPrompt(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Be concise.") {
		t.Errorf("expected prompt text in output, got: %s", out)
	}
	if !strings.Contains(out, "system prompt") {
		t.Errorf("expected 'system prompt' separator, got: %s", out)
	}
}

func TestCmdPrompt_EmptyPrompt(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.PromptProvider = &stubPromptProvider{text: ""}

	app.cmdPrompt(context.Background())

	out := p.Output()
	if !strings.Contains(out, "(no system prompt)") {
		t.Errorf("expected '(no system prompt)' message, got: %s", out)
	}
}

func TestCmdPrompt_Error(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.PromptProvider = &stubPromptProvider{err: context.DeadlineExceeded}

	app.cmdPrompt(context.Background())

	errOut := p.ErrOutput()
	if !strings.Contains(errOut, "Error loading prompt") {
		t.Errorf("expected error message in stderr, got: %s", errOut)
	}
}
