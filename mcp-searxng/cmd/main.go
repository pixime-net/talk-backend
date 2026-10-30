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
	opts := []mcpserver.Option{
		mcpserver.WithTools(
			mcpserver.RegisterTool(tools.NewSearchTool(env.SearxngURL, env.SearchMaxResults)),
			mcpserver.RegisterTool(tools.NewScrapeTool(env.ScrapeMaxChars)),
		),
		mcpserver.WithBaseEnvHTTPSecurity(env.BaseEnv),
	}
	opts = append(opts, mcpserver.WithBaseEnvAuth(env.BaseEnv)...)
	return mcpserver.NewApp("searxng-mcp", version.Version, opts...)
}
