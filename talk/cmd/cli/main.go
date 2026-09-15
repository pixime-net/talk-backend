package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pixime-net/talk-libs/version"
	"github.com/pixime-net/talk/internal/config"
	"github.com/pixime-net/talk/internal/domain"
	"github.com/pixime-net/talk/internal/llm/router"
	"github.com/pixime-net/talk/internal/mcp"
	sqlitestore "github.com/pixime-net/talk/internal/memory/sqlite"
	"github.com/pixime-net/talk/internal/observability"
	"github.com/pixime-net/talk/internal/prompt"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		modelFlag      string
		systemFileFlag string
		pprofFlag      bool
	)

	cmd := &cobra.Command{
		Use:   "talk-cli",
		Short: "Interactive LLM conversation session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), modelFlag, systemFileFlag, pprofFlag)
		},
	}

	cmd.Flags().StringVar(&modelFlag, "model", "", "Model alias to use (e.g. sonnet-4.6, devstral)")
	cmd.Flags().StringVar(&systemFileFlag, "system-file", defaultSystemPromptPath(), "Path to a Markdown system prompt file")
	registerPprofFlag(cmd, &pprofFlag)

	_ = cmd.MarkFlagRequired("model")

	cmd.AddCommand(newServeCmd())

	return cmd
}

func run(ctx context.Context, modelAlias, systemFile string, pprofEnabled bool) error {
	startPprofIfEnabled(pprofEnabled)

	cfg, err := config.Load(".env")
	if err != nil {
		return err
	}

	r := router.NewLLMRouter(cfg)
	client, err := r.Get(modelAlias)
	if err != nil {
		return err
	}

	modelDescriptor, err := domain.Lookup(modelAlias)
	if err != nil {
		return err
	}

	pp := buildPromptProvider(systemFile)

	sessionID := domain.GenerateSessionID()
	const userID = "anonymous"
	scope := domain.NewSessionScope(sessionID, userID)

	dbPath := storeDBPath()
	messages, browser, err := sqlitestore.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening session store: %w", err)
	}
	defer func() { _ = messages.Close() }()

	// MCP server registry and manager.
	mcpRegistry, err := mcp.NewSQLiteRegistry(messages.DB())
	if err != nil {
		return fmt.Errorf("initializing mcp registry: %w", err)
	}
	mcpManager := mcp.NewManager(mcpRegistry)
	mcpManager.ConnectAll(ctx)
	defer mcpManager.Close()

	handlers := domain.NewMessageEventHandlers([][]domain.MessageEventHandler{
		{messages},
		buildReporters(cfg),
	})

	manager := domain.NewConversationManager(domain.ConversationManagerConfig{
		Client:             client,
		ModelID:            modelAlias,
		Scope:              scope,
		Provider:           modelDescriptor.OTLPProvider,
		Store:              messages,
		SessionBrowser:     browser,
		PromptProvider:     pp,
		Tools:              mcpManager.Tools,
		EventHandlers:      handlers,
		MaxConcurrentTools: cfg.ToolsMaxConcurrent,
		ContextFullTurns:   cfg.ContextFullTurns,
	})

	lr, err := NewGoPromptReader(historyFilePath())
	if err != nil {
		return fmt.Errorf("initializing prompt reader: %w", err)
	}

	app := &App{
		Printer:      stdPrinter{},
		Router:       r,
		Manager:      manager,
		Scope:        scope,
		Messages:     messages,
		Sessions:     browser,
		PP:           pp,
		MCPManager:   mcpManager,
		MCPRegistry:  mcpRegistry,
		CurrentModel: modelAlias,
		LR:           lr,
	}

	return app.runSession(ctx)
}

func buildReporters(cfg *config.Config) []domain.MessageEventHandler {
	var reporters []domain.MessageEventHandler
	if cfg.ConsoleUsageReporter {
		reporters = append(reporters, &observability.ConsoleUsageReporter{})
	}
	if cfg.LangfuseSecretKey != "" && cfg.LangfusePublicKey != "" {
		reporters = append(reporters, observability.NewLangfuseUsageReporter(observability.LangfuseConfig{
			PublicKey: cfg.LangfusePublicKey,
			SecretKey: cfg.LangfuseSecretKey,
			BaseURL:   cfg.LangfuseBaseURL,
		}))
	}
	if len(reporters) == 0 {
		reporters = append(reporters, &observability.ConsoleUsageReporter{})
	}
	return reporters
}

func (a *App) runSession(ctx context.Context) error {
	a.Printf("%s%s\n", cyan(bold+"Session started."+reset), faint(" "+version.Version))
	a.cmdHelp()
	for {
		a.Println()
		input, err := a.LR.ReadLine(green(bold+"You"+reset+":") + " ")
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			a.handleSlashCommand(ctx, input)
			continue
		}

		answer, err := a.Manager.Chat(ctx, input)
		if err != nil {
			a.Printf("\n%s %s\n", red("Error:"), err.Error())
			continue
		}

		a.Printf("\n%s %s\n", cyan(bold+"Assistant"+reset+":"), answer)
	}

	a.Println("\n" + faint("Session ended."))
	return nil
}

func buildPromptProvider(systemFile string) domain.PromptProvider {
	return prompt.NewFileProvider(systemFile)
}

func defaultSystemPromptPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "system_prompt.md"
	}
	return filepath.Join(filepath.Dir(exe), "system_prompt.md")
}

func historyFilePath() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".talk")
		_ = os.MkdirAll(dir, 0o700)
		return filepath.Join(dir, "history")
	}
	return "history"
}

func storeDBPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".talk")
		_ = os.MkdirAll(dir, 0o700)
		return filepath.Join(dir, "talk.db")
	}
	return "talk.db"
}
