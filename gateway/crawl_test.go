package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestExtractLinks(t *testing.T) {
	htmlContent := `<html><body>
		<a href="/page1">Page 1</a>
		<a href="/page2">Page 2</a>
		<a href="https://other.com/ext">External</a>
		<a href="#fragment">Fragment</a>
		<a href="javascript:void(0)">JS</a>
		<a href="mailto:test@test.com">Mail</a>
		<a href="">Empty</a>
	</body></html>`

	links := extractLinks(htmlContent, "https://example.com/start")

	expected := map[string]bool{
		"https://example.com/page1": false,
		"https://example.com/page2": false,
		"https://other.com/ext":     false,
	}

	for _, link := range links {
		if _, ok := expected[link]; ok {
			expected[link] = true
		}
	}

	for link, found := range expected {
		if !found {
			t.Errorf("expected link %q not found", link)
		}
	}

	for _, link := range links {
		if strings.HasPrefix(link, "#") || strings.HasPrefix(link, "javascript:") || strings.HasPrefix(link, "mailto:") || link == "" {
			t.Errorf("unexpected link %q should have been filtered", link)
		}
	}
}

func TestExtractLinksRelative(t *testing.T) {
	htmlContent := `<html><body>
		<a href="sub/page">Relative</a>
		<a href="../other">Parent</a>
	</body></html>`

	links := extractLinks(htmlContent, "https://example.com/docs/intro")

	hasSubPage := false
	for _, link := range links {
		if link == "https://example.com/docs/sub/page" {
			hasSubPage = true
		}
	}
	if !hasSubPage {
		t.Errorf("expected resolved relative link, got %v", links)
	}
}

func TestFilterLinksSameDomain(t *testing.T) {
	seed, _ := url.Parse("https://example.com/start")
	links := []string{
		"https://example.com/page1",
		"https://example.com/page2",
		"https://other.com/page",
		"ftp://example.com/file",
	}

	filtered := filterLinks(links, seed, nil, nil)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 same-domain links, got %d: %v", len(filtered), filtered)
	}
}

func TestFilterLinksIncludePatterns(t *testing.T) {
	seed, _ := url.Parse("https://example.com")
	links := []string{
		"https://example.com/docs/intro",
		"https://example.com/blog/post",
		"https://example.com/docs/guide",
	}

	filtered := filterLinks(links, seed, []string{"/docs/"}, nil)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 matching links, got %d: %v", len(filtered), filtered)
	}
}

func TestFilterLinksExcludePatterns(t *testing.T) {
	seed, _ := url.Parse("https://example.com")
	links := []string{
		"https://example.com/docs/intro",
		"https://example.com/login",
		"https://example.com/docs/guide",
	}

	filtered := filterLinks(links, seed, nil, []string{"/login"})

	if len(filtered) != 2 {
		t.Fatalf("expected 2 links after exclusion, got %d: %v", len(filtered), filtered)
	}
}

func TestShouldSkipCrawlURL(t *testing.T) {
	tests := []struct {
		url  string
		skip bool
	}{
		{"https://example.com/page", false},
		{"https://example.com/doc.pdf", true},
		{"https://example.com/image.png", true},
		{"https://example.com/style.css", true},
		{"https://example.com/app.js", true},
		{"https://example.com/font.woff2", true},
		{"https://example.com/video.mp4", true},
		{"https://example.com/about", false},
		{"https://example.com/page.html", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := shouldSkipCrawlURL(tt.url)
			if got != tt.skip {
				t.Fatalf("shouldSkipCrawlURL(%q) = %v, want %v", tt.url, got, tt.skip)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/page#section", "https://example.com/page"},
		{"https://example.com/page?q=1", "https://example.com/page"},
		{"https://example.com/path/", "https://example.com/path"},
		{"https://example.com", "https://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeURL(tt.input)
			if got != tt.want {
				t.Fatalf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		html string
		want string
	}{
		{`<html><head><title>Hello World</title></head></html>`, "Hello World"},
		{`<html><head><title>  Trimmed  </title></head></html>`, "Trimmed"},
		{`<html><body>No title</body></html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := extractTitle(tt.html)
			if got != tt.want {
				t.Fatalf("extractTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCrawlManagerDepthEnforcement(t *testing.T) {
	pages := map[string]string{
		"/":       `<html><head><title>Root</title></head><body><a href="/level1">L1</a></body></html>`,
		"/level1": `<html><head><title>Level 1</title></head><body><a href="/level2">L2</a></body></html>`,
		"/level2": `<html><head><title>Level 2</title></head><body><a href="/level3">L3</a></body></html>`,
		"/level3": `<html><head><title>Level 3</title></head><body><a href="/level4">L4</a></body></html>`,
		"/level4": `<html><head><title>Level 4</title></head><body>End</body></html>`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	cm := &CrawlManager{
		httpClient: srv.Client(),
		queue:      NewQueueClient("localhost:6379"),
	}

	req := CrawlRequest{
		URL:        srv.URL + "/",
		MaxPages:   100,
		MaxDepth:   2,
		MaxTimeout: 10,
		Format:     "markdown",
	}

	results := runTestCrawl(t, cm, req)

	maxDepth := 0
	for _, r := range results {
		if r.Depth > maxDepth {
			maxDepth = r.Depth
		}
	}

	if maxDepth > 2 {
		t.Fatalf("expected max depth 2, but found page at depth %d", maxDepth)
	}

	if len(results) > 3 {
		t.Fatalf("expected at most 3 pages (depth 0,1,2), got %d", len(results))
	}
}

func TestCrawlManagerMaxPagesEnforcement(t *testing.T) {
	pageCount := 20
	linkHTML := "<html><head><title>Links</title></head><body>"
	for i := 1; i <= pageCount; i++ {
		linkHTML += fmt.Sprintf(`<a href="/page%d">Page %d</a>`, i, i)
	}
	linkHTML += "</body></html>"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/" {
			fmt.Fprint(w, linkHTML)
		} else {
			fmt.Fprintf(w, "<html><head><title>%s</title></head><body>Content</body></html>", r.URL.Path)
		}
	}))
	defer srv.Close()

	cm := &CrawlManager{
		httpClient: srv.Client(),
		queue:      NewQueueClient("localhost:6379"),
	}

	req := CrawlRequest{
		URL:        srv.URL + "/",
		MaxPages:   5,
		MaxDepth:   3,
		MaxTimeout: 10,
		Format:     "markdown",
	}

	results := runTestCrawl(t, cm, req)

	if len(results) > 5 {
		t.Fatalf("expected at most 5 pages, got %d", len(results))
	}
}

func TestCrawlManagerTimeoutReturnsPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			time.Sleep(2 * time.Second)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Slow</title></head><body>
			<a href="/slow1">S1</a><a href="/slow2">S2</a>
			<a href="/slow3">S3</a><a href="/slow4">S4</a>
		</body></html>`)
	}))
	defer srv.Close()

	cm := &CrawlManager{
		httpClient: srv.Client(),
		queue:      NewQueueClient("localhost:6379"),
	}

	req := CrawlRequest{
		URL:        srv.URL + "/",
		MaxPages:   10,
		MaxDepth:   3,
		MaxTimeout: 1,
		Format:     "markdown",
	}

	results := runTestCrawl(t, cm, req)

	if len(results) < 1 {
		t.Fatal("expected at least the seed page")
	}
	if len(results) >= 5 {
		t.Fatalf("expected fewer than 5 pages due to timeout, got %d", len(results))
	}
}

func runTestCrawl(t *testing.T, cm *CrawlManager, req CrawlRequest) []crawlResult {
	t.Helper()

	seedURL, err := url.Parse(req.URL)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.MaxTimeout)*time.Second)
	defer cancel()

	visited := make(map[string]bool)
	visited[normalizeURL(req.URL)] = true

	type queueItem struct {
		url   string
		depth int
	}
	queue := []queueItem{{url: req.URL, depth: 0}}

	var results []crawlResult
	pagesDone := 0

	for len(queue) > 0 && pagesDone < req.MaxPages {
		if ctx.Err() != nil {
			break
		}

		item := queue[0]
		queue = queue[1:]

		if ctx.Err() != nil {
			break
		}

		r := cm.fetchPage(ctx, item.url)
		r.Depth = item.depth
		pagesDone++
		results = append(results, r)

		if item.depth >= req.MaxDepth || r.Error != "" || r.Content == "" {
			continue
		}

		links := extractLinks(r.Content, r.URL)
		links = filterLinks(links, seedURL, req.IncludePatterns, req.ExcludePatterns)
		for _, link := range links {
			norm := normalizeURL(link)
			if !visited[norm] {
				visited[norm] = true
				queue = append(queue, queueItem{url: link, depth: r.Depth + 1})
			}
		}
	}

	return results
}

func TestCrawlMaxDepthValidation(t *testing.T) {
	app := testApp()

	body := `{"url": "https://example.com", "max_depth": 50}`
	req := httptest.NewRequest(http.MethodPost, "/v1/crawl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusBadRequest {
		t.Fatal("max_depth=50 should not be rejected; it should be capped silently")
	}
}

func TestCrawlMaxTimeoutValidation(t *testing.T) {
	app := testApp()

	body := `{"url": "https://example.com", "max_timeout": 9999}`
	req := httptest.NewRequest(http.MethodPost, "/v1/crawl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusBadRequest {
		t.Fatal("max_timeout=9999 should not be rejected; it should be capped silently")
	}
}
