package domain

import (
	"context"
	"strings"
	"testing"
)

func TestSessionScopeUserID(t *testing.T) {
	scope := NewSessionScope("session-1", "user-1")
	if scope.UserID() != "user-1" {
		t.Fatalf("UserID() = %q, want %q", scope.UserID(), "user-1")
	}
}

func TestGenerateSessionIDFormat(t *testing.T) {
	id := GenerateSessionID()
	if len(id) != 36 {
		t.Fatalf("GenerateSessionID length = %d, want 36", len(id))
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Fatalf("GenerateSessionID %q missing uuid separators", id)
	}
	// UUIDv4 version nibble.
	if id[14] != '4' {
		t.Fatalf("GenerateSessionID version nibble = %q, want %q", id[14], '4')
	}
	// RFC4122 variant nibble in {8,9,a,b}.
	if !strings.ContainsRune("89ab", rune(id[19])) {
		t.Fatalf("GenerateSessionID variant nibble = %q, want one of 8,9,a,b", id[19])
	}
}

func TestNoOpMessageEventHandlerMethods(t *testing.T) {
	h := NoOpMessageEventHandler{}
	ctx := context.Background()

	if err := h.HandleMessageEvent(ctx, MessageEvent{}); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}
	if err := h.HandleTurnEvent(ctx, TurnEvent{}); err != nil {
		t.Fatalf("HandleTurnEvent error: %v", err)
	}
	if err := h.HandleToolCallStart(ctx, ToolCallEvent{}); err != nil {
		t.Fatalf("HandleToolCallStart error: %v", err)
	}
	if err := h.HandleToolCallEnd(ctx, ToolCallEndEvent{}); err != nil {
		t.Fatalf("HandleToolCallEnd error: %v", err)
	}
}
