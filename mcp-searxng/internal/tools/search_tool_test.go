package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const searxngSample = `{
  "query": "prix autoroute paris orleans",
  "number_of_results": 0,
  "results": [
    {
      "url": "https://www.autoroutes.fr/fr/itineraire.htm",
      "title": "Calcul d'itinéraire",
      "content": "Péage Paris - Orléans : 11,30 €",
      "engine": "duckduckgo",
      "engines": ["duckduckgo", "brave"],
      "parsed_url": ["https", "www.autoroutes.fr", "/fr/itineraire.htm", "", "", ""],
      "template": "default.html",
      "positions": [1, 2],
      "score": 3.5,
      "category": "general",
      "priority": "",
      "publishedDate": null,
      "pubdate": "",
      "length": null,
      "thumbnail": "",
      "img_src": "",
      "iframe_src": "",
      "audio_src": "",
      "views": "",
      "author": "",
      "metadata": "",
      "open_group": false,
      "close_group": false
    },
    {
      "url": "https://video.example.com/v/1",
      "title": "Video",
      "content": "",
      "engine": "youtube",
      "publishedDate": "2025-01-02T10:00:00",
      "length": 95.0
    }
  ],
  "answers": [
    {"url": null, "engine": "answerer:calc", "parsed_url": null, "template": "answer/legacy.html", "answer": "42"}
  ],
  "corrections": [],
  "infoboxes": [
    {
      "infobox": "Orléans",
      "id": "https://fr.wikipedia.org/wiki/Orl%C3%A9ans",
      "content": "Orléans est une commune française.",
      "img_src": "https://upload.wikimedia.org/orleans.png",
      "urls": [{"title": "Wikipedia", "url": "https://fr.wikipedia.org/wiki/Orl%C3%A9ans"}],
      "attributes": [{"label": "Population", "value": "116 617"}],
      "engine": "wikidata",
      "engines": ["wikidata", "wikipedia"]
    }
  ],
  "suggestions": ["péage A10"],
  "unresponsive_engines": [["google", "timeout"]]
}`

func TestSearchToolOutput_DecodesSearxngJSON(t *testing.T) {
	var out SearchToolOutput
	if err := json.Unmarshal([]byte(searxngSample), &out); err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}

	if out.Query != "prix autoroute paris orleans" {
		t.Errorf("unexpected query %q", out.Query)
	}
	if len(out.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out.Results))
	}
	r := out.Results[0]
	if r.URL != "https://www.autoroutes.fr/fr/itineraire.htm" || r.Engine != "duckduckgo" || r.Score != 3.5 {
		t.Errorf("unexpected first result: %+v", r)
	}
	if r.PublishedDate != "" || r.Length != nil {
		t.Errorf("expected null publishedDate/length to decode as zero values: %+v", r)
	}
	if out.Results[1].PublishedDate != "2025-01-02T10:00:00" || out.Results[1].Length != 95.0 {
		t.Errorf("unexpected second result: %+v", out.Results[1])
	}
	if len(out.Answers) != 1 || out.Answers[0].Answer != "42" || out.Answers[0].URL != "" {
		t.Errorf("unexpected answers: %+v", out.Answers)
	}
	if len(out.Infoboxes) != 1 {
		t.Fatalf("expected 1 infobox, got %d", len(out.Infoboxes))
	}
	ib := out.Infoboxes[0]
	if ib.Infobox != "Orléans" || len(ib.URLs) != 1 || ib.Attributes[0].Value != "116 617" {
		t.Errorf("unexpected infobox: %+v", ib)
	}
	if len(out.Suggestions) != 1 || out.Suggestions[0] != "péage A10" {
		t.Errorf("unexpected suggestions: %v", out.Suggestions)
	}
}

func TestSearchTool_Metadata(t *testing.T) {
	tool := NewSearchTool("http://searxng:8080", 10)
	if tool.Name() != "search" {
		t.Errorf("unexpected tool name: %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestSearchTool_Call_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("expected path /search, got %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "prix autoroute paris orleans" {
			t.Errorf("expected q=%q, got %q", "prix autoroute paris orleans", got)
		}
		if got := r.URL.Query().Get("format"); got != "json" {
			t.Errorf("expected format=json, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searxngSample))
	}))
	defer srv.Close()

	tool := newSearchToolWithClient(srv.URL, 10, srv.Client())
	out, err := tool.Call(context.Background(), SearchToolInput{Query: "  prix autoroute paris orleans  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Results) != 2 || out.Results[0].Engine != "duckduckgo" {
		t.Errorf("unexpected results: %+v", out.Results)
	}
	if len(out.Infoboxes) != 1 || len(out.Suggestions) != 1 {
		t.Errorf("unexpected infoboxes/suggestions: %+v / %v", out.Infoboxes, out.Suggestions)
	}
}

func TestSearchTool_Call_BaseURLWithSubPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/searxng/search" {
			t.Errorf("expected path /searxng/search, got %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"query":"q","results":[]}`))
	}))
	defer srv.Close()

	tool := newSearchToolWithClient(srv.URL+"/searxng/", 10, srv.Client())
	if _, err := tool.Call(context.Background(), SearchToolInput{Query: "q"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchTool_Call_OmitsEmptyFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(searxngSample))
	}))
	defer srv.Close()

	tool := newSearchToolWithClient(srv.URL, 10, srv.Client())
	out, err := tool.Call(context.Background(), SearchToolInput{Query: "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	s := string(b)
	for _, empty := range []string{`""`, `null`, `[]`, `{}`, `:0,`, `:0}`, `:false`} {
		if strings.Contains(s, empty) {
			t.Errorf("expected no empty value %s in output, got %s", empty, s)
		}
	}
	for _, dropped := range []string{"parsed_url", "pubdate", "open_group", "close_group", "template", "positions", "engines", "category", "priority", "img_src", "unresponsive_engines"} {
		if strings.Contains(s, `"`+dropped+`"`) {
			t.Errorf("expected field %q to be dropped, got %s", dropped, s)
		}
	}
}

func TestSearchTool_Call_LimitsResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"query":"q","results":[
			{"url":"https://1","score":1},
			{"url":"https://2","score":2},
			{"url":"https://3","score":3},
			{"url":"https://4","score":4},
			{"url":"https://5","score":5}
		]}`))
	}))
	defer srv.Close()

	tests := []struct {
		name      string
		serverMax int
		wantURLs  []string
	}{
		{"keeps the best results up to the server limit", 3, []string{"https://5", "https://4", "https://3"}},
		{"limit above available results", 10, []string{"https://5", "https://4", "https://3", "https://2", "https://1"}},
		{"invalid server max falls back to 1", 0, []string{"https://5"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool := newSearchToolWithClient(srv.URL, tc.serverMax, srv.Client())
			out, err := tool.Call(context.Background(), SearchToolInput{Query: "q"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var got []string
			for _, r := range out.Results {
				got = append(got, r.URL)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantURLs, ",") {
				t.Errorf("expected %v, got %v", tc.wantURLs, got)
			}
		})
	}
}

func TestSearchTool_Call_EmptyResponseIsMinimal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"query":"q","number_of_results":0,"results":[],"answers":[],"corrections":[],"infoboxes":[],"suggestions":[],"unresponsive_engines":[]}`))
	}))
	defer srv.Close()

	tool := newSearchToolWithClient(srv.URL, 10, srv.Client())
	out, err := tool.Call(context.Background(), SearchToolInput{Query: "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := json.Marshal(out)
	if string(b) != `{"query":"q"}` {
		t.Errorf("expected minimal output, got %s", b)
	}
}

func TestSearchTool_Call_SortsResultsByScoreDesc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// SearXNG order after category/template grouping: not sorted by score.
		_, _ = w.Write([]byte(`{"query":"q","results":[
			{"url":"https://a","score":1.5},
			{"url":"https://b","score":4.0},
			{"url":"https://c"},
			{"url":"https://d","score":4.0},
			{"url":"https://e","score":2.25}
		]}`))
	}))
	defer srv.Close()

	tool := newSearchToolWithClient(srv.URL, 10, srv.Client())
	out, err := tool.Call(context.Background(), SearchToolInput{Query: "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []string
	for _, r := range out.Results {
		got = append(got, r.URL)
	}
	// Stable sort: b stays before d on equal scores; missing score sorts last.
	want := []string{"https://b", "https://d", "https://e", "https://a", "https://c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("expected order %v, got %v", want, got)
	}
}

func TestSearchTool_Call_EmptyQuery(t *testing.T) {
	tool := NewSearchTool("http://unused", 10)
	if _, err := tool.Call(context.Background(), SearchToolInput{Query: "   "}); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearchTool_Call_Errors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"json format disabled", http.StatusForbidden, "Forbidden", "search.formats"},
		{"server error", http.StatusInternalServerError, "", "status 500"},
		{"rate limited", http.StatusTooManyRequests, "", "status 429"},
		{"invalid json", http.StatusOK, "<html>", "decoding SearXNG response"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			tool := newSearchToolWithClient(srv.URL, 10, srv.Client())
			_, err := tool.Call(context.Background(), SearchToolInput{Query: "q"})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSearchTool_Call_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	tool := newSearchToolWithClient(url, 10, nil)
	if _, err := tool.Call(context.Background(), SearchToolInput{Query: "q"}); err == nil {
		t.Fatal("expected error when SearXNG is unreachable")
	}
}
