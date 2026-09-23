package domain

import "context"

// NoOpMessageEventHandler silently discards all events.
type NoOpMessageEventHandler struct{}

func (NoOpMessageEventHandler) HandleMessageEvent(context.Context, MessageEvent) error { return nil }
func (NoOpMessageEventHandler) HandleTurnEvent(context.Context, TurnEvent) error       { return nil }
func (NoOpMessageEventHandler) HandleToolCallStart(context.Context, ToolCallEvent) error {
	return nil
}
func (NoOpMessageEventHandler) HandleToolCallEnd(context.Context, ToolCallEndEvent) error {
	return nil
}
