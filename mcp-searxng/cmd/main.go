package main

import (
	"os"

	"github.com/pixime-net/mcp-searxng/internal/config"
	"github.com/pixime-net/mcp-searxng/internal/tools"
	"github.com/pixime-net/talk-libs/logger"
	"github.com/pixime-net/talk-libs/mcpserver"
	"github.com/pixime-net/talk-libs/version"
)

func main() {
	log := logger.GetLogger()

	env, err := config.LoadServerEnv(".env")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	app := buildApp(env)
	app.Run()
}

// buildApp creates the MCP server application from the given configuration.
func buildApp(env *config.ServerEnv) *mcpserver.App {
	scrape := tools.NewScrapeTool(env.ScrapeMaxChars)
	if env.ScrapeRenderURL != "" {
		scrape = scrape.WithRenderer(
			tools.NewCDPRenderer(env.ScrapeRenderURL, env.ScrapeRenderToken, env.ScrapeRenderTimeout),
			env.ScrapeRenderMode,
			env.ScrapeRenderConcurrency,
		)
	}

	opts := []mcpserver.Option{
		mcpserver.WithTools(
			mcpserver.RegisterTool(tools.NewSearchTool(env.SearxngURL, env.SearchMaxResults)),
			mcpserver.RegisterTool(scrape),
		),
		mcpserver.WithBaseEnvHTTPSecurity(env.BaseEnv),
	}
	opts = append(opts, mcpserver.WithBaseEnvAuth(env.BaseEnv)...)
	return mcpserver.NewApp("searxng-mcp", version.Version, opts...)
}
