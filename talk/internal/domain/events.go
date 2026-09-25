package domain

import "time"

/*
This file defines the various events that occur during the lifecycle of a conversation, including message events, turn events, and tool call events.
Each event type captures specific details relevant to that stage of the conversation lifecycle.
It provides a structured way to track and respond to different stages of the conversation lifecycle.
*/

// CallKind classifies the type of LLM call within a conversation turn.
type CallKind string

const (
	CallKindInitial    CallKind = "initial"
	CallKindToolResult CallKind = "tool_result"
)

// MessageEvent is emitted for each message produced during a turn.
type MessageEvent struct {
	Message
	SessionScope SessionScope
	Model        Model
	TurnSpanID   string
	Kind         CallKind
	Usage        Usage
	StartedAt    time.Time
	EndedAt      time.Time
	Input        string
	Output       string
}

// TurnEndEvent is emitted once at the end of a full Chat() turn.
type TurnEndEvent struct {
	TurnID          string
	TurnSpanID      string
	StartedAt       time.Time
	EndedAt         time.Time
	SessionScope    SessionScope
	Model           Model
	TotalUsage      Usage
	CallCount       int
	Input           string
	Output          string
	ToolCalls       []ToolCall
	Status          string
	InterruptID     string
	InterruptReason string
	InterruptState  string
}

// ToolStartEvent is emitted when a tool call is about to be executed.
type ToolStartEvent struct {
	TurnID    string
	ToolCall  ToolCall
	StartedAt time.Time
}

// ToolEndEvent is emitted when a tool call has completed execution.
type ToolEndEvent struct {
	TurnID    string
	ToolCall  ToolCall
	Result    ToolResult
	StartedAt time.Time
	EndedAt   time.Time
}
