package domain

import (
	"context"
	"errors"
	"sync"
)

// EventHandler receives all conversation lifecycle events.
type EventHandler interface {
	HandleMessageEvent(ctx context.Context, event MessageEvent) error
	HandleTurnEndEvent(ctx context.Context, event TurnEndEvent) error
	HandleToolStartEvent(ctx context.Context, event ToolStartEvent) error
	HandleToolEndEvent(ctx context.Context, event ToolEndEvent) error
}

// EventHandlers executes handlers by sequential phases, with parallel execution inside each phase.
type EventHandlers struct {
	eventHandlers [][]EventHandler
}

var _ EventHandler = (*EventHandlers)(nil)

// NewEventHandlers creates a phased event handler pipeline.
func NewEventHandlers(eventHandlers [][]EventHandler) EventHandlers {
	return EventHandlers{eventHandlers: eventHandlers}
}

// HandleMessageEvent dispatches one message event through all phases.
func (h *EventHandlers) HandleMessageEvent(ctx context.Context, event MessageEvent) error {
	return h.runHandlersSequentially(func(eventHandlers EventHandler) error { return eventHandlers.HandleMessageEvent(ctx, event) })
}

// HandleTurnEndEvent dispatches one turn event through all phases.
func (h *EventHandlers) HandleTurnEndEvent(ctx context.Context, event TurnEndEvent) error {
	return h.runHandlersSequentially(func(eventHandlers EventHandler) error { return eventHandlers.HandleTurnEndEvent(ctx, event) })
}

// HandleToolStartEvent dispatches one tool call start event through all phases.
func (h *EventHandlers) HandleToolStartEvent(ctx context.Context, event ToolStartEvent) error {
	return h.runHandlersSequentially(func(eventHandlers EventHandler) error { return eventHandlers.HandleToolStartEvent(ctx, event) })
}

// HandleToolEndEvent dispatches one tool call end event through all phases.
func (h *EventHandlers) HandleToolEndEvent(ctx context.Context, event ToolEndEvent) error {
	return h.runHandlersSequentially(func(eventHandlers EventHandler) error { return eventHandlers.HandleToolEndEvent(ctx, event) })
}

func (h *EventHandlers) runHandlersSequentially(call func(EventHandler) error) error {
	if h == nil || len(h.eventHandlers) == 0 {
		return nil
	}
	for _, handlers := range h.eventHandlers {
		if err := h.runHandlersConcurrently(handlers, call); err != nil {
			return err
		}
	}
	return nil
}

func (h *EventHandlers) runHandlersConcurrently(handlers []EventHandler, call func(EventHandler) error) error {
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
