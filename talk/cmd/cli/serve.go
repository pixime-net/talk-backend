package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/pixime-net/talk-libs/logger"
	"github.com/pixime-net/talk/internal/agui"
	"github.com/pixime-net/talk/internal/config"
	"github.com/pixime-net/talk/internal/domain"
	"github.com/pixime-net/talk/internal/llm/router"
	"github.com/pixime-net/talk/internal/mcp"
	sqlitestore "github.com/pixime-net/talk/internal/memory/sqlite"
	"github.com/spf13/cobra"
)

const defaultServePort = "8090"

func newServeCmd() *cobra.Command {
	var portFlag string
	var systemFileFlag string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the AG-UI protocol HTTP server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			port := resolvePort(portFlag)
			systemFile := systemFileFlag
			if systemFile == "" {
				systemFile = defaultSystemPromptPath()
			}
			return runServe(cmd.Context(), port, systemFile)
		},
	}

	cmd.Flags().StringVar(&portFlag, "port", "", "HTTP server port (default: 8090, env: SERVE_PORT)")
	cmd.Flags().StringVar(&systemFileFlag, "system-file", "", "Path to a Markdown system prompt file")

	return cmd
}

func resolvePort(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("SERVE_PORT"); env != "" {
		return env
	}
	return defaultServePort
}

func runServe(ctx context.Context, port, systemFile string) error {
	log := logger.Logger
	if log == nil {
		log = slog.Default()
	}

	cfg, err := config.Load(".env")
	if err != nil {
		return err
	}

	// LLM router for per-request model resolution.
	llmRouter := router.NewLLMRouter(cfg)

	pp := buildPromptProvider(systemFile)

	// Open shared SQLite store.
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

	// ChatFunc resolves model per request from the alias passed by the handler.
	chatFn := buildChatFunc(log, cfg, llmRouter, pp, messages, browser, mcpManager)

	mux := http.NewServeMux()

	aguiHandler := agui.NewHandler(log, chatFn, domain.SupportedModels())
	mux.Handle("POST /agent", aguiHandler)
	mux.HandleFunc("GET /agent", methodNotAllowed)
	mux.HandleFunc("PUT /agent", methodNotAllowed)
	mux.HandleFunc("DELETE /agent", methodNotAllowed)

	handler := corsMiddleware(mux, cfg)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      0, // SSE requires no write timeout
	}

	// Graceful shutdown on SIGTERM/SIGINT.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		gracefulShutdown(ctx, log, srv)
	}()

	log.Info("starting AG-UI server",
		slog.String("port", port),
		slog.String("db", dbPath),
	)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	wg.Wait()
	log.Info("server stopped")
	return nil
}

// gracefulShutdown waits for context cancellation then shuts down srv with a 30-second deadline.
func gracefulShutdown(ctx context.Context, log *slog.Logger, srv *http.Server) {
	<-ctx.Done()
	log.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", slog.String("error", err.Error()))
	}
}

// extractUserInput returns the content of the last user message from the AG-UI message list.
func extractUserInput(messages []types.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleUser {
			if s, ok := messages[i].Content.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", messages[i].Content)
		}
	}
	return ""
}

func corsMiddleware(next http.Handler, cfg *config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", cfg.CORSAllowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", cfg.CORSAllowHeaders)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
}

// buildChatFunc constructs the ChatFunc that resolves the model, wires the
// conversation manager, and streams events back via the SSE writer.
// errors returned by the ChatFunc are sanitized for end users,
// while technical details are logged at ERROR level.
// ChatFunc and its relative errors are designed to be used in the AG-UI HTTP handler.
func buildChatFunc(
	log *slog.Logger,
	cfg *config.Config,
	llmRouter *router.Router,
	pp domain.PromptProvider,
	messages *sqlitestore.MessageRepository,
	browser *sqlitestore.Browser,
	mcpManager *mcp.Manager,
) agui.ChatFunc {
	return func(reqCtx context.Context, threadID string, modelAlias string, aguiMessages []types.Message, opts agui.ChatOptions) error {
		client, err := llmRouter.Get(modelAlias)
		if err != nil {
			log.Error("resolving model", slog.String("model", modelAlias), slog.String("error", err.Error()))
			return sanitizeError(err)
		}

		modelDescriptor, err := domain.Lookup(modelAlias)
		if err != nil {
			log.Error("looking up model", slog.String("model", modelAlias), slog.String("error", err.Error()))
			return sanitizeError(err)
		}

		aguiEmitter := agui.NewAGUIEmitter(opts.SSEWriter, log)
		handlers := domain.NewMessageEventHandlers([][]domain.MessageEventHandler{
			{aguiEmitter, messages},
		})

		scope := domain.NewSessionScope(threadID, "anonymous")
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

		userInput := extractUserInput(aguiMessages)
		if userInput == "" {
			return fmt.Errorf("no user message found in request")
		}

		if opts.ThinkingEffort != "" {
			manager.SetThinkingEffort(opts.ThinkingEffort)
		}

		_, chatErr := manager.Chat(reqCtx, userInput)
		if chatErr != nil {
			log.Error("chat error",
				slog.String("threadId", threadID),
				slog.String("model", modelAlias),
				slog.String("error", chatErr.Error()),
			)
			// Let ErrMaxToolIterations pass through so the handler can emit an interrupt.
			if errors.Is(chatErr, domain.ErrMaxToolIterations) {
				return chatErr
			}
			return sanitizeError(chatErr)
		}
		return nil
	}
}

// sanitizeError returns a message suitable for end users.
// The message is sent to the client via the AG-UI SSE stream.
func sanitizeError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("request timed out, please try again")
	case errors.Is(err, config.ErrMissingEnvVar):
		return fmt.Errorf("missing environment variable, please contact the administrator")
	case errors.Is(err, domain.ErrSystemPrompt):
		return fmt.Errorf("system prompt error, please contact the administrator")
	case errors.As(err, new(*sqlitestore.ErrStore)):
		return fmt.Errorf("store temporarily unavailable, please try again")
	case errors.Is(err, mcp.ErrSessionUnavailable):
		return fmt.Errorf("MCP tool execution is temporarily unavailable for an MCP server, please try again")
	default:
		return fmt.Errorf("an unexpected error occurred")
	}
}
