package domain

import (
	"context"
	"time"
)

// SessionSummary holds brief metadata about a past conversation session.
type SessionSummary struct {
	// Unique session identifier
	ID string
	// Title of the session (typically the first user question)
	Title string
	// Timestamp of session creation (or last update)
	CreatedAt time.Time
	// Number of turns in the session, may be zero if unavailable
	TurnCount int
}

// HistoryTurn represents a single question/answer pair from a past conversation.
type HistoryTurn struct {
	// The user's question in this turn.
	Question string
	// The assistant's answer in this turn.
	Answer string
	// Timestamp of when this turn occurred (optional, may be zero if unavailable).
	At time.Time
	// The model used for this turn (optional, may be empty if unavailable).
	Model string
	// The number of times this turn has been called (optional, may be zero if unavailable).
	CallCount int
	// Unique identifier for this turn, used to reconcile in-memory and remote sources.
	TurnID string
	// Status indicates whether this turn completed normally or was interrupted.
	Status string
	// InterruptID is the unique identifier of the emitted interrupt (empty if not interrupted).
	InterruptID string
	// InterruptReason identifies why the interrupt was emitted (e.g. "talk:max_iterations").
	InterruptReason string
	// InterruptState tracks lifecycle: "open", "resolved", or "cancelled".
	InterruptState string
}

// SessionScope identifies the current conversation context.
// It is an immutable value type; switching sessions means creating a new SessionScope.
type SessionScope struct {
	sessionID string
	userID    string
}

// NewSessionScope creates a SessionScope.
func NewSessionScope(sessionID, userID string) SessionScope {
	return SessionScope{sessionID: sessionID, userID: userID}
}

// SessionID returns the session identifier.
func (s SessionScope) SessionID() string { return s.sessionID }

// UserID returns the user identifier.
func (s SessionScope) UserID() string { return s.userID }

// MessageRepository persists conversation messages.
// Implementations are fully stateless — all identity context is passed via parameters.
type MessageRepository interface {
	// AllMessages returns all messages for the given session.
	AllMessages(ctx context.Context, sessionID string) ([]Message, error)
	// ClearMessages removes all messages for the given session.
	ClearMessages(ctx context.Context, sessionID string) error
}

// SessionRepository provides access to historical sessions stored in an external system.
type SessionRepository interface {
	ListSessions(ctx context.Context, userID string) ([]SessionSummary, error)
	LoadHistoryTurnsFromSession(ctx context.Context, sessionID string) ([]HistoryTurn, error)
	DeleteSession(ctx context.Context, sessionID string) error
}
