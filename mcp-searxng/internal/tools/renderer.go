package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	// defaultRenderTimeout bounds a single render when no timeout is configured.
	defaultRenderTimeout = 20 * time.Second
	// renderSettleDelay lets single-page apps populate the DOM after the page
	// load event before the DOM is captured.
	renderSettleDelay = 500 * time.Millisecond
)

// cdpRenderer drives a remote Chromium over the DevTools Protocol. It works
// with browserless, a Playwright server or any Chrome started with
// --remote-debugging-port.
type cdpRenderer struct {
	wsURL   string
	timeout time.Duration
}

// NewCDPRenderer returns a renderer that connects to the given CDP WebSocket
// endpoint (for example ws://browser:3000). It must be the endpoint served by
// the browser (browserless exposes CDP on "/", raw Chrome on
// "/devtools/browser/<id>"). When token is set and the URL has no token query
// parameter, it is appended as ?token=<token> (browserless authentication). An
// explicit token in the URL always wins. A non-positive timeout falls back to
// defaultRenderTimeout.
func NewCDPRenderer(wsURL, token string, timeout time.Duration) PageRenderer {
	if timeout <= 0 {
		timeout = defaultRenderTimeout
	}
	return &cdpRenderer{wsURL: withToken(wsURL, token), timeout: timeout}
}

// withToken returns wsURL with a token query parameter when token is non-empty
// and wsURL does not already carry one.
func withToken(wsURL, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return wsURL
	}
	u, err := url.Parse(wsURL)
	if err != nil {
		return wsURL
	}
	q := u.Query()
	if q.Has("token") {
		return wsURL
	}
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

// Render loads target in the browser, waits for the DOM to settle and returns
// the final URL, the title and the serialized HTML document.
func (r *cdpRenderer) Render(ctx context.Context, target *url.URL) (finalURL, title, html string, err error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// NoModifyURL keeps the endpoint exactly as configured, preserving the
	// browserless token query parameter (browserless serves CDP on "/").
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, r.wsURL, chromedp.NoModifyURL)
	defer cancelAlloc()
	tabCtx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()

	err = chromedp.Run(tabCtx,
		chromedp.Navigate(target.String()),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(renderSettleDelay),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
		chromedp.Evaluate(`document.documentElement.outerHTML`, &html),
	)
	if err != nil {
		return "", "", "", fmt.Errorf("chromedp: %w", err)
	}
	return finalURL, title, html, nil
}
