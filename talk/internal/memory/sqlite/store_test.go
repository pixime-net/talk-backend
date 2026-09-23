package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pixime-net/talk/internal/domain"
)

var scope = domain.NewSessionScope("sess-1", "user1")

type messageStore interface {
	HandleMessageEvent(context.Context, domain.MessageEvent) error
	HandleTurnEvent(context.Context, domain.TurnEvent) error
}

type messageCleaner interface {
	ClearMessages(context.Context, string) error
}

var (
	sqliteLastTurnIDBySession   = map[string]string{}
	sqliteLastQuestionBySession = map[string]string{}
	sqliteTurnCounter           int64
)

func mustAddMessage(t *testing.T, store messageStore, msg domain.Message, scope domain.SessionScope) {
	t.Helper()

	if msg.TurnID == "" && msg.Role == domain.RoleUser {
		sqliteTurnCounter++
		msg.TurnID = fmt.Sprintf("turn-%d", sqliteTurnCounter)
	}
	if msg.TurnID == "" && msg.Role == domain.RoleAssistant {
		msg.TurnID = sqliteLastTurnIDBySession[scope.SessionID()]
	}

	if err := store.HandleMessageEvent(context.Background(), domain.MessageEvent{
		Message:      msg,
		SessionScope: scope,
	}); err != nil {
		t.Fatalf("HandleMessageEvent: %v", err)
	}

	if msg.Role == domain.RoleUser {
		sqliteLastTurnIDBySession[scope.SessionID()] = msg.TurnID
		sqliteLastQuestionBySession[scope.SessionID()] = msg.Content
		if err := store.HandleTurnEvent(context.Background(), domain.TurnEvent{
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
		if err := store.HandleTurnEvent(context.Background(), domain.TurnEvent{
			TurnID:       msg.TurnID,
			TurnSpanID:   "span-1",
			SessionScope: scope,
			Model:        domain.Model{Name: "test-model"},
			StartedAt:    time.Now(),
			EndedAt:      time.Now(),
			Input:        sqliteLastQuestionBySession[scope.SessionID()],
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

func newTestStore(t *testing.T) (*SqliteMessageRepository, *SqliteSessionRepository, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	r, b, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cleanup := func() { _ = r.db.conn.Close() }
	t.Cleanup(cleanup)
	return r, b, cleanup
}

func newStoreAtPath(dbPath string) (*SqliteMessageRepository, *SqliteSessionRepository, error) {
	conn, err := Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	messages, sessions, _, err := NewSqliteStore(conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return messages, sessions, nil
}

func TestStore_AddAndAll(t *testing.T) {
	s, _, _ := newTestStore(t)
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
	s, b, _ := newTestStore(t)

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
	s, b, _ := newTestStore(t)
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
	s, _, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hello"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "hi"}, scope)

	mustClearMessages(t, s, scope.SessionID())
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after clear, got %d", len(msgs))
	}
}

func TestStore_ClearUnmaterializedSession(t *testing.T) {
	s, _, _ := newTestStore(t)
	// Clear on unmaterialized session should not panic
	mustClearMessages(t, s, scope.SessionID())
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if msgs != nil {
		t.Fatalf("expected nil, got %v", msgs)
	}
}

func TestStore_MultiSession(t *testing.T) {
	s, _, _ := newTestStore(t)
	scope2 := domain.NewSessionScope("sess-2", "user1")

	// Add messages to first session
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "a1"}, scope)

	// New session has no messages yet
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
	s, b, _ := newTestStore(t)
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

func TestStore_ListSessionsFiltersByUserID(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	r, b, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.db.conn.Close() }()

	aliceScope := domain.NewSessionScope("sess-a", "alice")
	bobScope := domain.NewSessionScope("sess-b", "bob")

	mustAddMessage(t, r, domain.Message{Role: domain.RoleUser, Content: "alice msg"}, aliceScope)
	mustAddMessage(t, r, domain.Message{Role: domain.RoleUser, Content: "bob msg"}, bobScope)

	// Bob should only see his session
	sessions, _ := b.ListSessions(context.Background(), "bob")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session for bob, got %d", len(sessions))
	}
	if sessions[0].ID != "sess-b" {
		t.Errorf("expected bob's session, got %s", sessions[0].ID)
	}

	// Alice should only see her session
	sessions, _ = b.ListSessions(context.Background(), "alice")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session for alice, got %d", len(sessions))
	}
	if sessions[0].ID != "sess-a" {
		t.Errorf("expected alice's session, got %s", sessions[0].ID)
	}
}

func TestStore_ListSessionsCreatedAtIsSet(t *testing.T) {
	before := time.Now().Add(-time.Second)
	s, b, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hi"}, scope)
	after := time.Now().Add(time.Second)

	sessions, _ := b.ListSessions(context.Background(), "user1")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].CreatedAt.Before(before) || sessions[0].CreatedAt.After(after) {
		t.Errorf("createdAt %v not between %v and %v", sessions[0].CreatedAt, before, after)
	}
}

func TestStore_LoadSessionReturnsNilForUnknown(t *testing.T) {
	_, b, _ := newTestStore(t)
	turns, err := b.LoadHistoryTurnsFromSession(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if turns != nil {
		t.Fatalf("expected nil turns for unknown session, got %d", len(turns))
	}
}

func TestStore_LoadSessionBuildsTurns(t *testing.T) {
	s, b, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q1", TurnID: "t1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "a1", TurnID: "t1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q2", TurnID: "t2"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "a2", TurnID: "t2"}, scope)

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
	s, b, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q1", TurnID: "t1"}, scope)

	turns, _ := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].Question != "q1" || turns[0].Answer != "" {
		t.Errorf("turn 0: got Q=%q A=%q", turns[0].Question, turns[0].Answer)
	}
}

func TestStore_LoadSessionTimestampsAreSet(t *testing.T) {
	before := time.Now().Add(-time.Second)
	s, b, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q1", TurnID: "t1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "a1", TurnID: "t1"}, scope)
	after := time.Now().Add(time.Second)

	turns, _ := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].At.Before(before) || turns[0].At.After(after) {
		t.Errorf("turn timestamp %v not between %v and %v", turns[0].At, before, after)
	}
}

func TestStore_AllMessagesDoesNotShareState(t *testing.T) {
	s, _, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hello"}, scope)

	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	msgs[0].Content = "modified"

	original, _ := s.AllMessages(context.Background(), scope.SessionID())
	if original[0].Content != "hello" {
		t.Error("AllMessages() did not return independent data; modification affected store")
	}
}

func TestStore_PersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create store, add messages, close
	r1, _, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mustAddMessage(t, r1, domain.Message{Role: domain.RoleUser, Content: "persistent question"}, scope)
	mustAddMessage(t, r1, domain.Message{Role: domain.RoleAssistant, Content: "persistent answer"}, scope)
	_ = r1.db.conn.Close()

	// Reopen — messages should be available from disk
	r2, _, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r2.db.conn.Close() }()

	msgs, _ := r2.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after reopen, got %d", len(msgs))
	}
	if msgs[0].Content != "persistent question" {
		t.Errorf("expected %q, got %q", "persistent question", msgs[0].Content)
	}
	if msgs[1].Content != "persistent answer" {
		t.Errorf("expected %q, got %q", "persistent answer", msgs[1].Content)
	}
}

func TestStore_PersistenceSessionsListAfterReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	r1, _, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mustAddMessage(t, r1, domain.Message{Role: domain.RoleUser, Content: "q1"}, scope)
	_ = r1.db.conn.Close()

	// Reopen — should still list the session
	_, b2, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b2.db.conn.Close() }()

	sessions, _ := b2.ListSessions(context.Background(), "user1")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "sess-1" {
		t.Errorf("expected %q, got %q", "sess-1", sessions[0].ID)
	}
	if sessions[0].Title != "q1" {
		t.Errorf("expected title %q, got %q", "q1", sessions[0].Title)
	}
}

func TestStore_NewInvalidPath(t *testing.T) {
	_, _, err := newStoreAtPath(filepath.Join(string(os.PathSeparator), "nonexistent", "deeply", "nested", "test.db"))
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestStore_AddToolMessagePersistsToolMetadata(t *testing.T) {
	s, _, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q"}, scope)
	mustAddMessage(t, s, domain.Message{
		Role:    domain.RoleTool,
		Content: "get_weather",
		ToolCalls: []domain.ToolCall{{
			ID:    "call-1",
			Name:  "get_weather",
			Input: map[string]any{"city": "Paris"},
		}},
		ToolResults: []domain.ToolResult{{
			ToolCallID: "call-1",
			Content:    `{"temperature":"20C"}`,
		}},
	}, scope)

	var toolName, toolInput, toolOutput, toolCallID string
	err := s.conn.QueryRow(
		"SELECT tool_name, tool_input, tool_output, tool_call_id FROM messages WHERE session_id = ? AND role = ? ORDER BY id DESC LIMIT 1",
		scope.SessionID(),
		string(domain.RoleTool),
	).Scan(&toolName, &toolInput, &toolOutput, &toolCallID)
	if err != nil {
		t.Fatalf("query tool row: %v", err)
	}

	if toolName != "get_weather" {
		t.Fatalf("expected tool_name %q, got %q", "get_weather", toolName)
	}
	if toolCallID != "call-1" {
		t.Fatalf("expected tool_call_id %q, got %q", "call-1", toolCallID)
	}
	if toolOutput != `{"temperature":"20C"}` {
		t.Fatalf("unexpected tool_output: %q", toolOutput)
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(toolInput), &input); err != nil {
		t.Fatalf("unmarshal tool_input: %v", err)
	}
	if input["city"] != "Paris" {
		t.Fatalf("expected city Paris, got %v", input["city"])
	}
}

func TestStore_AssistantToolCallsRoundTrip(t *testing.T) {
	s, _, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q"}, scope)
	mustAddMessage(t, s, domain.Message{
		Role: domain.RoleAssistant,
		ToolCalls: []domain.ToolCall{
			{ID: "call-1", Name: "tool1", Input: map[string]any{"x": 1}},
			{ID: "call-2", Name: "tool2", Input: map[string]any{"x": 2}},
		},
	}, scope)

	// Re-read from DB to verify round-trip
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after reload, got %d", len(msgs))
	}
	if len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls after reload, got %d", len(msgs[1].ToolCalls))
	}
	if msgs[1].ToolCalls[0].Name != "tool1" || msgs[1].ToolCalls[1].Name != "tool2" {
		t.Fatalf("unexpected tool call names after reload: %+v", msgs[1].ToolCalls)
	}
}

// --- Error and edge-case tests to improve coverage ---

func TestStore_DeleteSession(t *testing.T) {
	s, b, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "hello", TurnID: "t1"}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "hi", TurnID: "t1"}, scope)

	err := b.DeleteSession(context.Background(), scope.SessionID())
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	// Session and messages should be gone.
	sessions, _ := b.ListSessions(context.Background(), "user1")
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d", len(sessions))
	}
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if msgs != nil {
		t.Errorf("expected nil messages after delete, got %d", len(msgs))
	}
	// History turns should be gone too.
	turns, _ := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if turns != nil {
		t.Errorf("expected nil turns after delete, got %d", len(turns))
	}
}

func TestStore_DeleteSession_NonExistent(t *testing.T) {
	_, b, _ := newTestStore(t)
	// Deleting a non-existent session should not error.
	err := b.DeleteSession(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestStore_AddMessage_AfterClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, _, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.db.conn.Close()

	if err := s.HandleMessageEvent(context.Background(), domain.MessageEvent{
		Message:      domain.Message{Role: domain.RoleUser, Content: "hello"},
		SessionScope: scope,
	}); err == nil {
		t.Fatal("expected error on closed DB")
	}
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if msgs != nil {
		t.Errorf("expected nil messages on closed DB, got %d", len(msgs))
	}
}

func TestStore_ClearMessages_AfterClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, _, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.db.conn.Close()

	if err := s.ClearMessages(context.Background(), scope.SessionID()); err == nil {
		t.Fatal("expected error on closed DB")
	}
}

func TestStore_ListSessions_AfterClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	_, b, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = b.db.conn.Close()

	_, err = b.ListSessions(context.Background(), "user1")
	if err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestStore_LoadHistoryTurns_AfterClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	_, b, err := newStoreAtPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = b.db.conn.Close()

	_, err = b.LoadHistoryTurnsFromSession(context.Background(), "sess-1")
	if err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestStore_HistoryTurn_UpsertOnMultipleMessages(t *testing.T) {
	s, b, _ := newTestStore(t)
	turnID := "turn-upsert-1"
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "question", TurnID: turnID}, scope)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "answer", TurnID: turnID}, scope)

	turns, err := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].Question != "question" || turns[0].Answer != "answer" {
		t.Errorf("got Q=%q A=%q", turns[0].Question, turns[0].Answer)
	}
}

func TestStore_HistoryTurn_ToolMessagesDoNotCreateTurns(t *testing.T) {
	s, b, _ := newTestStore(t)
	turnID := "turn-tool-1"
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "question", TurnID: turnID}, scope)
	// An assistant message WITH tool calls should not create a turn answer.
	mustAddMessage(t, s, domain.Message{
		Role:      domain.RoleAssistant,
		Content:   "calling tool",
		TurnID:    turnID,
		ToolCalls: []domain.ToolCall{{ID: "c1", Name: "t1", Input: map[string]any{}}},
	}, scope)
	// A tool result should not create a turn.
	mustAddMessage(t, s, domain.Message{
		Role:        domain.RoleTool,
		TurnID:      turnID,
		ToolResults: []domain.ToolResult{{ToolCallID: "c1", Content: "result"}},
	}, scope)
	// The final assistant answer without tool calls should update the turn.
	mustAddMessage(t, s, domain.Message{Role: domain.RoleAssistant, Content: "final answer", TurnID: turnID}, scope)

	turns, _ := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].Answer != "final answer" {
		t.Errorf("expected answer %q, got %q", "final answer", turns[0].Answer)
	}
}

func TestStore_ToolMessageWithoutToolCalls(t *testing.T) {
	s, _, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q"}, scope)
	// A tool message with only ToolResults (no ToolCalls) should still persist.
	mustAddMessage(t, s, domain.Message{
		Role:        domain.RoleTool,
		TurnID:      "t1",
		ToolResults: []domain.ToolResult{{ToolCallID: "call-99", Content: "output"}},
	}, scope)

	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	toolMsg := msgs[1]
	if len(toolMsg.ToolResults) != 1 || toolMsg.ToolResults[0].Content != "output" {
		t.Errorf("unexpected tool results: %+v", toolMsg.ToolResults)
	}
}

func TestStore_EmptyContentMessages(t *testing.T) {
	s, _, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "start"}, scope)
	// Assistant message with empty content but tool calls should be stored.
	mustAddMessage(t, s, domain.Message{
		Role:      domain.RoleAssistant,
		Content:   "",
		ToolCalls: []domain.ToolCall{{ID: "c1", Name: "tool1", Input: map[string]any{"a": "b"}}},
	}, scope)

	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if len(msgs[1].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(msgs[1].ToolCalls))
	}
}

func TestStore_LargeContent(t *testing.T) {
	s, _, _ := newTestStore(t)
	// Store a large message to verify no truncation.
	large := make([]byte, 100_000)
	for i := range large {
		large[i] = 'A' + byte(i%26)
	}
	content := string(large)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: content}, scope)
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != content {
		t.Error("large content was truncated or corrupted")
	}
}

func TestStore_TurnIDPersisted(t *testing.T) {
	s, _, _ := newTestStore(t)
	mustAddMessage(t, s, domain.Message{Role: domain.RoleUser, Content: "q", TurnID: "turn-abc"}, scope)
	msgs, _ := s.AllMessages(context.Background(), scope.SessionID())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].TurnID != "turn-abc" {
		t.Errorf("expected TurnID %q, got %q", "turn-abc", msgs[0].TurnID)
	}
}

func TestStore_HandleTurnEvent_StatusPersisted(t *testing.T) {
	s, b, _ := newTestStore(t)

	// Materialize session first.
	if err := s.HandleMessageEvent(context.Background(), domain.MessageEvent{
		Message:      domain.Message{Role: domain.RoleUser, Content: "hello", TurnID: "t1"},
		SessionScope: scope,
	}); err != nil {
		t.Fatal(err)
	}

	// Persist a complete turn.
	if err := s.HandleTurnEvent(context.Background(), domain.TurnEvent{
		TurnID:       "t1",
		TurnSpanID:   "s1",
		SessionScope: scope,
		Model:        domain.Model{Name: "m"},
		StartedAt:    time.Now(),
		EndedAt:      time.Now(),
		Input:        "hello",
		Output:       "world",
		Status:       domain.TurnStatusComplete,
	}); err != nil {
		t.Fatal(err)
	}

	// Persist an incomplete turn.
	if err := s.HandleMessageEvent(context.Background(), domain.MessageEvent{
		Message:      domain.Message{Role: domain.RoleUser, Content: "loop", TurnID: "t2"},
		SessionScope: scope,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleTurnEvent(context.Background(), domain.TurnEvent{
		TurnID:       "t2",
		TurnSpanID:   "s2",
		SessionScope: scope,
		Model:        domain.Model{Name: "m"},
		StartedAt:    time.Now(),
		EndedAt:      time.Now(),
		Input:        "loop",
		Output:       "",
		Status:       domain.TurnStatusIncomplete,
	}); err != nil {
		t.Fatal(err)
	}

	turns, err := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}

	if turns[0].TurnID != "t1" || turns[0].Status != domain.TurnStatusComplete {
		t.Errorf("turn 0: id=%q status=%q, want t1/complete", turns[0].TurnID, turns[0].Status)
	}
	if turns[1].TurnID != "t2" || turns[1].Status != domain.TurnStatusIncomplete {
		t.Errorf("turn 1: id=%q status=%q, want t2/incomplete", turns[1].TurnID, turns[1].Status)
	}
}

func TestStore_HandleTurnEvent_EmptyStatusDefaultsToComplete(t *testing.T) {
	s, b, _ := newTestStore(t)

	// Materialize session.
	if err := s.HandleMessageEvent(context.Background(), domain.MessageEvent{
		Message:      domain.Message{Role: domain.RoleUser, Content: "hi", TurnID: "t1"},
		SessionScope: scope,
	}); err != nil {
		t.Fatal(err)
	}

	// Persist turn with empty status — should default to "complete".
	if err := s.HandleTurnEvent(context.Background(), domain.TurnEvent{
		TurnID:       "t1",
		TurnSpanID:   "s1",
		SessionScope: scope,
		Model:        domain.Model{Name: "m"},
		StartedAt:    time.Now(),
		EndedAt:      time.Now(),
		Input:        "hi",
		Output:       "hey",
		Status:       "",
	}); err != nil {
		t.Fatal(err)
	}

	turns, err := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].Status != domain.TurnStatusComplete {
		t.Errorf("status = %q, want %q", turns[0].Status, domain.TurnStatusComplete)
	}
}

func TestStore_HandleTurnEvent_InterruptFieldsPersisted(t *testing.T) {
	s, b, _ := newTestStore(t)

	// Materialize session.
	if err := s.HandleMessageEvent(context.Background(), domain.MessageEvent{
		Message:      domain.Message{Role: domain.RoleUser, Content: "hi", TurnID: "t1"},
		SessionScope: scope,
	}); err != nil {
		t.Fatal(err)
	}

	// Persist turn with interrupt fields.
	if err := s.HandleTurnEvent(context.Background(), domain.TurnEvent{
		TurnID:          "t1",
		TurnSpanID:      "s1",
		SessionScope:    scope,
		Model:           domain.Model{Name: "m"},
		StartedAt:       time.Now(),
		EndedAt:         time.Now(),
		Input:           "do stuff",
		Output:          "",
		Status:          domain.TurnStatusIncomplete,
		InterruptID:     "int-abc",
		InterruptReason: "talk:max_iterations",
		InterruptState:  domain.InterruptStateOpen,
	}); err != nil {
		t.Fatal(err)
	}

	turns, err := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].InterruptID != "int-abc" {
		t.Errorf("interrupt_id = %q, want %q", turns[0].InterruptID, "int-abc")
	}
	if turns[0].InterruptReason != "talk:max_iterations" {
		t.Errorf("interrupt_reason = %q, want %q", turns[0].InterruptReason, "talk:max_iterations")
	}
	if turns[0].InterruptState != domain.InterruptStateOpen {
		t.Errorf("interrupt_state = %q, want %q", turns[0].InterruptState, domain.InterruptStateOpen)
	}
}

func TestStore_Migration_ColumnsExistOnFreshDB(t *testing.T) {
	s, b, _ := newTestStore(t)

	// Materialize session and insert a turn — should work even on brand-new DB
	// (all columns created via schema, migration is a no-op).
	if err := s.HandleMessageEvent(context.Background(), domain.MessageEvent{
		Message:      domain.Message{Role: domain.RoleUser, Content: "test", TurnID: "t1"},
		SessionScope: scope,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleTurnEvent(context.Background(), domain.TurnEvent{
		TurnID:       "t1",
		TurnSpanID:   "s1",
		SessionScope: scope,
		Model:        domain.Model{Name: "m"},
		StartedAt:    time.Now(),
		EndedAt:      time.Now(),
		Input:        "test",
		Output:       "reply",
	}); err != nil {
		t.Fatal(err)
	}

	turns, err := b.LoadHistoryTurnsFromSession(context.Background(), scope.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	// Default values for new columns.
	if turns[0].InterruptID != "" {
		t.Errorf("interrupt_id = %q, want empty", turns[0].InterruptID)
	}
	if turns[0].InterruptState != "" {
		t.Errorf("interrupt_state = %q, want empty", turns[0].InterruptState)
	}
}
