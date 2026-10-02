package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func testApp() *fiber.App {
	cfg := Config{
		RedisURL: "localhost:6379",
		APIKeys:  []string{"test-key-123"},
		Port:     "3000",
	}
	queue := NewQueueClient(cfg.RedisURL)
	return NewApp(cfg, queue)
}

func TestScrapeRequiresAuth(t *testing.T) {
	app := testApp()

	body := bytes.NewBufferString(`{"url": "https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/scrape", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result ErrorResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if result.Success {
		t.Fatal("expected success=false")
	}
}

func TestScrapeValidatesBody(t *testing.T) {
	app := testApp()

	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "empty body",
			body: `{}`,
			want: http.StatusBadRequest,
		},
		{
			name: "missing url",
			body: `{"format": "markdown"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "invalid url",
			body: `{"url": "not-a-url"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "invalid format",
			body: `{"url": "https://example.com", "format": "pdf"}`,
			want: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/scrape", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", "test-key-123")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.want {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected %d, got %d, body: %s", tt.want, resp.StatusCode, string(body))
			}
		})
	}
}

func TestCrawlFormatValidation(t *testing.T) {
	app := testApp()

	body := bytes.NewBufferString(`{"url": "https://example.com", "format": "pdf"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/crawl", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for invalid crawl format, got %d, body: %s", resp.StatusCode, string(respBody))
	}
}

func TestRateLimiterHeadersPresent(t *testing.T) {
	app := testApp()

	body := bytes.NewBufferString(`{"url": "https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/scrape", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	limit := resp.Header.Get("X-RateLimit-Limit")
	if limit == "" {
		t.Fatal("missing X-RateLimit-Limit header")
	}
	if limit != "100" {
		t.Fatalf("expected X-RateLimit-Limit=100, got %s", limit)
	}

	remaining := resp.Header.Get("X-RateLimit-Remaining")
	if remaining == "" {
		t.Fatal("missing X-RateLimit-Remaining header")
	}
}

func TestCrawlRequiresAuth(t *testing.T) {
	app := testApp()

	body := bytes.NewBufferString(`{"url": "https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/crawl", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestExtractRequiresSchema(t *testing.T) {
	app := testApp()

	body := bytes.NewBufferString(`{"url": "https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/extract", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d, body: %s", resp.StatusCode, string(respBody))
	}
}

func TestCrawlMaxPagesCapped(t *testing.T) {
	app := testApp()

	body := bytes.NewBufferString(`{"url": "https://example.com", "max_pages": 50000}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/crawl", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusBadRequest {
		t.Fatal("max_pages=50000 should not be rejected; it should be capped silently")
	}
}

func TestOwnerPersistsInJSON(t *testing.T) {
	status := JobStatus{
		JobID:  "test-123",
		Status: "queued",
		Owner:  "hash-abc",
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var decoded JobStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Owner != "hash-abc" {
		t.Fatalf("expected owner to survive marshal/unmarshal, got %q", decoded.Owner)
	}
}

func TestOwnerStrippedFromResponse(t *testing.T) {
	status := JobStatus{
		JobID:  "test-123",
		Status: "queued",
		Owner:  "hash-abc",
	}
	status.Owner = ""
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["owner"]; ok {
		t.Fatal("Owner should not appear after being zeroed (omitempty)")
	}
}

func TestCallerOwnerHashes(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("api_key", "test-key-123")
		return c.Next()
	})
	app.Get("/", func(c *fiber.Ctx) error {
		owner := callerOwner(c)
		if owner == "test-key-123" {
			return fiber.NewError(500, "owner should be hashed, not raw key")
		}
		if len(owner) != 64 {
			return fiber.NewError(500, "expected 64-char hex SHA-256 hash")
		}
		return c.SendString(owner)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d, body: %s", resp.StatusCode, string(respBody))
	}
}

func TestCallerOwnerAnonymous(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("api_key", "anonymous")
		return c.Next()
	})
	app.Get("/", func(c *fiber.Ctx) error {
		owner := callerOwner(c)
		if owner != "anonymous" {
			return fiber.NewError(500, "expected anonymous, got "+owner)
		}
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d, body: %s", resp.StatusCode, string(respBody))
	}
}

func TestScrapeRejectsSSRF(t *testing.T) {
	app := testApp()

	tests := []struct {
		name string
		url  string
	}{
		{"loopback", "http://127.0.0.1/"},
		{"private 10.x", "http://10.0.0.1/"},
		{"metadata endpoint", "http://169.254.169.254/latest/meta-data/"},
		{"ftp scheme", "ftp://example.com/file"},
		{"file scheme", "file:///etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(`{"url": "` + tt.url + `"}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/scrape", body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", "test-key-123")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				respBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 400 for SSRF attempt %q, got %d, body: %s", tt.url, resp.StatusCode, string(respBody))
			}
		})
	}
}
