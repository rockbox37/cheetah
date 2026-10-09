package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassifyURLProtectedDomains(t *testing.T) {
	tests := []struct {
		url  string
		tier Tier
	}{
		{"https://www.linkedin.com/in/someone", TierStealth},
		{"https://instagram.com/user", TierStealth},
		{"https://m.facebook.com/page", TierStealth},
		{"https://twitter.com/user/status/123", TierStealth},
		{"https://x.com/user", TierStealth},
		{"https://www.amazon.com/dp/B001", TierStealth},
		{"https://www.zillow.com/homes", TierStealth},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			d := ClassifyURL(tt.url)
			if d.Tier != tt.tier {
				t.Fatalf("ClassifyURL(%q) = %s, want %s (reason: %s)", tt.url, d.Tier, tt.tier, d.Reason)
			}
			if d.Confidence < 0.8 {
				t.Fatalf("expected high confidence for protected domain, got %.2f", d.Confidence)
			}
		})
	}
}

func TestClassifyURLStaticDocHosts(t *testing.T) {
	tests := []string{
		"https://flask.readthedocs.io/en/latest/",
		"https://username.github.io/project/",
		"https://docs.python.org/3/library/re.html",
		"https://wiki.archlinux.org/title/Vim",
		"https://en.wikipedia.org/wiki/Go_(programming_language)",
		"https://developer.mozilla.org/en-US/docs/Web/API",
		"https://myproject.gitbook.io/docs/intro",
	}

	for _, u := range tests {
		t.Run(u, func(t *testing.T) {
			d := ClassifyURL(u)
			if d.Tier != TierFast {
				t.Fatalf("ClassifyURL(%q) = %s, want fast (reason: %s)", u, d.Tier, d.Reason)
			}
			if d.Confidence < 0.8 {
				t.Fatalf("expected high confidence for doc host, got %.2f", d.Confidence)
			}
		})
	}
}

func TestClassifyURLSPAPatterns(t *testing.T) {
	tests := []string{
		"https://app.example.com/app/settings",
		"https://example.com/dashboard/overview",
		"https://example.com/portal/login",
		"https://example.com/console/projects",
		"https://example.com/workspace/files",
	}

	for _, u := range tests {
		t.Run(u, func(t *testing.T) {
			d := ClassifyURL(u)
			if d.Tier != TierBrowser {
				t.Fatalf("ClassifyURL(%q) = %s, want browser (reason: %s)", u, d.Tier, d.Reason)
			}
		})
	}
}

func TestClassifyURLHashRouter(t *testing.T) {
	tests := []string{
		"https://example.com/app/#/home",
		"https://example.com/#!/settings",
	}

	for _, u := range tests {
		t.Run(u, func(t *testing.T) {
			d := ClassifyURL(u)
			if d.Tier != TierBrowser {
				t.Fatalf("ClassifyURL(%q) = %s, want browser (reason: %s)", u, d.Tier, d.Reason)
			}
			if d.Confidence < 0.7 {
				t.Fatalf("expected decent confidence for hash router, got %.2f", d.Confidence)
			}
		})
	}
}

func TestClassifyURLDefaultFast(t *testing.T) {
	tests := []string{
		"https://example.com/about",
		"https://blog.example.com/post/hello-world",
		"https://news.ycombinator.com",
	}

	for _, u := range tests {
		t.Run(u, func(t *testing.T) {
			d := ClassifyURL(u)
			if d.Tier != TierFast {
				t.Fatalf("ClassifyURL(%q) = %s, want fast (reason: %s)", u, d.Tier, d.Reason)
			}
		})
	}
}

func TestStreamForTier(t *testing.T) {
	tests := []struct {
		tier   Tier
		stream string
	}{
		{TierFast, "scrape_jobs"},
		{TierBrowser, "browser_jobs"},
		{TierStealth, "stealth_jobs"},
	}

	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			got := StreamForTier(tt.tier)
			if got != tt.stream {
				t.Fatalf("StreamForTier(%s) = %q, want %q", tt.tier, got, tt.stream)
			}
		})
	}
}

func TestIsTierAllowed(t *testing.T) {
	if !IsTierAllowed(TierFast, []string{"fast"}) {
		t.Fatal("fast should be allowed when plan includes fast")
	}
	if IsTierAllowed(TierBrowser, []string{"fast"}) {
		t.Fatal("browser should not be allowed when plan only includes fast")
	}
	if !IsTierAllowed(TierStealth, []string{"fast", "browser", "stealth"}) {
		t.Fatal("stealth should be allowed when plan includes it")
	}
	if IsTierAllowed(TierBrowser, nil) {
		t.Fatal("browser should not be allowed when AllowedTiers is empty (defaults to fast-only)")
	}
	if !IsTierAllowed(TierFast, nil) {
		t.Fatal("fast should be allowed when AllowedTiers is empty")
	}
}

func TestScrapeRoutesToCorrectStream(t *testing.T) {
	cfg := Config{
		RedisURL: "localhost:6379",
		APIKeys:  []string{"test-key-123"},
		Port:     "3000",
	}
	queue := NewQueueClient(cfg.RedisURL)
	allTiers := Plan{
		Name:             "pro",
		AllowedTiers:     []string{"fast", "browser", "stealth"},
		MaxRatePerMinute: 100,
	}
	app := NewApp(cfg, queue, WithPlanLoader(&stubPlanLoader{plan: allTiers}))

	body := `{"url": "https://docs.python.org/3/library/re.html"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/scrape", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusForbidden {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("docs.python.org should be allowed on pro plan, got 403: %s", string(respBody))
	}
}

func TestScrapeRejectsDisallowedTier(t *testing.T) {
	app := testApp()

	body := `{"url": "https://www.linkedin.com/in/someone"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/scrape", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 for stealth URL on free plan, got %d, body: %s", resp.StatusCode, string(respBody))
	}

	respBody, _ := io.ReadAll(resp.Body)
	var errResp ErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatal(err)
	}
	if errResp.Error == "" {
		t.Fatal("expected error message about tier requirement")
	}
}

func TestScrapeAllowsFastOnFreePlan(t *testing.T) {
	app := testApp()

	body := `{"url": "https://example.com/about"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/scrape", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("fast tier URL should be allowed on free plan")
	}
}

func TestClassifyURLPlatformStrategies(t *testing.T) {
	tests := []struct {
		url      string
		strategy string
	}{
		{"https://github.com/rockbox37/cheetah", StrategyGitHubRepo},
		{"https://www.github.com/rockbox37/cheetah/issues/5", StrategyGitHubRepo},
		{"https://github.com/rockbox37/cheetah/blob/main/README.md", StrategyGitHubRepo},
		{"https://news.ycombinator.com/item?id=12345", StrategyHNThread},
		// Not platform content: no strategy.
		{"https://github.com/", ""},
		{"https://github.com/rockbox37", ""},
		{"https://github.com/settings/profile", ""},
		{"https://github.com/topics/go", ""},
		{"https://news.ycombinator.com/", ""},
		{"https://news.ycombinator.com/item?id=abc", ""},
		{"https://news.ycombinator.com/item", ""},
		{"https://notgithub.com/a/b", ""},
		{"https://github.com.evil.example/a/b", ""},
		{"https://example.com/a/b", ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			d := ClassifyURL(tt.url)
			if d.Strategy != tt.strategy {
				t.Fatalf("ClassifyURL(%q).Strategy = %q, want %q (reason: %s)", tt.url, d.Strategy, tt.strategy, d.Reason)
			}
			if tt.strategy != "" && d.Tier != TierFast {
				t.Fatalf("platform strategy should route to the fast tier, got %s", d.Tier)
			}
		})
	}
}

func TestClassifyURLPlatformDoesNotChangeOtherRouting(t *testing.T) {
	// Protected domains keep routing to stealth with no strategy.
	for _, u := range []string{"https://x.com/user/status/1", "https://www.linkedin.com/in/someone"} {
		d := ClassifyURL(u)
		if d.Tier != TierStealth || d.Strategy != "" {
			t.Fatalf("ClassifyURL(%q) = tier %s strategy %q, want stealth with no strategy", u, d.Tier, d.Strategy)
		}
	}
}

func TestPageMetadataStrategyOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(PageMetadata{Title: "t", StatusCode: 200})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("strategy")) {
		t.Fatalf("strategy should be omitted when empty: %s", b)
	}
	b, _ = json.Marshal(PageMetadata{Title: "t", StatusCode: 200, Strategy: "generic"})
	if !bytes.Contains(b, []byte(`"strategy":"generic"`)) {
		t.Fatalf("strategy missing: %s", b)
	}
}
