package main

import (
	"strings"
	"testing"

	"github.com/pixime-net/talk/internal/domain"
)

func newThinkingTestApp(p *spyPrinter) *App {
	app := newTestApp(p)
	store := newFakeStore()
	app.Messages = store
	app.Manager = domain.NewConversationManager(domain.ConversationManagerConfig{
		Client: fakeLlmClient{}, Model: domain.Model{Name: "sonnet-4.6", OTLPProvider: domain.OTLPProviderAnthropic}, Scope: app.Scope,
		Store:          store,
		SessionBrowser: newFakeSessionBrowser(), PromptProvider: &stubPromptProvider{},
		Tools: func() []domain.Tool { return nil }, MaxConcurrentTools: 1, ContextFullTurns: -1,
	})
	return app
}

func TestCmdThinking_DefaultCurrentAndReadError(t *testing.T) {
	p := &spyPrinter{}
	app := newThinkingTestApp(p)

	// No scripted input means ReadLine returns an error and command exits.
	app.Reader = newScriptReader()
	app.cmdThinking()

	out := p.Output()
	if !strings.Contains(out, "Thinking levels") {
		t.Fatalf("expected thinking list header in output, got: %s", out)
	}
	if !strings.Contains(out, "current") {
		t.Fatalf("expected current marker in output, got: %s", out)
	}
	if app.Manager.ThinkingEffort() != "" {
		t.Fatalf("expected unchanged effort when read fails, got: %q", app.Manager.ThinkingEffort())
	}
}

func TestCmdThinking_InvalidChoiceKeepsCurrent(t *testing.T) {
	p := &spyPrinter{}
	app := newThinkingTestApp(p)
	app.Manager.SetThinkingEffort(domain.ThinkingMedium)
	app.Reader = newScriptReader("999")

	app.cmdThinking()

	out := p.Output()
	if !strings.Contains(out, "Invalid choice") {
		t.Fatalf("expected invalid choice warning, got: %s", out)
	}
	if app.Manager.ThinkingEffort() != domain.ThinkingMedium {
		t.Fatalf("expected thinking to remain medium, got: %q", app.Manager.ThinkingEffort())
	}
}

func TestCmdThinking_ValidChoiceOff(t *testing.T) {
	p := &spyPrinter{}
	app := newThinkingTestApp(p)
	app.Manager.SetThinkingEffort(domain.ThinkingHigh)
	app.Reader = newScriptReader("1")

	app.cmdThinking()

	if app.Manager.ThinkingEffort() != domain.ThinkingOff {
		t.Fatalf("expected thinking off, got: %q", app.Manager.ThinkingEffort())
	}
	out := p.Output()
	if !strings.Contains(out, "Thinking set to") || !strings.Contains(out, "off") {
		t.Fatalf("expected off confirmation, got: %s", out)
	}
}

func TestCmdThinking_ValidChoiceHighWithTrim(t *testing.T) {
	p := &spyPrinter{}
	app := newThinkingTestApp(p)
	app.Manager.SetThinkingEffort(domain.ThinkingOff)
	app.Reader = newScriptReader(" 4 ")

	app.cmdThinking()

	if app.Manager.ThinkingEffort() != domain.ThinkingHigh {
		t.Fatalf("expected thinking high, got: %q", app.Manager.ThinkingEffort())
	}
	out := p.Output()
	if !strings.Contains(out, "Thinking set to") || !strings.Contains(out, string(domain.ThinkingHigh)) {
		t.Fatalf("expected high confirmation, got: %s", out)
	}
}
