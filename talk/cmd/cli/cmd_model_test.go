package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/pixime-net/talk/internal/domain"
)

func TestCmdModel_ValidSelection(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	store := newFakeStore()
	app.Messages = store
	app.Manager = domain.NewConversationManager(domain.ConversationManagerConfig{
		Client: fakeLlmClient{}, Model: domain.Model{Name: "sonnet-4.6", OTLPProvider: domain.OTLPProviderAnthropic}, SessionScope: app.Scope,
		MessageRepository: store,
		SessionRepository: newFakeSessionBrowser(), PromptProvider: &stubPromptProvider{},
		Tools: func() []domain.Tool { return nil }, MaxConcurrentTools: 1, ContextFullTurns: -1,
	})

	// Find the index of a model different from the current one.
	models := domain.SupportedModels()
	slices.Sort(models)
	targetIdx := -1
	for i, m := range models {
		if string(m) != "sonnet-4.6" {
			targetIdx = i + 1
			break
		}
	}
	if targetIdx == -1 {
		t.Skip("need at least 2 models")
	}

	app.Reader = newScriptReader(fmt.Sprintf("%d", targetIdx))
	app.cmdModel()

	out := p.Output()
	if !strings.Contains(out, "Switched to") {
		t.Errorf("expected 'Switched to' in output, got: %s", out)
	}
	if app.CurrentModel == "sonnet-4.6" {
		t.Error("expected CurrentModel to change")
	}
}

func TestCmdModel_InvalidChoice(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.Reader = newScriptReader("999")

	app.cmdModel()

	out := p.Output()
	if !strings.Contains(out, "Invalid choice") {
		t.Errorf("expected 'Invalid choice' in output, got: %s", out)
	}
}

func TestCmdModel_NonNumericChoice(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.Reader = newScriptReader("abc")

	app.cmdModel()

	out := p.Output()
	if !strings.Contains(out, "Invalid choice") {
		t.Errorf("expected 'Invalid choice' in output, got: %s", out)
	}
}

func TestCmdModel_RouterError(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.Router = &fakeRouter{err: fmt.Errorf("missing API key")}
	store := newFakeStore()
	app.Messages = store
	app.Manager = domain.NewConversationManager(domain.ConversationManagerConfig{
		Client: fakeLlmClient{}, Model: domain.Model{Name: "sonnet-4.6", OTLPProvider: domain.OTLPProviderAnthropic}, SessionScope: app.Scope,
		MessageRepository: store,
		SessionRepository: newFakeSessionBrowser(), PromptProvider: &stubPromptProvider{},
		Tools: func() []domain.Tool { return nil }, MaxConcurrentTools: 1, ContextFullTurns: -1,
	})

	// Pick any valid model index
	app.Reader = newScriptReader("1")
	app.cmdModel()

	errOut := p.ErrOutput()
	if !strings.Contains(errOut, "Error building client") {
		t.Errorf("expected error on stderr, got: %s", errOut)
	}
}

func TestCmdModel_ListsCurrentModel(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.Reader = newScriptReader("") // empty → error → returns early

	app.cmdModel()

	out := p.Output()
	if !strings.Contains(out, "Available models") {
		t.Errorf("expected 'Available models' header, got: %s", out)
	}
	if !strings.Contains(out, "← current") {
		t.Errorf("expected '← current' marker, got: %s", out)
	}
}

func TestCmdHelp_ContainsAllCommands(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.cmdHelp()

	out := p.Output()
	commands := []string{"/help", "/model", "/memory", "/session", "/prompt", "/mcp", "/q"}
	for _, cmd := range commands {
		if !strings.Contains(out, cmd) {
			t.Errorf("expected %q in help output", cmd)
		}
	}
}
