package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/pixime-net/talk-libs/mcpserver"
)

const defaultRateLimitPerMinute = 60

// DefaultSearchMaxResults is used when SEARCH_MAX_RESULTS is not set.
const DefaultSearchMaxResults = 10

// DefaultScrapeMaxChars is used when SCRAPE_MAX_CHARS is not set.
const DefaultScrapeMaxChars = 8_000

// Defaults for the optional headless renderer used by the scrape tool.
const (
	DefaultScrapeRenderTimeout     = 20 * time.Second
	DefaultScrapeRenderConcurrency = 1
)

// RenderMode controls when the scrape tool renders pages with a headless
// browser instead of using the plain HTTP fetch.
type RenderMode string

const (
	// RenderOff never renders: the scrape tool behaves as before (default).
	RenderOff RenderMode = "off"
	// RenderFallback renders only when the HTTP fetch returns a JS-only shell.
	RenderFallback RenderMode = "fallback"
	// RenderAlways renders every page, skipping the HTTP fetch.
	RenderAlways RenderMode = "always"
)

// ServerEnv holds the environment configuration specific to this MCP server.
type ServerEnv struct {
	mcpserver.BaseEnv
	SearxngURL       string // SEARXNG_URL — URL of the SearXNG instance
	SearchMaxResults int    // SEARCH_MAX_RESULTS — number of results returned by the search tool
	ScrapeMaxChars   int    // SCRAPE_MAX_CHARS — default and upper bound of characters returned by the scrape tool

	// Headless renderer for JavaScript-heavy pages (all optional).
	ScrapeRenderURL         string        // SCRAPE_RENDER_URL — CDP WebSocket endpoint, empty to disable rendering
	ScrapeRenderToken       string        // BROWSERLESS_TOKEN — token appended to the CDP URL
	ScrapeRenderMode        RenderMode    // SCRAPE_RENDER_MODE — off | fallback | always
	ScrapeRenderTimeout     time.Duration // SCRAPE_RENDER_TIMEOUT — per-render timeout
	ScrapeRenderConcurrency int           // SCRAPE_RENDER_CONCURRENCY — maximum simultaneous renders
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

	scrapeRenderURL, err := renderURLEnv("SCRAPE_RENDER_URL")
	if err != nil {
		return nil, err
	}
	scrapeRenderToken := strings.TrimSpace(os.Getenv("BROWSERLESS_TOKEN"))
	// A configured renderer defaults to fallback mode when the mode is unset.
	defaultRenderMode := RenderOff
	if scrapeRenderURL != "" {
		defaultRenderMode = RenderFallback
	}
	scrapeRenderMode, err := renderModeEnv("SCRAPE_RENDER_MODE", defaultRenderMode)
	if err != nil {
		return nil, err
	}
	if scrapeRenderMode != RenderOff && scrapeRenderURL == "" {
		return nil, fmt.Errorf("SCRAPE_RENDER_MODE is %q but SCRAPE_RENDER_URL is empty", scrapeRenderMode)
	}
	scrapeRenderTimeout, err := durationEnv("SCRAPE_RENDER_TIMEOUT", DefaultScrapeRenderTimeout)
	if err != nil {
		return nil, err
	}
	scrapeRenderConcurrency, err := positiveIntEnv("SCRAPE_RENDER_CONCURRENCY", DefaultScrapeRenderConcurrency)
	if err != nil {
		return nil, err
	}

	env := &ServerEnv{
		BaseEnv:                 mcpserver.LoadBaseEnv(),
		SearxngURL:              SearxngURL,
		SearchMaxResults:        searchMaxResults,
		ScrapeMaxChars:          scrapeMaxChars,
		ScrapeRenderURL:         scrapeRenderURL,
		ScrapeRenderToken:       scrapeRenderToken,
		ScrapeRenderMode:        scrapeRenderMode,
		ScrapeRenderTimeout:     scrapeRenderTimeout,
		ScrapeRenderConcurrency: scrapeRenderConcurrency,
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

// renderModeEnv reads a render mode, falling back to def when the variable is
// unset. Accepted values are off, fallback and always (case-insensitive).
func renderModeEnv(name string, def RenderMode) (RenderMode, error) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if v == "" {
		return def, nil
	}
	switch mode := RenderMode(v); mode {
	case RenderOff, RenderFallback, RenderAlways:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid %s %q: must be off, fallback or always", name, v)
	}
}

// renderURLEnv reads the renderer CDP endpoint, which must be a ws:// or
// wss:// URL when set. An empty value disables rendering.
func renderURLEnv(name string) (string, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", fmt.Errorf("invalid %s %q: %w", name, v, err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("invalid %s %q: scheme must be ws or wss", name, v)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("invalid %s %q: missing host", name, v)
	}
	return v, nil
}

// durationEnv reads a strictly positive duration (e.g. "20s"), falling back to
// def when the variable is unset.
func durationEnv(name string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be a positive duration (e.g. 20s)", name, v)
	}
	return d, nil
}
