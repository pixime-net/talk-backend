package observability

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pixime-net/talk/internal/domain"
)

// ConsoleUsageReporter implements domain.MessageEventHandler by printing token usage
// to the terminal using ANSI color helpers.
// It is the default reporter for the CLI session when CONSOLE_USAGE_REPORTER=true.
// Can be combined with other reporters like LangfuseUsageReporter for dual logging.
type ConsoleUsageReporter struct{}

var _ domain.MessageEventHandler = (*ConsoleUsageReporter)(nil) // compile-time interface check

// HandleMessageEvent is called for every message event and prints usage for assistant LLM calls
// and tool invocation details for tool calls.
//
// Parameters:
// - messageEvent: The MessageEvent containing details about the call and its token usage.
func (ConsoleUsageReporter) HandleMessageEvent(_ context.Context, messageEvent domain.MessageEvent) error {
	switch messageEvent.Kind {
	case domain.CallKindInitial, domain.CallKindToolResult:
		if messageEvent.Role != domain.RoleAssistant {
			return nil
		}
		if messageEvent.Thinking != "" {
			fmt.Printf("\n%s %s\n", "\033[36mThinking:\033[0m", messageEvent.Thinking)
		}
		reasoningInfo := ""
		if messageEvent.Usage.ReasoningTokens > 0 {
			reasoningInfo = fmt.Sprintf(" reasoning=%5d", messageEvent.Usage.ReasoningTokens)
		}
		fmt.Printf(
			faint("  ↳ [api call] model=%-14s kind=%-12s in=%5d out=%5d cache_read=%5d cache_write=%5d%s\n"),
			messageEvent.Model.Name, string(messageEvent.Kind),
			messageEvent.Usage.InputTokens, messageEvent.Usage.OutputTokens,
			messageEvent.Usage.CacheReadTokens, messageEvent.Usage.CacheWriteTokens,
			reasoningInfo,
		)
	}

	return nil
}

// HandleToolCallStart is called right before tool execution starts.
func (ConsoleUsageReporter) HandleToolCallStart(_ context.Context, e domain.ToolCallEvent) error {
	inputJSON, _ := json.Marshal(e.ToolCall.Input)
	fmt.Printf(
		faint("  ↳   [tool call] tool=%s args=%s\n"),
		e.ToolCall.Name, string(inputJSON),
	)

	return nil
}

// HandleToolCallEnd is called after tool execution completes.
func (ConsoleUsageReporter) HandleToolCallEnd(_ context.Context, _ domain.ToolCallEndEvent) error {
	return nil
}

// HandleTurnEvent is called after every conversation turn and prints aggregated usage.
//
// Parameters:
// - e: The TurnEvent containing details about the conversation turn and its token usage.
func (ConsoleUsageReporter) HandleTurnEvent(_ context.Context, e domain.TurnEvent) error {
	reasoningInfo := ""
	if e.TotalUsage.ReasoningTokens > 0 {
		reasoningInfo = fmt.Sprintf(" reasoning=%5d", e.TotalUsage.ReasoningTokens)
	}
	statusInfo := ""
	if e.Status != "" && e.Status != domain.TurnStatusComplete {
		statusInfo = fmt.Sprintf(" status=%s", e.Status)
	}
	fmt.Printf(
		faint("  ↳ [turn]  model=%-14s calls=%d  total_in=%5d total_out=%5d cache_read=%5d cache_write=%5d%s%s\n"),
		e.Model.Name, e.CallCount,
		e.TotalUsage.InputTokens, e.TotalUsage.OutputTokens,
		e.TotalUsage.CacheReadTokens, e.TotalUsage.CacheWriteTokens,
		reasoningInfo, statusInfo,
	)

	return nil
}

// faint returns the input string with ANSI faint (dim) formatting.
// This is a helper function for consistent terminal output styling.
func faint(s string) string {
	return "\033[2m" + s + "\033[0m"
}
