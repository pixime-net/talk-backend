package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"

	"github.com/pixime-net/talk-libs/mcpserver"
)

const defaultRateLimitPerMinute = 60

// DefaultSearchMaxResults is used when SEARCH_MAX_RESULTS is not set.
const DefaultSearchMaxResults = 10

// DefaultScrapeMaxChars is used when SCRAPE_MAX_CHARS is not set.
const DefaultScrapeMaxChars = 8_000

// ServerEnv holds the environment configuration specific to this MCP server.
type ServerEnv struct {
	mcpserver.BaseEnv
	SearxngURL       string // SEARXNG_URL — URL of the SearXNG instance
	SearchMaxResults int    // SEARCH_MAX_RESULTS — number of results returned by the search tool
	ScrapeMaxChars   int    // SCRAPE_MAX_CHARS — default and upper bound of characters returned by the scrape tool
}

// LoadServerEnv reads .env files (if present) then loads the environment
// variables required by the SearXNG server.
// godotenv.Load never overrides already-set environment variables.
func LoadServerEnv(envFiles ...string) (*ServerEnv, error) {
	_ = godotenv.Load(envFiles...)

	SearxngURL := "http://localhost:8888"
	if v := os.Getenv("SEARXNG_URL"); v != "" {
		SearxngURL = v
	}

	searchMaxResults, err := positiveIntEnv("SEARCH_MAX_RESULTS", DefaultSearchMaxResults)
	if err != nil {
		return nil, err
	}

	scrapeMaxChars, err := positiveIntEnv("SCRAPE_MAX_CHARS", DefaultScrapeMaxChars)
	if err != nil {
		return nil, err
	}

	env := &ServerEnv{
		BaseEnv:          mcpserver.LoadBaseEnv(),
		SearxngURL:       SearxngURL,
		SearchMaxResults: searchMaxResults,
		ScrapeMaxChars:   scrapeMaxChars,
	}

	return env, nil
}

// positiveIntEnv reads a strictly positive integer from the named variable,
// falling back to def when it is unset.
func positiveIntEnv(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid %s %q: must be a positive integer", name, v)
	}
	return n, nil
}
