package domain

import "context"

// NoOpEventHandler silently discards all events.
type NoOpEventHandler struct{}

var _ EventHandler = NoOpEventHandler{}

func (NoOpEventHandler) HandleMessageEvent(context.Context, MessageEvent) error { return nil }
func (NoOpEventHandler) HandleTurnEndEvent(context.Context, TurnEndEvent) error { return nil }
func (NoOpEventHandler) HandleToolStartEvent(context.Context, ToolStartEvent) error {
	return nil
}
func (NoOpEventHandler) HandleToolEndEvent(context.Context, ToolEndEvent) error {
	return nil
}
