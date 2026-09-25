package domain

import (
	"context"
	"testing"
)

func TestNoOpEventHandlerMethods(t *testing.T) {
	h := NoOpEventHandler{}
	ctx := context.Background()

	if err := h.HandleMessageEvent(ctx, MessageEvent{}); err != nil {
		t.Fatalf("HandleMessageEvent error: %v", err)
	}
	if err := h.HandleTurnEndEvent(ctx, TurnEndEvent{}); err != nil {
		t.Fatalf("HandleTurnEndEvent error: %v", err)
	}
	if err := h.HandleToolStartEvent(ctx, ToolStartEvent{}); err != nil {
		t.Fatalf("HandleToolStartEvent error: %v", err)
	}
	if err := h.HandleToolEndEvent(ctx, ToolEndEvent{}); err != nil {
		t.Fatalf("HandleToolEndEvent error: %v", err)
	}
}
