package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pixime-net/talk/internal/domain"
)

// --- cmdMemory tests ---

func TestCmdMemory_EmptyHistory(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.cmdMemory(context.Background())

	out := p.Output()
	if !strings.Contains(out, "no history for current session") {
		t.Errorf("expected 'no history' message, got: %s", out)
	}
}

func TestCmdMemory_ShowsTurns(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.turns[app.Scope.SessionID()] = []domain.HistoryTurn{
		{Question: "Hello", Answer: "Hi there!", At: time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)},
	}
	app.Sessions = sb

	app.cmdMemory(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Hello") {
		t.Errorf("expected question in output, got: %s", out)
	}
	if !strings.Contains(out, "Hi there!") {
		t.Errorf("expected answer in output, got: %s", out)
	}
	if !strings.Contains(out, "Turn 1") {
		t.Errorf("expected 'Turn 1' in output, got: %s", out)
	}
}

func TestCmdMemory_ShowsTitleAndMultipleCalls(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: app.Scope.SessionID(), Title: "My Topic"},
	}
	sb.turns[app.Scope.SessionID()] = []domain.HistoryTurn{
		{Question: "Q1", Answer: "A1", At: time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC), TurnID: "turn-1", CallCount: 3},
	}
	app.Sessions = sb

	app.cmdMemory(context.Background())

	out := p.Output()
	if !strings.Contains(out, "My Topic") {
		t.Errorf("expected session title 'My Topic', got: %s", out)
	}
	if !strings.Contains(out, "3 LLM calls") {
		t.Errorf("expected '3 LLM calls', got: %s", out)
	}
	if !strings.Contains(out, "turn-1") {
		t.Errorf("expected turnID 'turn-1', got: %s", out)
	}
}

// --- cmdSession dispatcher tests ---

func TestCmdSession_DefaultIsList(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: app.Scope.SessionID(), Title: "Chat", CreatedAt: time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC), TurnCount: 2},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("") // cancel

	app.cmdSession(context.Background(), "")

	out := p.Output()
	if !strings.Contains(out, "Sessions:") {
		t.Errorf("expected session list, got: %s", out)
	}
}

func TestCmdSession_UnknownSubcommand(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.cmdSession(context.Background(), "foo")

	out := p.Output()
	if !strings.Contains(out, "Unknown /session subcommand") {
		t.Errorf("expected unknown subcommand message, got: %s", out)
	}
}

// --- cmdSessionList tests ---

func TestCmdSessionList_Empty(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	app.Reader = newScriptReader("")

	app.cmdSessionList(context.Background())

	out := p.Output()
	if !strings.Contains(out, "no past sessions found") {
		t.Errorf("expected 'no past sessions found', got: %s", out)
	}
}

func TestCmdSessionList_ShowsSessionsWithTitleAndTurns(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: app.Scope.SessionID(), Title: "My Chat", CreatedAt: time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC), TurnCount: 5},
		{ID: "other-0000-0000-0000-000000000000", Title: "", CreatedAt: time.Date(2025, 3, 2, 10, 0, 0, 0, time.UTC), TurnCount: 1},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("")

	app.cmdSessionList(context.Background())

	out := p.Output()
	if !strings.Contains(out, "My Chat") {
		t.Errorf("expected title 'My Chat', got: %s", out)
	}
	if !strings.Contains(out, "(untitled)") {
		t.Errorf("expected '(untitled)' fallback, got: %s", out)
	}
	if !strings.Contains(out, "5 turns") {
		t.Errorf("expected '5 turns', got: %s", out)
	}
	if !strings.Contains(out, "\xe2\x86\x90 current") {
		t.Errorf("expected current marker, got: %s", out)
	}
}

func TestCmdSessionList_SwitchByNumber(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: "aaaa0000-0000-0000-0000-000000000000", Title: "first"},
		{ID: "bbbb0000-0000-0000-0000-000000000000", Title: "second"},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("2")

	app.cmdSessionList(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Switched to session") {
		t.Errorf("expected 'Switched to session', got: %s", out)
	}
	if app.Scope.SessionID() != "bbbb0000-0000-0000-0000-000000000000" {
		t.Errorf("expected session bbbb..., got: %s", app.Scope.SessionID())
	}
}

func TestCmdSessionList_NewFromMenu(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: "aaaa0000-0000-0000-0000-000000000000", Title: "old"},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("new")

	app.cmdSessionList(context.Background())

	out := p.Output()
	if !strings.Contains(out, "New session created") {
		t.Errorf("expected 'New session created', got: %s", out)
	}
}

func TestCmdSessionList_InvalidChoice(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: "aaaa0000-0000-0000-0000-000000000000"},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("99")

	app.cmdSessionList(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Invalid choice") {
		t.Errorf("expected 'Invalid choice', got: %s", out)
	}
}

func TestCmdSessionList_Cancel(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: "aaaa0000-0000-0000-0000-000000000000"},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("")

	app.cmdSessionList(context.Background())

	out := p.Output()
	if strings.Contains(out, "Switched") {
		t.Error("expected no switch on cancel")
	}
}

// --- cmdSessionNew tests ---

func TestCmdSessionNew_CreatesSession(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	origID := app.Scope.SessionID()
	app.cmdSessionNew(context.Background())

	out := p.Output()
	if !strings.Contains(out, "New session created") {
		t.Errorf("expected 'New session created', got: %s", out)
	}
	if app.Scope.SessionID() == origID {
		t.Error("expected sessionID to change")
	}
}

// --- cmdSessionRemove tests ---

func TestCmdSessionRemove_EmptyList(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.cmdSessionRemove(context.Background())

	out := p.Output()
	if !strings.Contains(out, "no sessions to remove") {
		t.Errorf("expected 'no sessions to remove', got: %s", out)
	}
}

func TestCmdSessionRemove_RemovesSession(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: app.Scope.SessionID(), Title: "current one", CreatedAt: time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC), TurnCount: 2},
		{ID: "other-0000-0000-0000-000000000000", Title: "other one", CreatedAt: time.Date(2025, 3, 2, 10, 0, 0, 0, time.UTC), TurnCount: 1},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("2") // select the non-current session

	app.cmdSessionRemove(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Removed session") {
		t.Errorf("expected 'Removed session', got: %s", out)
	}
	if len(sb.deleted) != 1 || sb.deleted[0] != "other-0000-0000-0000-000000000000" {
		t.Errorf("expected other session to be deleted, got: %v", sb.deleted)
	}
}

func TestCmdSessionRemove_CannotRemoveCurrent(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: app.Scope.SessionID(), Title: "current one"},
		{ID: "other-0000-0000-0000-000000000000", Title: "other one"},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("1") // select the current session

	app.cmdSessionRemove(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Cannot remove the current session") {
		t.Errorf("expected cannot-remove message, got: %s", out)
	}
	if len(sb.deleted) != 0 {
		t.Errorf("expected nothing deleted, got: %v", sb.deleted)
	}
}

func TestCmdSessionRemove_Cancel(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: "other-0000-0000-0000-000000000000", Title: "x"},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("")

	app.cmdSessionRemove(context.Background())

	if len(sb.deleted) != 0 {
		t.Errorf("expected nothing deleted on cancel, got: %v", sb.deleted)
	}
}

func TestCmdSessionRemove_InvalidChoice(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: "other-0000-0000-0000-000000000000", Title: "x"},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("99")

	app.cmdSessionRemove(context.Background())

	out := p.Output()
	if !strings.Contains(out, "Invalid choice") {
		t.Errorf("expected 'Invalid choice', got: %s", out)
	}
}

// --- additional coverage tests ---

func TestCmdSession_ListSubcommand(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: app.Scope.SessionID(), Title: "my session", CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("") // cancel

	app.cmdSession(context.Background(), "list")

	if !strings.Contains(p.Output(), "Sessions:") {
		t.Errorf("expected sessions list output, got: %s", p.Output())
	}
}

func TestCmdSession_NewSubcommand(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.cmdSession(context.Background(), "new")

	if !strings.Contains(p.Output(), "New session created") {
		t.Errorf("expected 'New session created', got: %s", p.Output())
	}
}

func TestCmdSession_RemoveSubcommand(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)

	app.cmdSession(context.Background(), "remove")

	if !strings.Contains(p.Output(), "no sessions to remove") {
		t.Errorf("expected 'no sessions to remove', got: %s", p.Output())
	}
}

func TestCmdSessionNew_WithManager(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	mgr := domain.NewConversationManager(domain.ConversationManagerConfig{
		Scope:          app.Scope,
		Store:          newFakeStore(),
		SessionBrowser: newFakeSessionBrowser(),
	})
	app.Manager = mgr

	origID := app.Scope.SessionID()
	app.cmdSessionNew(context.Background())

	if app.Scope.SessionID() == origID {
		t.Error("expected session ID to change after cmdSessionNew")
	}
}

func TestCmdSessionList_ListError(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.listErr = fmt.Errorf("db connection failed")
	app.Sessions = sb

	app.cmdSessionList(context.Background())

	if !strings.Contains(p.ErrOutput(), "db connection failed") {
		t.Errorf("expected error message in output, got: %s", p.ErrOutput())
	}
}

func TestCmdSessionList_SwitchWithManager(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: "target-000-0000-0000-000000000000", Title: "target"},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("1")
	mgr := domain.NewConversationManager(domain.ConversationManagerConfig{
		Scope:          app.Scope,
		Store:          newFakeStore(),
		SessionBrowser: newFakeSessionBrowser(),
	})
	app.Manager = mgr

	app.cmdSessionList(context.Background())

	if app.Scope.SessionID() != "target-000-0000-0000-000000000000" {
		t.Errorf("expected session to switch, got: %s", app.Scope.SessionID())
	}
}

func TestCmdSessionList_SwitchUntitledSession(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: "untitled-00-0000-0000-000000000000", Title: ""},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("1")

	app.cmdSessionList(context.Background())

	if !strings.Contains(p.Output(), "Switched to session") {
		t.Errorf("expected switch message, got: %s", p.Output())
	}
}

func TestCmdSessionRemove_ListError(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.listErr = fmt.Errorf("storage unavailable")
	app.Sessions = sb

	app.cmdSessionRemove(context.Background())

	if !strings.Contains(p.ErrOutput(), "storage unavailable") {
		t.Errorf("expected error in output, got: %s", p.ErrOutput())
	}
}

func TestCmdSessionRemove_DeleteError(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: app.Scope.SessionID(), Title: "current"},
		{ID: "other-0000-0000-0000-000000000000", Title: "other"},
	}
	sb.deleteErr = fmt.Errorf("delete failed")
	app.Sessions = sb
	app.Reader = newScriptReader("2")

	app.cmdSessionRemove(context.Background())

	if !strings.Contains(p.ErrOutput(), "delete failed") {
		t.Errorf("expected delete error in output, got: %s", p.ErrOutput())
	}
}

func TestCmdSessionRemove_UntitledSession(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.sessions = []domain.SessionSummary{
		{ID: app.Scope.SessionID(), Title: "current"},
		{ID: "other-0000-0000-0000-000000000000", Title: ""},
	}
	app.Sessions = sb
	app.Reader = newScriptReader("2")

	app.cmdSessionRemove(context.Background())

	if !strings.Contains(p.Output(), "Removed session") {
		t.Errorf("expected removal message, got: %s", p.Output())
	}
}

func TestCmdMemory_LoadError(t *testing.T) {
	p := &spyPrinter{}
	app := newTestApp(p)
	sb := newFakeSessionBrowser()
	sb.loadErr = fmt.Errorf("history unavailable")
	app.Sessions = sb

	app.cmdMemory(context.Background())

	if !strings.Contains(p.ErrOutput(), "history unavailable") {
		t.Errorf("expected load error in output, got: %s", p.ErrOutput())
	}
}
