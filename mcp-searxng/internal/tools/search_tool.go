package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pixime-net/talk-libs/mcpserver"
)

// SearchToolInput holds the search query. The number of results is set by the
// server (SEARCH_MAX_RESULTS) and deliberately not exposed: models tend to
// pick a small arbitrary value and miss relevant results.
type SearchToolInput struct {
	Query string `json:"query" description:"Precise web search query, in the language most likely to return good results (e.g. French for French topics)"`
}

// SearchToolOutput is the subset of the SearXNG JSON body
// (GET /search?format=json, see searx/webutils.py get_json_response) that is
// useful to a model. Rendering-only fields (template, thumbnails, positions,
// engines...) are dropped and every field except query is omitempty: SearXNG
// returns many empty values and each one would cost tokens to the model.
type SearchToolOutput struct {
	Query           string          `json:"query" description:"Query as interpreted by SearXNG"`
	NumberOfResults int             `json:"number_of_results,omitempty" description:"Estimated total number of results reported by the engines"`
	Results         []SearchResult  `json:"results,omitempty" description:"Main results sorted by descending score"`
	Answers         []SearchAnswer  `json:"answers,omitempty" description:"Direct answers (e.g. computations, unit conversions)"`
	Corrections     []string        `json:"corrections,omitempty" description:"Spelling corrections of the query"`
	Infoboxes       []SearchInfobox `json:"infoboxes,omitempty" description:"Knowledge panels (e.g. Wikipedia, Wikidata)"`
	Suggestions     []string        `json:"suggestions,omitempty" description:"Related query suggestions"`
}

// SearchResult is a SearXNG main result (searx/result_types MainResult).
type SearchResult struct {
	URL           string         `json:"url,omitempty" description:"Result URL"`
	Title         string         `json:"title,omitempty" description:"Result title"`
	Content       string         `json:"content,omitempty" description:"Snippet or description of the result"`
	Engine        string         `json:"engine,omitempty" description:"Engine that produced the result"`
	Score         float64        `json:"score,omitempty" description:"Ranking score computed by SearXNG"`
	PublishedDate string         `json:"publishedDate,omitempty" description:"Publication date in ISO 8601 format"`
	Author        string         `json:"author,omitempty" description:"Author of the content"`
	Views         StringOrNumber `json:"views,omitempty" description:"Humanized view count"`
	Metadata      string         `json:"metadata,omitempty" description:"Miscellaneous metadata"`
	// Length is a number of seconds for typed results, but legacy engines may emit a string.
	Length any `json:"length,omitempty" description:"Playing duration (seconds)"`
}

// StringOrNumber accepts a JSON string or number and renders it as a string.
// SearXNG engines disagree on the type of display-only fields such as views.
type StringOrNumber string

// UnmarshalJSON accepts strings, numbers and null; anything else decodes to "".
func (s *StringOrNumber) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = StringOrNumber(str)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(data, &num); err == nil {
		*s = StringOrNumber(num.String())
		return nil
	}
	*s = ""
	return nil
}

// SearchAnswer is a SearXNG direct answer (searx/result_types/answer.py).
type SearchAnswer struct {
	Answer string `json:"answer,omitempty" description:"Answer text"`
	URL    string `json:"url,omitempty" description:"Source URL of the answer"`
	Engine string `json:"engine,omitempty" description:"Engine or answerer that produced the answer"`
}

// SearchInfobox is a SearXNG knowledge panel.
type SearchInfobox struct {
	Infobox    string                   `json:"infobox,omitempty" description:"Infobox title"`
	ID         string                   `json:"id,omitempty" description:"Infobox identifier (usually a URL)"`
	Content    string                   `json:"content,omitempty" description:"Infobox description"`
	URLs       []SearchInfoboxURL       `json:"urls,omitempty" description:"Related links"`
	Attributes []SearchInfoboxAttribute `json:"attributes,omitempty" description:"Key/value facts"`
	Engine     string                   `json:"engine,omitempty" description:"Engine that produced the infobox"`
}

// SearchInfoboxURL is a link displayed in an infobox.
type SearchInfoboxURL struct {
	Title string `json:"title,omitempty" description:"Link title"`
	URL   string `json:"url,omitempty" description:"Link URL"`
}

// SearchInfoboxAttribute is a fact displayed in an infobox.
type SearchInfoboxAttribute struct {
	Label string `json:"label,omitempty" description:"Attribute label"`
	Value string `json:"value,omitempty" description:"Attribute value"`
}

const (
	searchHTTPTimeout = 15 * time.Second
	// maxSearchResponseBytes bounds the SearXNG body read into memory.
	maxSearchResponseBytes = 5 << 20
)

// SearchTool performs a web search through a SearXNG instance.
type SearchTool struct {
	baseURL    string
	maxResults int
	client     *http.Client
}

var _ mcpserver.MCPTool[SearchToolInput, SearchToolOutput] = (*SearchTool)(nil)

// NewSearchTool creates a SearchTool targeting the SearXNG instance at searxngURL
// and returning at most maxResults results.
func NewSearchTool(searxngURL string, maxResults int) *SearchTool {
	return newSearchToolWithClient(searxngURL, maxResults, nil)
}

// newSearchToolWithClient creates a SearchTool with a custom HTTP client (for testing).
func newSearchToolWithClient(searxngURL string, maxResults int, client *http.Client) *SearchTool {
	if client == nil {
		client = &http.Client{Timeout: searchHTTPTimeout}
	}
	if maxResults < 1 {
		maxResults = 1
	}
	return &SearchTool{baseURL: searxngURL, maxResults: maxResults, client: client}
}

func (t *SearchTool) Name() string { return "search" }
func (t *SearchTool) Description() string {
	return "Search the web and return the most relevant results (title, URL, snippet), plus direct answers and knowledge panels when available. " +
		"Use it for recent or changing information (prices, tolls, road works, regulations, opening hours, news) or facts you are not sure are still current. " +
		"Snippets are short: if they do not contain the answer, read the most promising result URLs with the 'scrape' tool. " +
		"If results are not relevant, search again with a reformulated or more specific query."
}

// Call queries SearXNG (GET /search?format=json) and returns its JSON response.
func (t *SearchTool) Call(ctx context.Context, input SearchToolInput) (SearchToolOutput, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return SearchToolOutput{}, fmt.Errorf("parameter 'query' must be a non-empty string")
	}

	endpoint, err := url.JoinPath(t.baseURL, "search")
	if err != nil {
		return SearchToolOutput{}, fmt.Errorf("invalid SearXNG URL %q: %w", t.baseURL, err)
	}
	params := url.Values{"q": {query}, "format": {"json"}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return SearchToolOutput{}, fmt.Errorf("building SearXNG request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return SearchToolOutput{}, fmt.Errorf("SearXNG request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusForbidden:
		return SearchToolOutput{}, fmt.Errorf("SearXNG returned status 403: ensure 'json' is listed in search.formats of settings.yml")
	case resp.StatusCode != http.StatusOK:
		return SearchToolOutput{}, fmt.Errorf("SearXNG returned status %d", resp.StatusCode)
	}

	var out SearchToolOutput
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSearchResponseBytes)).Decode(&out); err != nil {
		return SearchToolOutput{}, fmt.Errorf("decoding SearXNG response: %w", err)
	}
	// SearXNG groups results by category/template after ranking, so its order
	// is not strictly by score.
	sort.SliceStable(out.Results, func(i, j int) bool {
		return out.Results[i].Score > out.Results[j].Score
	})
	if len(out.Results) > t.maxResults {
		out.Results = out.Results[:t.maxResults]
	}
	return out, nil
}
