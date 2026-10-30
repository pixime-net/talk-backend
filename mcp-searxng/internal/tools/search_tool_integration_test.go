//go:build integration

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// defaultSearxngTestImage is pinned for reproducibility; override with SEARXNG_TEST_IMAGE.
const defaultSearxngTestImage = "docker.io/searxng/searxng:2026.10.28-c360868e5"

// startSearxng runs a SearXNG container configured with the offline demo
// engine and returns its base URL.
func startSearxng(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	image := os.Getenv("SEARXNG_TEST_IMAGE")
	if image == "" {
		image = defaultSearxngTestImage
	}

	settings, err := filepath.Abs(filepath.Join("testdata", "searxng", "settings.yml"))
	if err != nil {
		t.Fatalf("resolving settings path: %v", err)
	}

	ctr, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts("8080/tcp"),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath:      settings,
			ContainerFilePath: "/etc/searxng/settings.yml",
			FileMode:          0o644,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/healthz").WithPort("8080/tcp").WithStartupTimeout(2*time.Minute),
		),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("starting SearXNG container: %v", err)
	}

	endpoint, err := ctr.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		t.Fatalf("resolving SearXNG endpoint: %v", err)
	}
	return endpoint
}

func TestSearchTool_Integration(t *testing.T) {
	baseURL := startSearxng(t)
	tool := NewSearchTool(baseURL, 10)

	t.Run("returns results from SearXNG JSON API", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		out, err := tool.Call(ctx, SearchToolInput{Query: "Prix de l'autoroute entre Paris et Orléans"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Query != "Prix de l'autoroute entre Paris et Orléans" {
			t.Errorf("expected query to be echoed back, got %q", out.Query)
		}
		if len(out.Results) == 0 {
			t.Fatal("expected results from the demo offline engine, got none")
		}
		for i, r := range out.Results {
			if r.Engine != "demo offline" {
				t.Errorf("result %d: expected engine %q, got %q", i, "demo offline", r.Engine)
			}
		}
		for i := 1; i < len(out.Results); i++ {
			if out.Results[i-1].Score < out.Results[i].Score {
				t.Errorf("results not sorted by descending score at index %d: %v < %v", i, out.Results[i-1].Score, out.Results[i].Score)
			}
		}
	})

	t.Run("server limit caps the number of results", func(t *testing.T) {
		out, err := NewSearchTool(baseURL, 2).Call(context.Background(), SearchToolInput{Query: "test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out.Results) != 2 {
			t.Errorf("expected 2 results, got %d", len(out.Results))
		}
	})

	t.Run("base URL with trailing slash", func(t *testing.T) {
		out, err := NewSearchTool(baseURL+"/", 10).Call(context.Background(), SearchToolInput{Query: "test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out.Results) == 0 {
			t.Error("expected results")
		}
	})
}
