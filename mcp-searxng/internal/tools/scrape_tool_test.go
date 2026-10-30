package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

const scrapeSamplePage = `<!DOCTYPE html>
<html>
<head>
  <title>  Péage   A10 </title>
  <style>body{color:red}</style>
  <script>alert("x")</script>
</head>
<body>
  <nav><a href="/home">Accueil</a></nav>
  <main>
    <h1>Tarifs autoroute</h1>
    <p>Le trajet <strong>Paris - Orléans</strong> coûte 11,30 €.</p>
    <p><a href="/details">Voir le détail</a></p>
  </main>
  <footer>Mentions légales</footer>
</body>
</html>`

// testScrapeMaxChars is the server-side limit used by unit tests.
const testScrapeMaxChars = 1000

// newTestScrapeTool uses the test server client, which bypasses the
// public-address guard so that httptest servers on 127.0.0.1 are reachable.
func newTestScrapeTool(srv *httptest.Server) *ScrapeTool {
	return newScrapeToolWithClient(testScrapeMaxChars, srv.Client())
}

func TestScrapeTool_Metadata(t *testing.T) {
	tool := NewScrapeTool(testScrapeMaxChars)
	if tool.Name() != "scrape" {
		t.Errorf("unexpected tool name: %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestScrapeTool_Call_ConvertsHTMLToMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); !strings.Contains(ua, "mcp-searxng") {
			t.Errorf("unexpected User-Agent %q", ua)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(scrapeSamplePage))
	}))
	defer srv.Close()

	out, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL + "/page"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Title != "Péage A10" {
		t.Errorf("unexpected title %q", out.Title)
	}
	if out.URL != srv.URL+"/page" {
		t.Errorf("unexpected url %q", out.URL)
	}
	for _, want := range []string{
		"# Tarifs autoroute",
		"**Paris - Orléans**",
		"11,30 €",
		"[Voir le détail](" + srv.URL + "/details)",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("expected content to contain %q, got:\n%s", want, out.Content)
		}
	}
	for _, unwanted := range []string{"<", "alert", "color:red", "Accueil", "Mentions légales"} {
		if strings.Contains(out.Content, unwanted) {
			t.Errorf("expected content not to contain %q, got:\n%s", unwanted, out.Content)
		}
	}
	if out.NextIndex != 0 || out.TotalChars != len([]rune(out.Content)) {
		t.Errorf("expected whole content in one part, got next_index=%d total_chars=%d", out.NextIndex, out.TotalChars)
	}
}

func TestScrapeTool_Call_DropsImages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
<p>Avant <img src="/very/long/image/path.png" alt="photo"> après</p>
<figure><picture><source srcset="/a.webp"><img src="/a.jpg"></picture><figcaption>Légende utile</figcaption></figure>
<p><a href="/logo-link"><img src="/logo.png" alt="logo"></a></p>
<video src="/clip.mp4"></video>
</body></html>`))
	}))
	defer srv.Close()

	out, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, unwanted := range []string{"![", "[]", ".png", ".jpg", ".webp", ".mp4", "\n\n\n"} {
		if strings.Contains(out.Content, unwanted) {
			t.Errorf("expected content not to contain %q, got:\n%s", unwanted, out.Content)
		}
	}
	for _, want := range []string{"Avant", "après", "Légende utile"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("expected content to contain %q, got:\n%s", want, out.Content)
		}
	}
}

func TestScrapeTool_Call_DecodesCharset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		// "Orléans" encoded in ISO-8859-1.
		_, _ = w.Write([]byte("<html><body><p>Orl\xe9ans</p></body></html>"))
	}))
	defer srv.Close()

	out, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "Orléans" {
		t.Errorf("expected %q, got %q", "Orléans", out.Content)
	}
}

func TestScrapeTool_Call_FollowsRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<p>moved</p>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL + "/old"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.URL != srv.URL+"/new" || out.Content != "moved" {
		t.Errorf("unexpected output: %+v", out)
	}
}

func TestScrapeTool_Call_TooManyRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL}); err == nil {
		t.Fatal("expected error on redirect loop")
	}
}

func TestScrapeTool_Call_PlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("  <b>not html</b>\n"))
	}))
	defer srv.Close()

	out, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "<b>not html</b>" || out.Title != "" {
		t.Errorf("expected plain text returned as is, got %+v", out)
	}
}

func TestScrapeTool_Call_TruncatesLongContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("é", 2*testScrapeMaxChars)))
	}))
	defer srv.Close()
	tool := newTestScrapeTool(srv)

	tests := []struct {
		name      string
		maxChars  int
		wantRunes int
	}{
		{"server default when omitted", 0, testScrapeMaxChars},
		{"negative falls back to default", -5, testScrapeMaxChars},
		{"smaller requested limit", 100, 100},
		{"larger requested limit is capped", 10 * testScrapeMaxChars, testScrapeMaxChars},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tool.Call(context.Background(), ScrapeToolInput{URL: srv.URL, MaxChars: tc.maxChars})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.NextIndex != tc.wantRunes || out.TotalChars != 2*testScrapeMaxChars {
				t.Errorf("expected next_index=%d total_chars=%d, got %d/%d", tc.wantRunes, 2*testScrapeMaxChars, out.NextIndex, out.TotalChars)
			}
			if n := utf8.RuneCountInString(out.Content); n != tc.wantRunes {
				t.Errorf("expected %d runes, got %d", tc.wantRunes, n)
			}
		})
	}
}

func TestScrapeTool_Call_ContentWithinLimitIsNotTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("a", 100)))
	}))
	defer srv.Close()

	out, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL, MaxChars: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.NextIndex != 0 || len(out.Content) != 100 {
		t.Errorf("expected untouched content, got next_index=%d len=%d", out.NextIndex, len(out.Content))
	}
}

// pagedText is 3 paragraphs of 5 words: "pN-wM" separated by spaces.
func pagedText() string {
	var paras []string
	for p := 1; p <= 3; p++ {
		var words []string
		for w := 1; w <= 5; w++ {
			words = append(words, fmt.Sprintf("p%d-w%d", p, w))
		}
		paras = append(paras, strings.Join(words, " "))
	}
	return strings.Join(paras, "\n\n")
}

func TestScrapeTool_Call_PaginatesOnBoundaries(t *testing.T) {
	text := pagedText()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(text))
	}))
	defer srv.Close()
	tool := newTestScrapeTool(srv)

	// Each paragraph is 29 chars + "\n\n": a 40-char window ends inside the
	// second paragraph, after the first paragraph break.
	var parts []string
	start := 0
	for i := 0; ; i++ {
		if i > 10 {
			t.Fatal("pagination does not terminate")
		}
		out, err := tool.Call(context.Background(), ScrapeToolInput{URL: srv.URL, StartIndex: start, MaxChars: 40})
		if err != nil {
			t.Fatalf("unexpected error at start_index=%d: %v", start, err)
		}
		if out.TotalChars != len(text) {
			t.Errorf("expected total_chars %d, got %d", len(text), out.TotalChars)
		}
		parts = append(parts, out.Content)
		if out.NextIndex == 0 {
			break
		}
		if out.NextIndex <= start {
			t.Fatalf("next_index %d does not progress from %d", out.NextIndex, start)
		}
		start = out.NextIndex
	}

	if parts[0] != "p1-w1 p1-w2 p1-w3 p1-w4 p1-w5" {
		t.Errorf("expected first part to stop at the paragraph break, got %q", parts[0])
	}
	for _, p := range parts {
		if strings.HasPrefix(p, "-") || strings.HasSuffix(p, "-") {
			t.Errorf("part cut inside a word: %q", p)
		}
	}
	if got := strings.Join(strings.Fields(strings.Join(parts, " ")), " "); got != strings.Join(strings.Fields(text), " ") {
		t.Errorf("parts do not rebuild the content:\n%q", got)
	}
}

func TestScrapeTool_Call_StartIndexErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	tool := newTestScrapeTool(srv)

	for _, start := range []int{-1, 5, 100} {
		if _, err := tool.Call(context.Background(), ScrapeToolInput{URL: srv.URL, StartIndex: start}); err == nil {
			t.Errorf("expected error for start_index=%d", start)
		}
	}
}

func TestChunkEnd_HardCutWithoutSeparator(t *testing.T) {
	content := []rune(strings.Repeat("é", 50))
	if got := chunkEnd(content, 10, 20); got != 30 {
		t.Errorf("expected hard cut at 30, got %d", got)
	}
	if got := chunkEnd(content, 40, 20); got != 50 {
		t.Errorf("expected end of content 50, got %d", got)
	}
}

func TestNewScrapeTool_NonPositiveMaxChars(t *testing.T) {
	if got := newScrapeToolWithClient(0, &http.Client{}).charLimit(0); got != 1 {
		t.Errorf("expected limit clamped to 1, got %d", got)
	}
}

func TestScrapeTool_Call_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
	}))
	defer srv.Close()

	out, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "" || out.URL != srv.URL {
		t.Errorf("unexpected output: %+v", out)
	}
}

func TestScrapeTool_Call_Errors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		wantErr     string
	}{
		{"not found", http.StatusNotFound, "text/html", "status 404"},
		{"server error", http.StatusInternalServerError, "text/html", "status 500"},
		{"pdf", http.StatusOK, "application/pdf", "unsupported content type"},
		{"image", http.StatusOK, "image/png", "unsupported content type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			_, err := newTestScrapeTool(srv).Call(context.Background(), ScrapeToolInput{URL: srv.URL})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestScrapeTool_Call_InvalidURL(t *testing.T) {
	tool := NewScrapeTool(testScrapeMaxChars)
	for _, raw := range []string{"", "   ", "example.com/page", "ftp://example.com", "file:///etc/passwd", "http://", "javascript:alert(1)"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := tool.Call(context.Background(), ScrapeToolInput{URL: raw}); err == nil {
				t.Fatalf("expected error for url %q", raw)
			}
		})
	}
}

func TestScrapeTool_Call_BlocksInternalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("internal server must not be reached")
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	tool := NewScrapeTool(testScrapeMaxChars)
	for _, raw := range []string{
		srv.URL,
		"http://localhost:" + u.Port() + "/",
		"http://[::1]:1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://0.0.0.0:1/",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := tool.Call(context.Background(), ScrapeToolInput{URL: raw})
			if !errors.Is(err, errBlockedAddress) {
				t.Fatalf("expected errBlockedAddress for %q, got %v", raw, err)
			}
		})
	}
}

func TestScrapeTool_Call_BlocksRedirectToInternalAddress(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("internal server must not be reached")
	}))
	defer internal.Close()

	// The public hop is simulated by a fake transport; the redirect target is
	// then dialled through the guarded transport of the real tool.
	tool := NewScrapeTool(testScrapeMaxChars)
	guarded := tool.client.Transport
	tool.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "public.example" {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {internal.URL}},
				Body:       http.NoBody,
				Request:    r,
			}, nil
		}
		return guarded.RoundTrip(r)
	})

	_, err := tool.Call(context.Background(), ScrapeToolInput{URL: "http://public.example/"})
	if !errors.Is(err, errBlockedAddress) {
		t.Fatalf("expected errBlockedAddress, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIsPublicAddr(t *testing.T) {
	tests := map[string]bool{
		"8.8.8.8":         true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"::1":             false,
		"10.1.2.3":        false,
		"172.16.0.1":      false,
		"192.168.0.1":     false,
		"169.254.169.254": false,
		"fe80::1":         false,
		"fc00::1":         false,
		"100.64.0.1":      false,
		"0.0.0.0":         false,
		"224.0.0.1":       false,
	}
	for raw, want := range tests {
		if got := isPublicAddr(netip.MustParseAddr(raw)); got != want {
			t.Errorf("isPublicAddr(%s) = %v, want %v", raw, got, want)
		}
	}
}
