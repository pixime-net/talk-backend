package main

import (
	"github.com/pixime-net/talk/internal/domain"
	"github.com/pixime-net/talk/internal/mcp"
)

// ModelSwitcher builds LLM clients by model alias.
type ModelSwitcher interface {
	Get(model string) (domain.LlmClient, error)
}

// App holds the shared session state for the CLI application.
type App struct {
	Printer
	Router         ModelSwitcher
	Manager        *domain.ConversationManager
	Scope          domain.SessionScope
	Messages       domain.MessageRepository
	Sessions       domain.SessionRepository
	PromptProvider domain.PromptProvider
	MCPManager     *mcp.Manager
	MCPRegistry    mcp.Registry
	CurrentModel   string
	Reader         Reader
}
