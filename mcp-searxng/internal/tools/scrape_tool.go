package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/pixime-net/talk-libs/mcpserver"
)

const (
	scrapeHTTPTimeout = 20 * time.Second
	scrapeUserAgent   = "Mozilla/5.0 (compatible; mcp-searxng/1.0)"
	// maxScrapeBodyBytes bounds the page size read into memory.
	maxScrapeBodyBytes = 5 << 20
	maxScrapeRedirects = 5
)

// noiseTags are page-chrome and media elements that carry no content useful
// to a text model; images are dropped because their URLs are token-expensive.
var noiseTags = []string{
	"nav", "footer", "aside", "form", "button", "select", "dialog",
	"svg", "canvas", "img", "picture", "video", "audio",
}

var errBlockedAddress = errors.New("destination address is not allowed")

var (
	// emptyLinkRe matches links left without text once their image is removed.
	emptyLinkRe = regexp.MustCompile(`\[\s*\]\([^)]*\)`)
	blankRunRe  = regexp.MustCompile(`\n[ \t]*\n(?:[ \t]*\n)+`)
)

// cleanMarkdown drops empty links and collapses the blank lines they leave.
func cleanMarkdown(md string) string {
	md = emptyLinkRe.ReplaceAllString(md, "")
	return blankRunRe.ReplaceAllString(md, "\n\n")
}

// ScrapeToolInput holds the URL to scrape.
type ScrapeToolInput struct {
	URL        string `json:"url" description:"Absolute http(s) URL of the page to fetch"`
	StartIndex int    `json:"start_index,omitempty" description:"Optional character offset to start reading from. Use the next_index value of a previous call on the same URL to read the following part of the page."`
	MaxChars   int    `json:"max_chars,omitempty" description:"Optional maximum number of characters of content to return. Omit to use the server default, which is also the upper bound; larger values are capped."`
}

// ScrapeToolOutput holds the page converted to markdown.
type ScrapeToolOutput struct {
	URL        string `json:"url" description:"Final URL after redirects"`
	Title      string `json:"title,omitempty" description:"Page title"`
	Content    string `json:"content" description:"Page content in markdown"`
	TotalChars int    `json:"total_chars" description:"Total number of characters of the page content"`
	NextIndex  int    `json:"next_index,omitempty" description:"Present when more content is available: pass it as start_index to read the next part"`
}

// ScrapeTool fetches a web page and converts it to markdown.
type ScrapeTool struct {
	client   *http.Client
	conv     *converter.Converter
	maxChars int
}

var _ mcpserver.MCPTool[ScrapeToolInput, ScrapeToolOutput] = (*ScrapeTool)(nil)

// NewScrapeTool creates a ScrapeTool that refuses to reach loopback, private,
// link-local and other non-public addresses, so the tool cannot be used to
// probe the internal network (including the bundled SearXNG instance).
// maxChars is both the default content length and the upper bound accepted
// from the max_chars input parameter.
func NewScrapeTool(maxChars int) *ScrapeTool {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: publicAddressOnly}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       60 * time.Second,
	}
	return newScrapeToolWithClient(maxChars, &http.Client{Timeout: scrapeHTTPTimeout, Transport: transport})
}

// newScrapeToolWithClient creates a ScrapeTool with a custom HTTP client (for testing).
func newScrapeToolWithClient(maxChars int, client *http.Client) *ScrapeTool {
	if maxChars < 1 {
		maxChars = 1
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxScrapeRedirects {
			return fmt.Errorf("stopped after %d redirects", maxScrapeRedirects)
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
		}
		return nil
	}

	conv := converter.NewConverter(converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
	))
	for _, tag := range noiseTags {
		conv.Register.TagType(tag, converter.TagTypeRemove, converter.PriorityStandard)
	}
	return &ScrapeTool{client: client, conv: conv, maxChars: maxChars}
}

// charLimit returns the number of characters to keep for a request.
func (t *ScrapeTool) charLimit(requested int) int {
	if requested < 1 || requested > t.maxChars {
		return t.maxChars
	}
	return requested
}

func (t *ScrapeTool) Name() string { return "scrape" }
func (t *ScrapeTool) Description() string {
	return "Fetch a web page from its URL and return its main content converted to markdown. " +
		"Long pages are returned in parts: when next_index is present, call again with start_index=next_index only if the rest of the page is needed."
}

// Call downloads input.URL, converts the page to markdown and returns the
// part of it starting at input.StartIndex.
func (t *ScrapeTool) Call(ctx context.Context, input ScrapeToolInput) (ScrapeToolOutput, error) {
	if input.StartIndex < 0 {
		return ScrapeToolOutput{}, fmt.Errorf("parameter 'start_index' must not be negative")
	}
	target, err := parseScrapeURL(input.URL)
	if err != nil {
		return ScrapeToolOutput{}, err
	}

	resp, err := t.fetch(ctx, target)
	if err != nil {
		return ScrapeToolOutput{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	page, err := t.extract(ctx, resp)
	if err != nil {
		return ScrapeToolOutput{}, err
	}
	return paginate(page, input.StartIndex, t.charLimit(input.MaxChars))
}

// fetch performs the GET request and returns a successful (2xx) response.
// The caller must close the response body.
func (t *ScrapeTool) fetch(ctx context.Context, target *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", scrapeUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.1")

	resp, err := t.client.Do(req)
	if errors.Is(err, errBlockedAddress) {
		return nil, fmt.Errorf("fetching %s: %w", target.Host, errBlockedAddress)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", target, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetching %s: status %d", target, resp.StatusCode)
	}
	return resp, nil
}

// extract decodes the response body and returns the whole page: HTML is
// converted to markdown, other text types are returned as is.
func (t *ScrapeTool) extract(ctx context.Context, resp *http.Response) (ScrapeToolOutput, error) {
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	isHTML := mediaType == "" || mediaType == "text/html" || mediaType == "application/xhtml+xml"
	if !isHTML && !strings.HasPrefix(mediaType, "text/") {
		return ScrapeToolOutput{}, fmt.Errorf("unsupported content type %q", mediaType)
	}

	out := ScrapeToolOutput{URL: resp.Request.URL.String()}
	body, err := charset.NewReader(io.LimitReader(resp.Body, maxScrapeBodyBytes), contentType)
	if errors.Is(err, io.EOF) {
		return out, nil
	}
	if err != nil {
		return ScrapeToolOutput{}, fmt.Errorf("decoding charset: %w", err)
	}

	if isHTML {
		out.Title, out.Content, err = t.htmlToMarkdown(ctx, body, out.URL)
		if err != nil {
			return ScrapeToolOutput{}, err
		}
		return out, nil
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return ScrapeToolOutput{}, fmt.Errorf("reading body: %w", err)
	}
	out.Content = string(raw)
	return out, nil
}

// htmlToMarkdown parses an HTML document and returns its title and its
// content in markdown, with relative links resolved against pageURL.
func (t *ScrapeTool) htmlToMarkdown(ctx context.Context, body io.Reader, pageURL string) (title, content string, err error) {
	doc, err := html.Parse(body)
	if err != nil {
		return "", "", fmt.Errorf("parsing HTML: %w", err)
	}
	// Read the title first: the conversion mutates doc and drops <head>.
	title = documentTitle(doc)
	md, err := t.conv.ConvertNode(doc,
		converter.WithContext(ctx),
		converter.WithDomain(pageURL),
	)
	if err != nil {
		return "", "", fmt.Errorf("converting to markdown: %w", err)
	}
	return title, cleanMarkdown(string(md)), nil
}

// paginate keeps the part of page.Content starting at start and holding at
// most limit characters, and sets TotalChars and NextIndex accordingly.
func paginate(page ScrapeToolOutput, start, limit int) (ScrapeToolOutput, error) {
	content := []rune(strings.TrimSpace(page.Content))
	page.TotalChars = len(content)
	if start > 0 && start >= page.TotalChars {
		return ScrapeToolOutput{}, fmt.Errorf("start_index %d is beyond the end of the content (%d characters)", start, page.TotalChars)
	}
	end := chunkEnd(content, start, limit)
	page.Content = strings.TrimSpace(string(content[start:end]))
	if end < page.TotalChars {
		page.NextIndex = end
	}
	return page, nil
}

func parseScrapeURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("parameter 'url' must be a non-empty string")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid url %q: scheme must be http or https", raw)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("invalid url %q: missing host", raw)
	}
	return u, nil
}

// documentTitle returns the text of the first <title> element.
func documentTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		var sb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				sb.WriteString(c.Data)
			}
		}
		return strings.Join(strings.Fields(sb.String()), " ")
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if title := documentTitle(c); title != "" {
			return title
		}
	}
	return ""
}

// chunkEnd returns the end offset of the chunk starting at start and holding
// at most limit runes. When the content goes on, it prefers to cut after a
// paragraph, a line or a word found in the last third of the chunk.
func chunkEnd(content []rune, start, limit int) int {
	end := start + limit
	if end >= len(content) {
		return len(content)
	}
	minEnd := end - limit/3
	window := string(content[minEnd:end])
	for _, sep := range []string{"\n\n", "\n", " "} {
		if i := strings.LastIndex(window, sep); i >= 0 {
			return minEnd + utf8.RuneCountInString(window[:i+len(sep)])
		}
	}
	return end
}

// publicAddressOnly is a net.Dialer Control hook run on the resolved IP of
// every connection, redirects included, which also defeats DNS rebinding.
func publicAddressOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return errBlockedAddress
	}
	if !isPublicAddr(addr.Unmap()) {
		return errBlockedAddress
	}
	return nil
}

var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

func isPublicAddr(addr netip.Addr) bool {
	return addr.IsValid() &&
		addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() &&
		!cgnatPrefix.Contains(addr)
}
