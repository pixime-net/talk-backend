package domain

import (
	"context"
	"errors"
	"sync"
)

// MessageEventHandler receives all conversation lifecycle events.
type MessageEventHandler interface {
	HandleMessageEvent(ctx context.Context, event MessageEvent) error
	HandleTurnEvent(ctx context.Context, event TurnEvent) error
	HandleToolCallStart(ctx context.Context, event ToolCallEvent) error
	HandleToolCallEnd(ctx context.Context, event ToolCallEndEvent) error
}

// MessageEventHandlers executes handlers by sequential phases, with parallel execution inside each phase.
type MessageEventHandlers struct {
	handlers [][]MessageEventHandler
}

// NewMessageEventHandlers creates a phased event handler pipeline.
func NewMessageEventHandlers(handlers [][]MessageEventHandler) *MessageEventHandlers {
	return &MessageEventHandlers{handlers: handlers}
}

// HandleMessageEvent dispatches one message event through all phases.
func (h *MessageEventHandlers) HandleMessageEvent(ctx context.Context, event MessageEvent) error {
	return h.runHandlers(func(handler MessageEventHandler) error { return handler.HandleMessageEvent(ctx, event) })
}

// HandleTurnEvent dispatches one turn event through all phases.
func (h *MessageEventHandlers) HandleTurnEvent(ctx context.Context, event TurnEvent) error {
	return h.runHandlers(func(handler MessageEventHandler) error { return handler.HandleTurnEvent(ctx, event) })
}

// HandleToolCallStart dispatches one tool call start event through all phases.
func (h *MessageEventHandlers) HandleToolCallStart(ctx context.Context, event ToolCallEvent) error {
	return h.runHandlers(func(handler MessageEventHandler) error { return handler.HandleToolCallStart(ctx, event) })
}

// HandleToolCallEnd dispatches one tool call end event through all phases.
func (h *MessageEventHandlers) HandleToolCallEnd(ctx context.Context, event ToolCallEndEvent) error {
	return h.runHandlers(func(handler MessageEventHandler) error { return handler.HandleToolCallEnd(ctx, event) })
}

func (h *MessageEventHandlers) runHandlers(call func(MessageEventHandler) error) error {
	if h == nil || len(h.handlers) == 0 {
		return nil
	}
	for _, handlers := range h.handlers {
		if err := h.runHandler(handlers, call); err != nil {
			return err
		}
	}
	return nil
}

func (h *MessageEventHandlers) runHandler(handlers []MessageEventHandler, call func(MessageEventHandler) error) error {
	if len(handlers) == 0 {
		return nil
	}
	var (
		mu  sync.Mutex
		err error
		wg  sync.WaitGroup
	)
	for _, handler := range handlers {
		handler := handler
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if recover() != nil {
					mu.Lock()
					err = errors.Join(err, errors.New("message event handler panic"))
					mu.Unlock()
				}
			}()
			if callErr := call(handler); callErr != nil {
				mu.Lock()
				err = errors.Join(err, callErr)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return err
}
