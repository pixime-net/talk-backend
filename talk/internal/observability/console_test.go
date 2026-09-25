package observability

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/pixime-net/talk/internal/domain"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	_ = r.Close()

	return buf.String()
}

func TestConsoleUsageReporter_HandleMessageEvent_AssistantInitialWithThinking(t *testing.T) {
	reporter := ConsoleUsageReporter{}
	event := domain.MessageEvent{
		Kind: domain.CallKindInitial,
		Message: domain.Message{
			Role:     domain.RoleAssistant,
			Thinking: "internal reasoning",
		},
		Model: domain.Model{Name: "sonnet-4.6"},
		Usage: domain.Usage{
			InputTokens:      11,
			OutputTokens:     7,
			CacheReadTokens:  3,
			CacheWriteTokens: 2,
			ReasoningTokens:  5,
		},
	}

	output := captureStdout(t, func() {
		err := reporter.HandleMessageEvent(context.Background(), event)
		if err != nil {
			t.Fatalf("HandleMessageEvent unexpected error: %v", err)
		}
	})

	if !strings.Contains(output, "Thinking:") {
		t.Fatalf("output missing Thinking section: %q", output)
	}
	if !strings.Contains(output, "[api call]") {
		t.Fatalf("output missing api call marker: %q", output)
	}
	if !strings.Contains(output, "model=sonnet-4.6") {
		t.Fatalf("output missing model name: %q", output)
	}
	if !strings.Contains(output, "kind=initial") {
		t.Fatalf("output missing call kind: %q", output)
	}
	if !strings.Contains(output, "reasoning=") {
		t.Fatalf("output missing reasoning usage: %q", output)
	}
}

func TestConsoleUsageReporter_HandleMessageEvent_NonAssistant(t *testing.T) {
	reporter := ConsoleUsageReporter{}
	event := domain.MessageEvent{
		Kind:    domain.CallKindInitial,
		Message: domain.Message{Role: domain.RoleUser},
	}

	output := captureStdout(t, func() {
		err := reporter.HandleMessageEvent(context.Background(), event)
		if err != nil {
			t.Fatalf("HandleMessageEvent unexpected error: %v", err)
		}
	})

	if output != "" {
		t.Fatalf("expected no output for non-assistant, got %q", output)
	}
}

func TestConsoleUsageReporter_HandleMessageEvent_UnknownKind(t *testing.T) {
	reporter := ConsoleUsageReporter{}
	event := domain.MessageEvent{
		Kind:    domain.CallKind("unknown"),
		Message: domain.Message{Role: domain.RoleAssistant},
	}

	output := captureStdout(t, func() {
		err := reporter.HandleMessageEvent(context.Background(), event)
		if err != nil {
			t.Fatalf("HandleMessageEvent unexpected error: %v", err)
		}
	})

	if output != "" {
		t.Fatalf("expected no output for unknown kind, got %q", output)
	}
}

func TestConsoleUsageReporter_HandleToolCallStart(t *testing.T) {
	reporter := ConsoleUsageReporter{}
	event := domain.ToolStartEvent{
		ToolCall: domain.ToolCall{
			Name:  "get_weather",
			Input: map[string]any{"city": "Paris"},
		},
	}

	output := captureStdout(t, func() {
		err := reporter.HandleToolStartEvent(context.Background(), event)
		if err != nil {
			t.Fatalf("HandleToolCallStart unexpected error: %v", err)
		}
	})

	if !strings.Contains(output, "[tool call]") {
		t.Fatalf("output missing tool call marker: %q", output)
	}
	if !strings.Contains(output, "get_weather") {
		t.Fatalf("output missing tool name: %q", output)
	}
	if !strings.Contains(output, "Paris") {
		t.Fatalf("output missing tool args: %q", output)
	}
}

func TestConsoleUsageReporter_HandleTurnEvent(t *testing.T) {
	reporter := ConsoleUsageReporter{}
	event := domain.TurnEndEvent{
		Model:     domain.Model{Name: "sonnet-4.6"},
		CallCount: 2,
		TotalUsage: domain.Usage{
			InputTokens:      21,
			OutputTokens:     13,
			CacheReadTokens:  8,
			CacheWriteTokens: 1,
			ReasoningTokens:  9,
		},
	}

	output := captureStdout(t, func() {
		err := reporter.HandleTurnEndEvent(context.Background(), event)
		if err != nil {
			t.Fatalf("HandleTurnEvent unexpected error: %v", err)
		}
	})

	if !strings.Contains(output, "[turn]") {
		t.Fatalf("output missing turn marker: %q", output)
	}
	if !strings.Contains(output, "model=sonnet-4.6") {
		t.Fatalf("output missing model name: %q", output)
	}
	if !strings.Contains(output, "calls=2") {
		t.Fatalf("output missing call count: %q", output)
	}
	if !strings.Contains(output, "reasoning=") {
		t.Fatalf("output missing reasoning tokens: %q", output)
	}
}

func TestConsoleUsageReporter_HandleToolCallEnd_NoOp(t *testing.T) {
	reporter := ConsoleUsageReporter{}
	if err := reporter.HandleToolEndEvent(context.Background(), domain.ToolEndEvent{}); err != nil {
		t.Fatalf("HandleToolCallEnd unexpected error: %v", err)
	}
}

func TestFaintWrapsWithANSI(t *testing.T) {
	got := faint("hello")
	if got != "\033[2mhello\033[0m" {
		t.Fatalf("faint output = %q, want %q", got, "\\033[2mhello\\033[0m")
	}
}
