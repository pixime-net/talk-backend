package main

import (
	"context"
	"os"
	"strings"
)

func (a *App) handleSlashCommand(ctx context.Context, input string) {
	cmd := strings.Fields(input)[0]
	switch cmd {
	case "/model":
		a.cmdModel()
	case "/memory":
		a.cmdMemory(ctx)
	case "/session":
		args := strings.TrimSpace(strings.TrimPrefix(input, "/session"))
		a.cmdSession(ctx, args)
	case "/prompt":
		a.cmdPrompt(ctx)
	case "/mcp":
		args := strings.TrimSpace(strings.TrimPrefix(input, "/mcp"))
		a.cmdMCP(ctx, args)
	case "/thinking":
		a.cmdThinking()
	case "/help":
		a.cmdHelp()
	case "/q":
		a.cmdQuit()
	default:
		a.Printf("Unknown command %s. Type %s for available commands.\n",
			red(cmd), yellow("/help"))
	}
}

func (a *App) cmdHelp() {
	a.Println(emphasize("Commands:"))
	a.Println(faint("  /help                                — show this help"))
	a.Println(faint("  /model                               — switch models"))
	a.Println(faint("  /thinking                            — set reasoning level"))
	a.Println(faint("  /memory                              — show current session history"))
	a.Println(faint("  /session [list|new|remove]           — manage sessions"))
	a.Println(faint("  /prompt                              — show system prompt"))
	a.Println(faint("  /mcp [list|add|remove|refresh]       — manage MCP servers"))
	a.Println(faint("  /q                                   — quit"))
}

func (a *App) cmdQuit() {
	a.Println(faint("Exiting session."))
	os.Exit(0)
}

func (a *App) cmdPrompt(ctx context.Context) {
	text, err := a.PromptProvider.SystemPrompt(ctx)
	if err != nil {
		a.Errorf("%s%s\n", red("Error loading prompt: "), err.Error())
		return
	}
	if text == "" {
		a.Println(faint("(no system prompt)"))
		return
	}
	a.Printf("\n%s\n%s\n%s\n", faint("--- system prompt ---"), text, faint("--- end ---"))
}
