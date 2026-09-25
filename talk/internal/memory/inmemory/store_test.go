package inmemory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pixime-net/talk/internal/domain"
)

var scope = domain.NewSessionScope("sess-1", "user1")

type messageStore interface {
	HandleMessageEvent(context.Context, domain.MessageEvent) error
	HandleTurnEvent(context.Context, domain.TurnEndEvent) error
}

type messageCleaner interface {
	ClearMessages(context.Context, string) error
}

var (
	lastTurnIDBySession   = map[string]string{}
	lastQuestionBySession = map[string]string{}
	turnCounter           int64
)

func mustAddMessage(t *testing.T, store messageStore, msg domain.Message, scope domain.SessionScope) {
	t.Helper()

	if msg.TurnID == "" && msg.Role == domain.RoleUser {
		turnCounter++
		msg.TurnID = fmt.Sprintf("turn-%d", turnCounter)
	}
	if msg.TurnID == "" && msg.Role == domain.RoleAssistant {
		msg.TurnID = lastTurnIDBySession[scope.SessionID()]
	}

	if err := store.HandleMessageEvent(context.Background(), domain.MessageEvent{
		Message:      msg,
		SessionScope: scope,
	}); err != nil {
		t.Fatalf("HandleMessageEvent: %v", err)
	}

	if msg.Role == domain.RoleUser {
		lastTurnIDBySession[scope.SessionID()] = msg.TurnID
		lastQuestionBySession[scope.SessionID()] = msg.Content
		if err := store.HandleTurnEvent(context.Background(), domain.TurnEndEvent{
			TurnID:       msg.TurnID,
			TurnSpanID:   "span-1",
			SessionScope: scope,
			Model:        domain.Model{Name: "test-model"},
			StartedAt:    time.Now(),
			EndedAt:      time.Now(),
			Input:        msg.Content,
			Output:       "",
			CallCount:    0,
		}); err != nil {
			t.Fatalf("HandleTurnEvent(user): %v", err)
		}
	}

	if msg.Role == domain.RoleAssistant && msg.Content != "" && len(msg.ToolCalls) == 0 {
		if err := store.HandleTurnEvent(context.Background(), domain.TurnEndEvent{
			TurnID:       msg.TurnID,
			TurnSpanID:   "span-1",
			SessionScope: scope,
			Model:        domain.Model{Name: "test-model"},
			StartedAt:    time.Now(),
			EndedAt:      time.Now(),
			Input:        lastQuestionBySession[scope.SessionID()],
			Output:       msg.Content,
			CallCount:    1,
		}); err != nil {
			t.Fatalf("HandleTurnEvent: %v", err)
		}
	}
}

func mustClearMessages(t *testing.T, store messageCleaner, sessionID string) {
	t.Helper()
	if err := store.ClearMessages(context.Background(), sessionID); err != nil {
		t.Fatalf("ClearMessages: %v", err)
	}
}

func TestStore_AddAndAll(t *testing.T) {
	s, _ := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hello"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "world"}, scope)

	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" || msgs[1].Content != "world" {
		t.Error("unexpected message contents")
	}
}

func TestStore_SessionNotMaterializedUntilUserMessage(t *testing.T) {
	s, b := New()

	// Before any message, no sessions exist
	sessions, err := b.ListSessions(context.Background(), "user1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}

	// Adding an assistant message should NOT materialize the session
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "hi there"}, scope)
	sessions, _ = b.ListSessions(context.Background(), "user1")
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after assistant msg, got %d", len(sessions))
	}

	// AllMessages should return nil for unmaterialized session
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if msgs != nil {
		t.Fatalf("expected nil messages for unmaterialized session, got %d", len(msgs))
	}

	// Adding a user message materializes the session
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "question"}, scope)
	sessions, _ = b.ListSessions(context.Background(), "user1")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session after user msg, got %d", len(sessions))
	}
}

func TestStore_TitleSetFromFirstUserMessage(t *testing.T) {
	s, b := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "What is Go?"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "A programming language."}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "Tell me more"}, scope)

	sessions, _ := b.ListSessions(context.Background(), "user1")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Title != "What is Go?" {
		t.Errorf("expected title %q, got %q", "What is Go?", sessions[0].Title)
	}
}

func TestStore_Clear(t *testing.T) {
	s, _ := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hello"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "hi"}, scope)

	mustClearMessages(t, s, scope.SessionID())
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after clear, got %d", len(msgs))
	}
}

func TestStore_ClearUnmaterializedSession(t *testing.T) {
	s, _ := New()
	// Clear on unmaterialized session should not panic
	mustClearMessages(t, s, scope.SessionID())
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if msgs != nil {
		t.Fatalf("expected nil, got %v", msgs)
	}
}

func TestStore_MultiSession(t *testing.T) {
	s, _ := New()
	scope2 := domain.NewSessionScope("sess-2", "user1")

	// Add messages to first session
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "a1"}, scope)

	// Second session has no messages yet
	msgs, _ := s.AllMessages(context.Background(), scope2.SessionID())
	if msgs != nil {
		t.Fatalf("expected nil messages for new session, got %d", len(msgs))
	}

	// Add message to second session
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q2"}, scope2)
	msgs, _ = s.AllMessages(context.Background(), scope2.SessionID())
	if len(msgs) != 1 || msgs[0].Content != "q2" {
		t.Errorf("unexpected messages in session 2: %v", msgs)
	}

	// First session still has its messages
	msgs, _ = s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in session 1, got %d", len(msgs))
	}
	if msgs[0].Content != "q1" {
		t.Errorf("expected first message %q, got %q", "q1", msgs[0].Content)
	}
}

func TestStore_ListSessionsMultiple(t *testing.T) {
	s, b := New()
	scope2 := domain.NewSessionScope("sess-2", "user1")

	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "first"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "reply1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "second"}, scope)

	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hello sess2"}, scope2)

	sessions, err := b.ListSessions(context.Background(), "user1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Find each session and verify turn counts
	for _, sess := range sessions {
		switch sess.ID {
		case "sess-1":
			if sess.TurnCount != 2 {
				t.Errorf("sess-1: expected 2 turns, got %d", sess.TurnCount)
			}
			if sess.Title != "first" {
				t.Errorf("sess-1: expected title %q, got %q", "first", sess.Title)
			}
		case "sess-2":
			if sess.TurnCount != 1 {
				t.Errorf("sess-2: expected 1 turn, got %d", sess.TurnCount)
			}
			if sess.Title != "hello sess2" {
				t.Errorf("sess-2: expected title %q, got %q", "hello sess2", sess.Title)
			}
		default:
			t.Errorf("unexpected session ID: %s", sess.ID)
		}
	}
}

func TestStore_ListSessionsCreatedAtIsSet(t *testing.T) {
	before := time.Now()
	s, b := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hi"}, scope)
	after := time.Now()

	sessions, _ := b.ListSessions(context.Background(), "user1")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].CreatedAt.Before(before) || sessions[0].CreatedAt.After(after) {
		t.Errorf("createdAt %v not between %v and %v", sessions[0].CreatedAt, before, after)
	}
}

func TestStore_LoadSessionReturnsNilForUnknown(t *testing.T) {
	_, b := New()
	turns, err := b.LoadHistoryTurnsFromSession(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if turns != nil {
		t.Fatalf("expected nil turns for unknown session, got %d", len(turns))
	}
}

func TestStore_LoadSessionBuildsTurns(t *testing.T) {
	s, b := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "a1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q2"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "a2"}, scope)

	turns, err := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[0].Question != "q1" || turns[0].Answer != "a1" {
		t.Errorf("turn 0: got Q=%q A=%q", turns[0].Question, turns[0].Answer)
	}
	if turns[1].Question != "q2" || turns[1].Answer != "a2" {
		t.Errorf("turn 1: got Q=%q A=%q", turns[1].Question, turns[1].Answer)
	}
}

func TestStore_LoadSessionUserWithoutAnswer(t *testing.T) {
	s, b := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q1"}, scope)
	// No assistant reply

	turns, _ := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].Question != "q1" || turns[0].Answer != "" {
		t.Errorf("turn 0: got Q=%q A=%q", turns[0].Question, turns[0].Answer)
	}
}

func TestStore_LoadSessionTimestampsAreSet(t *testing.T) {
	before := time.Now()
	s, b := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "a1"}, scope)
	after := time.Now()

	turns, _ := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].At.Before(before) || turns[0].At.After(after) {
		t.Errorf("turn timestamp %v not between %v and %v", turns[0].At, before, after)
	}
}

func TestStore_AllReturnsCopy(t *testing.T) {
	s, _ := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hello"}, scope)

	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	msgs[0].Content = "modified"

	original, _ := s.AllMessages(context.Background(), scope.SessionID())
	if original[0].Content != "hello" {
		t.Error("AllMessages() did not return a copy; modification affected store")
	}
}

func TestStore_DeleteSession(t *testing.T) {
	s, b := New()
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "msg"}, scope)

	if err := b.DeleteSession(context.Background(), scope.SessionID()); err != nil {
		t.Fatal(err)
	}

	sessions, _ := b.ListSessions(context.Background(), "user1")
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after delete, got %d", len(sessions))
	}
}
