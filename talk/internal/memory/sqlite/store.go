package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/pixime-net/talk/internal/domain"

	_ "modernc.org/sqlite" // registers the SQLite driver with database/sql
)

const timeFormat = "2006-01-02T15:04:05Z"

// ErrStore and storeErr are used to wrap storage-layer errors consistently.

type ErrStore struct{ Err error }

func (e *ErrStore) Error() string { return e.Err.Error() }
func (e *ErrStore) Unwrap() error { return e.Err }

func storeErr(msg string, err error) error {
	return &ErrStore{Err: fmt.Errorf("%s: %w", msg, err)}
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL,
	title      TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id),
	role       TEXT NOT NULL,
	content    TEXT NOT NULL DEFAULT '',
	tool_name  TEXT NOT NULL DEFAULT '',
	tool_input TEXT NOT NULL DEFAULT '',
	tool_output TEXT NOT NULL DEFAULT '',
	tool_output_client TEXT NOT NULL DEFAULT '',
	tool_call_id TEXT NOT NULL DEFAULT '',
	turn_id    TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS history_turns (
	session_id       TEXT NOT NULL REFERENCES sessions(id),
	turn_id          TEXT NOT NULL,
	question         TEXT NOT NULL DEFAULT '',
	answer           TEXT NOT NULL DEFAULT '',
	question_at      DATETIME,
	answer_at        DATETIME,
	status           TEXT NOT NULL DEFAULT 'complete',
	interrupt_id     TEXT NOT NULL DEFAULT '',
	interrupt_reason TEXT NOT NULL DEFAULT '',
	interrupt_state  TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (session_id, turn_id)
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_tool_name ON messages(tool_name);
CREATE INDEX IF NOT EXISTS idx_messages_tool_call_id ON messages(tool_call_id);
CREATE INDEX IF NOT EXISTS idx_history_turns_session_id ON history_turns(session_id);
`

// db is the shared database handle for both MessageRepository and Browser.
type db struct {
	conn *sql.DB
	mu   sync.RWMutex
}

// SqliteMessageRepository implements domain.MessageStore backed by SQLite.
type SqliteMessageRepository struct{ *db }

// SqliteSessionRepository implements domain.SessionRepository backed by SQLite.
type SqliteSessionRepository struct{ *db }

// SqliteMessageEventHandler implements domain.MessageEventHandler backed by SQLite.
type SqliteMessageEventHandler struct{ *db }

var _ domain.MessageRepository = (*SqliteMessageRepository)(nil)
var _ domain.SessionRepository = (*SqliteSessionRepository)(nil)
var _ domain.EventHandler = (*SqliteMessageEventHandler)(nil)

// Open opens and initializes a SQLite database at dbPath.
func Open(dbPath string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, storeErr("opening sqlite db", err)
	}
	// Enable WAL mode for better concurrency.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = conn.Close()
		return nil, storeErr("setting WAL mode", err)
	}

	// SQLite supports only one writer at a time.
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0)

	if _, err := conn.Exec(schema); err != nil {
		_ = conn.Close()
		return nil, storeErr("creating schema", err)
	}

	return conn, nil
}

// NewSqliteStore creates SQLite-backed repositories using conn.
func NewSqliteStore(conn *sql.DB) (*SqliteMessageRepository, *SqliteSessionRepository, *SqliteMessageEventHandler, error) {
	if conn == nil {
		return nil, nil, nil, fmt.Errorf("sqlite connection is nil")
	}

	db := &db{conn: conn}
	return &SqliteMessageRepository{db}, &SqliteSessionRepository{db}, &SqliteMessageEventHandler{db}, nil
}

// HandleMessageEvent appends a message to the given session.
// The session is materialized in the database only on the first user message.
func (r *SqliteMessageEventHandler) HandleMessageEvent(ctx context.Context, event domain.MessageEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg := event.Message
	scope := event.SessionScope

	materialized, err := r.isSessionMaterialized(ctx, scope.SessionID())
	if err != nil {
		return err
	}
	if !materialized {
		if msg.Role != domain.RoleUser {
			return nil
		}
		if err := r.materializeSession(ctx, scope, msg.Content); err != nil {
			return err
		}
	}

	now := time.Now().UTC().Format(timeFormat)
	content := msg.Content
	var tool toolRoleFields

	if msg.Role == domain.RoleAssistant {
		tool.input = buildAssistantToolInput(msg.ToolCalls)
	}
	if msg.Role == domain.RoleTool {
		tool = buildToolRoleFields(msg)
	}

	if _, err := r.conn.ExecContext(ctx,
		"INSERT INTO messages (session_id, role, content, tool_name, tool_input, tool_output, tool_output_client, tool_call_id, turn_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		scope.SessionID(), string(msg.Role), content, tool.name, tool.input, tool.output, tool.clientOutput, tool.callID, msg.TurnID, now,
	); err != nil {
		return storeErr("inserting message", err)
	}

	return nil
}

// HandleTurnEndEvent persists one completed turn into history_turns.
func (r *SqliteMessageEventHandler) HandleTurnEndEvent(ctx context.Context, event domain.TurnEndEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if event.TurnID == "" {
		return nil
	}

	questionAt := any(nil)
	answerAt := any(nil)
	if !event.StartedAt.IsZero() {
		questionAt = event.StartedAt.UTC().Format(timeFormat)
	}
	if !event.EndedAt.IsZero() {
		answerAt = event.EndedAt.UTC().Format(timeFormat)
	}

	status := event.Status
	if status == "" {
		status = domain.TurnStatusComplete
	}

	if _, err := r.conn.ExecContext(ctx,
		`INSERT INTO history_turns (session_id, turn_id, question, answer, question_at, answer_at, status, interrupt_id, interrupt_reason, interrupt_state)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(session_id, turn_id) DO UPDATE SET
		   question = CASE WHEN excluded.question <> '' THEN excluded.question ELSE history_turns.question END,
		   answer = CASE WHEN excluded.answer <> '' THEN excluded.answer ELSE history_turns.answer END,
		   question_at = COALESCE(excluded.question_at, history_turns.question_at),
		   answer_at = COALESCE(excluded.answer_at, history_turns.answer_at),
		   status = excluded.status,
		   interrupt_id = CASE WHEN excluded.interrupt_id <> '' THEN excluded.interrupt_id ELSE history_turns.interrupt_id END,
		   interrupt_reason = CASE WHEN excluded.interrupt_reason <> '' THEN excluded.interrupt_reason ELSE history_turns.interrupt_reason END,
		   interrupt_state = CASE WHEN excluded.interrupt_state <> '' THEN excluded.interrupt_state ELSE history_turns.interrupt_state END`,
		event.SessionScope.SessionID(),
		event.TurnID,
		event.Input,
		event.Output,
		questionAt,
		answerAt,
		status,
		event.InterruptID,
		event.InterruptReason,
		event.InterruptState,
	); err != nil {
		return storeErr("upserting history turn", err)
	}

	return nil
}

// HandleToolStartEvent is a no-op for the SQLite store.
func (r *SqliteMessageEventHandler) HandleToolStartEvent(_ context.Context, _ domain.ToolStartEvent) error {
	return nil
}

// HandleToolEndEvent is a no-op for the SQLite store.
func (r *SqliteMessageEventHandler) HandleToolEndEvent(_ context.Context, _ domain.ToolEndEvent) error {
	return nil
}

func (r *SqliteMessageRepository) HandleMessageEvent(ctx context.Context, event domain.MessageEvent) error {
	return (&SqliteMessageEventHandler{r.db}).HandleMessageEvent(ctx, event)
}

func (r *SqliteMessageRepository) HandleTurnEvent(ctx context.Context, event domain.TurnEndEvent) error {
	return (&SqliteMessageEventHandler{r.db}).HandleTurnEndEvent(ctx, event)
}

func (r *SqliteMessageEventHandler) isSessionMaterialized(ctx context.Context, sessionID string) (bool, error) {
	var count int
	if err := r.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE id = ?", sessionID).Scan(&count); err != nil {
		return false, storeErr("checking session existence", err)
	}
	return count > 0, nil
}

// materializeSession inserts a new session row with the given title.
func (r *SqliteMessageEventHandler) materializeSession(ctx context.Context, scope domain.SessionScope, title string) error {
	_, err := r.conn.ExecContext(ctx,
		"INSERT INTO sessions (id, user_id, title, created_at) VALUES (?, ?, ?, ?)",
		scope.SessionID(), scope.UserID(), title, time.Now().UTC().Format(timeFormat),
	)
	if err != nil {
		return storeErr("materializing session", err)
	}
	return nil
}

// buildAssistantToolInput marshals the tool calls of an assistant message into JSON.
// Returns an empty string when there are no calls or marshaling fails.
func buildAssistantToolInput(toolCalls []domain.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	rawCalls, err := json.Marshal(toolCalls)
	if err != nil {
		return ""
	}
	return string(rawCalls)
}

// toolRoleFields holds the storage columns of a tool-role message.
type toolRoleFields struct {
	name         string
	input        string
	output       string
	clientOutput string
	callID       string
}

// buildToolRoleFields extracts the storage fields for a tool-role message.
func buildToolRoleFields(msg domain.Message) toolRoleFields {
	var f toolRoleFields
	if len(msg.ToolCalls) > 0 {
		f.name = msg.ToolCalls[0].Name
		f.callID = msg.ToolCalls[0].ID
		rawInput, err := json.Marshal(msg.ToolCalls[0].Input)
		if err == nil {
			f.input = string(rawInput)
		}
	}
	if len(msg.ToolResults) > 0 {
		f.output = msg.ToolResults[0].Content
		f.clientOutput = msg.ToolResults[0].ClientContent
		if f.callID == "" {
			f.callID = msg.ToolResults[0].ToolCallID
		}
	}
	return f
}

// AllMessages returns all messages for the given session.
func (r *SqliteMessageRepository) AllMessages(ctx context.Context, sessionID string) ([]domain.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rows, err := r.conn.QueryContext(ctx,
		"SELECT role, content, tool_name, tool_input, tool_output, tool_output_client, tool_call_id, turn_id FROM messages WHERE session_id = ? ORDER BY id",
		sessionID,
	)
	if err != nil {
		return nil, storeErr("querying messages", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []domain.Message
	for rows.Next() {
		msg, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, storeErr("iterating messages", err)
	}
	return messages, nil
}

// scanMessageRow scans one row from the messages query and reconstructs the domain.Message.
func scanMessageRow(rows *sql.Rows) (domain.Message, error) {
	var role, content, turnID string
	var tool toolRoleFields
	if err := rows.Scan(&role, &content, &tool.name, &tool.input, &tool.output, &tool.clientOutput, &tool.callID, &turnID); err != nil {
		return domain.Message{}, storeErr("scanning message", err)
	}
	msg := domain.Message{
		Role:    domain.Role(role),
		Content: content,
		TurnID:  turnID,
	}
	switch msg.Role {
	case domain.RoleAssistant:
		msg = buildAssistantMessage(msg, tool.input)
	case domain.RoleTool:
		msg = buildToolMessage(msg, tool)
	}
	return msg, nil
}

// buildAssistantMessage attaches unmarshalled tool calls to an assistant message.
func buildAssistantMessage(msg domain.Message, toolInput string) domain.Message {
	if toolInput == "" {
		return msg
	}
	var calls []domain.ToolCall
	if err := json.Unmarshal([]byte(toolInput), &calls); err == nil {
		msg.ToolCalls = calls
	}
	return msg
}

// buildToolMessage attaches tool call and tool result to a tool-role message.
func buildToolMessage(msg domain.Message, tool toolRoleFields) domain.Message {
	if tool.name == "" && tool.input == "" && tool.output == "" && tool.callID == "" {
		return msg
	}
	var input map[string]any
	if tool.input != "" {
		_ = json.Unmarshal([]byte(tool.input), &input)
	}
	msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
		ID:    tool.callID,
		Name:  tool.name,
		Input: input,
	})
	msg.ToolResults = append(msg.ToolResults, domain.ToolResult{
		ToolCallID:    tool.callID,
		Content:       tool.output,
		ClientContent: tool.clientOutput,
	})
	return msg
}

// ClearMessages removes all messages from the given session (in DB).
func (r *SqliteMessageRepository) ClearMessages(ctx context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.conn.ExecContext(ctx, "DELETE FROM messages WHERE session_id = ?", sessionID); err != nil {
		return storeErr("clearing messages", err)
	}
	if _, err := r.conn.ExecContext(ctx, "DELETE FROM history_turns WHERE session_id = ?", sessionID); err != nil {
		return storeErr("clearing history turns", err)
	}
	return nil
}

// ListSessions returns all sessions for the given user, ordered by creation date (newest first).
func (b *SqliteSessionRepository) ListSessions(ctx context.Context, userID string) ([]domain.SessionSummary, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	rows, err := b.conn.QueryContext(ctx, `
		SELECT s.id, s.title, s.created_at,
		       COUNT(CASE WHEN m.role = 'user' THEN 1 END) AS turn_count
		FROM sessions s
		LEFT JOIN messages m ON m.session_id = s.id
		WHERE s.user_id = ?
		GROUP BY s.id
		ORDER BY s.created_at DESC
	`, userID)
	if err != nil {
		return nil, storeErr("listing sessions", err)
	}
	defer func() { _ = rows.Close() }()

	sessions := []domain.SessionSummary{}
	for rows.Next() {
		var ss domain.SessionSummary
		var createdAt string
		if err := rows.Scan(&ss.ID, &ss.Title, &createdAt, &ss.TurnCount); err != nil {
			return nil, storeErr("scanning session", err)
		}
		parsed, err := time.Parse(timeFormat, createdAt)
		if err != nil {
			slog.Warn("parsing session created_at", "session_id", ss.ID, "value", createdAt, "error", err)
		}
		ss.CreatedAt = parsed
		sessions = append(sessions, ss)
	}
	if err := rows.Err(); err != nil {
		return nil, storeErr("iterating sessions", err)
	}
	return sessions, nil
}

// LoadHistoryTurnsFromSession returns the conversation history for the given session as question/answer pairs.
func (b *SqliteSessionRepository) LoadHistoryTurnsFromSession(ctx context.Context, sessionID string) ([]domain.HistoryTurn, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	rows, err := b.conn.QueryContext(ctx, `
		SELECT turn_id, question, answer, question_at, answer_at, status, interrupt_id, interrupt_reason, interrupt_state
		FROM history_turns
		WHERE session_id = ?
		ORDER BY COALESCE(question_at, answer_at), turn_id
	`, sessionID)
	if err != nil {
		return nil, storeErr("loading history turns", err)
	}
	defer func() { _ = rows.Close() }()

	var turns []domain.HistoryTurn
	for rows.Next() {
		var turn domain.HistoryTurn
		var questionAt sql.NullString
		var answerAt sql.NullString
		var status string
		if err := rows.Scan(&turn.TurnID, &turn.Question, &turn.Answer, &questionAt, &answerAt, &status, &turn.InterruptID, &turn.InterruptReason, &turn.InterruptState); err != nil {
			return nil, storeErr("scanning history turn", err)
		}
		turn.Status = status
		if questionAt.Valid {
			parsed, err := time.Parse(timeFormat, questionAt.String)
			if err != nil {
				slog.Warn("parsing question_at", "session_id", sessionID, "turn_id", turn.TurnID, "error", err)
			}
			turn.At = parsed
		} else if answerAt.Valid {
			parsed, err := time.Parse(timeFormat, answerAt.String)
			if err != nil {
				slog.Warn("parsing answer_at", "session_id", sessionID, "turn_id", turn.TurnID, "error", err)
			}
			turn.At = parsed
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, storeErr("iterating history turns", err)
	}
	return turns, nil
}

// DeleteSession removes a session and all its messages from the database.
func (b *SqliteSessionRepository) DeleteSession(ctx context.Context, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tx, err := b.conn.BeginTx(ctx, nil)
	if err != nil {
		return storeErr("begin tx", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM messages WHERE session_id = ?", sessionID); err != nil {
		_ = tx.Rollback()
		return storeErr("deleting messages", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM history_turns WHERE session_id = ?", sessionID); err != nil {
		_ = tx.Rollback()
		return storeErr("deleting history turns", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", sessionID); err != nil {
		_ = tx.Rollback()
		return storeErr("deleting session", err)
	}
	if err := tx.Commit(); err != nil {
		return storeErr("commit tx", err)
	}
	return nil
}
