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
		APIKeys:  map[string]bool{"test-key-123": true},
		Port:     "3000",
	}
	queue := NewQueueClient(cfg.RedisURL)
	return NewApp(cfg, queue)
}

func TestHealthEndpoint(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	status, ok := result["status"].(string)
	if !ok {
		t.Fatal("missing status field")
	}
	if status != "ok" && status != "degraded" {
		t.Fatalf("unexpected status: %s", status)
	}
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

func TestHealthNoAuth(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health should not require auth, got %d", resp.StatusCode)
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
