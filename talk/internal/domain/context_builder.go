package domain

import "context"

// ContextBuilder builds the message context for an LLM call by reconciling
// in-memory messages with historical turns loaded from a SessionBrowser.
type ContextBuilder struct {
	messageStore   MessageRepository
	sessionBrowser SessionRepository
	sessionID      string
	contextFull    int // -1 full, 0 lean, N hybrid
}

// NewContextBuilder creates a ContextBuilder.
func NewContextBuilder(messageStore MessageRepository, sessionBrowser SessionRepository, sessionID string, contextFullTurns int) *ContextBuilder {
	return &ContextBuilder{
		messageStore:   messageStore,
		sessionBrowser: sessionBrowser,
		sessionID:      sessionID,
		contextFull:    contextFullTurns,
	}
}

// BuildContextMessages returns the messages that should be sent to the LLM for the given turn.
// When contextFull is negative or no session browser is configured, all in-memory messages are returned.
// Otherwise historical turns are loaded and merged with the detailed messages from the current session.
func (b *ContextBuilder) BuildContextMessages(ctx context.Context, currentTurnID string) []Message {
	allMessages, err := b.messageStore.AllMessages(ctx, b.sessionID)
	if err != nil {
		// Fail open to keep the conversation functional when store read fails.
		return nil
	}
	if b.contextFull < 0 || b.sessionBrowser == nil {
		return allMessages
	}

	historyTurns, err := b.sessionBrowser.LoadHistoryTurnsFromSession(ctx, b.sessionID)
	if err != nil {
		// Fail open to keep the conversation functional when history loading fails.
		return allMessages
	}

	selectedDetailedTurnIDs := selectDetailedTurnIDs(allMessages, historyTurns, currentTurnID, b.contextFull)
	messages := historyTurnsAsMessages(historyTurns, selectedDetailedTurnIDs, currentTurnID)
	availableDetailedTurnIDs := buildAvailableDetailedSet(allMessages, selectedDetailedTurnIDs)
	messages = appendFallbackSummaries(messages, historyTurns, selectedDetailedTurnIDs, availableDetailedTurnIDs, currentTurnID)
	return appendDetailedMessages(messages, allMessages, selectedDetailedTurnIDs)
}

// selectDetailedTurnIDs builds the set of turn IDs that should be included with
// full message detail: the current turn, the last contextFull user turns, and
// any turns still in an incomplete state.
func selectDetailedTurnIDs(allMessages []Message, historyTurns []HistoryTurn, currentTurnID string, contextFull int) map[string]struct{} {
	ids := make(map[string]struct{})
	ids[currentTurnID] = struct{}{}
	if contextFull > 0 {
		for _, turnID := range lastNTurnIDs(allMessages, currentTurnID, contextFull) {
			ids[turnID] = struct{}{}
		}
	}
	for _, turn := range historyTurns {
		if turn.Status == TurnStatusIncomplete {
			ids[turn.TurnID] = struct{}{}
		}
	}
	return ids
}

// buildAvailableDetailedSet returns the subset of selectedDetailedTurnIDs for
// which at least one detailed message exists in allMessages.
func buildAvailableDetailedSet(allMessages []Message, selectedDetailedTurnIDs map[string]struct{}) map[string]struct{} {
	available := make(map[string]struct{})
	for _, msg := range allMessages {
		if msg.TurnID == "" {
			continue
		}
		if _, ok := selectedDetailedTurnIDs[msg.TurnID]; ok {
			available[msg.TurnID] = struct{}{}
		}
	}
	return available
}

// appendFallbackSummaries emits summary messages for any turn that was selected
// for detail but has no detailed messages available in the store.
func appendFallbackSummaries(messages []Message, historyTurns []HistoryTurn, selectedDetailedTurnIDs, availableDetailedTurnIDs map[string]struct{}, currentTurnID string) []Message {
	for turnID := range selectedDetailedTurnIDs {
		if turnID == currentTurnID {
			continue
		}
		if _, ok := availableDetailedTurnIDs[turnID]; ok {
			continue
		}
		messages = appendFallbackSummary(messages, historyTurns, turnID)
	}
	return messages
}

// appendFallbackSummary finds the history turn matching turnID and appends its
// question/answer as plain user/assistant messages.
func appendFallbackSummary(messages []Message, historyTurns []HistoryTurn, turnID string) []Message {
	for _, turn := range historyTurns {
		if turn.TurnID != turnID {
			continue
		}
		if turn.Question != "" {
			messages = append(messages, Message{Role: RoleUser, Content: turn.Question, TurnID: turn.TurnID})
		}
		if turn.Answer != "" {
			messages = append(messages, Message{Role: RoleAssistant, Content: turn.Answer, TurnID: turn.TurnID})
		}
		break
	}
	return messages
}

// appendDetailedMessages appends all messages whose turn is in selectedDetailedTurnIDs,
// plus any message without a turn ID.
func appendDetailedMessages(messages []Message, allMessages []Message, selectedDetailedTurnIDs map[string]struct{}) []Message {
	for _, msg := range allMessages {
		if msg.TurnID == "" {
			messages = append(messages, msg)
			continue
		}
		if _, ok := selectedDetailedTurnIDs[msg.TurnID]; ok {
			messages = append(messages, msg)
		}
	}
	return messages
}

func historyTurnsAsMessages(turns []HistoryTurn, detailedTurnIDs map[string]struct{}, currentTurnID string) []Message {
	messages := make([]Message, 0, len(turns)*2)
	for _, turn := range turns {
		if turn.TurnID == currentTurnID {
			continue
		}
		if _, ok := detailedTurnIDs[turn.TurnID]; ok {
			continue
		}
		if turn.Question != "" {
			messages = append(messages, Message{Role: RoleUser, Content: turn.Question, TurnID: turn.TurnID})
		}
		if turn.Answer != "" {
			messages = append(messages, Message{Role: RoleAssistant, Content: turn.Answer, TurnID: turn.TurnID})
		}
	}
	return messages
}

func lastNTurnIDs(messages []Message, currentTurnID string, n int) []string {
	if n <= 0 {
		return nil
	}
	ordered := make([]string, 0)
	seen := make(map[string]struct{})
	for _, msg := range messages {
		if !isPreviousUserTurn(msg, currentTurnID) {
			continue
		}
		if _, ok := seen[msg.TurnID]; ok {
			continue
		}
		seen[msg.TurnID] = struct{}{}
		ordered = append(ordered, msg.TurnID)
	}
	if len(ordered) <= n {
		return ordered
	}
	return ordered[len(ordered)-n:]
}

// isPreviousUserTurn reports whether msg belongs to a user turn other than currentTurnID.
func isPreviousUserTurn(msg Message, currentTurnID string) bool {
	return msg.Role == RoleUser && msg.TurnID != "" && msg.TurnID != currentTurnID
}
