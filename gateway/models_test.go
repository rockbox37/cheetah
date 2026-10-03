package main

import (
	"encoding/json"
	"testing"
)

func TestScrapeRequestMarshal(t *testing.T) {
	req := ScrapeRequest{
		URL:    "https://example.com",
		Format: "markdown",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ScrapeRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.URL != req.URL {
		t.Fatalf("expected %s, got %s", req.URL, decoded.URL)
	}
	if decoded.Format != req.Format {
		t.Fatalf("expected %s, got %s", req.Format, decoded.Format)
	}
}

func TestCrawlRequestDefaults(t *testing.T) {
	input := `{"url":"https://example.com"}`
	var req CrawlRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatal(err)
	}
	if req.MaxPages != 0 {
		t.Fatalf("expected 0 default, got %d", req.MaxPages)
	}
}

func TestErrorResponseJSON(t *testing.T) {
	resp := ErrorResponse{Success: false, Error: "bad request"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("empty JSON output")
	}
}

func TestExtractRequestRequiresSchema(t *testing.T) {
	input := `{"url":"https://example.com","extract_schema":{"type":"object"}}`
	var req ExtractRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatal(err)
	}
	if req.URL != "https://example.com" {
		t.Fatalf("unexpected url: %s", req.URL)
	}
	if req.ExtractSchema == nil {
		t.Fatal("extract_schema should not be nil")
	}
}

func TestCrawlRequestNewFields(t *testing.T) {
	input := `{"url":"https://example.com","max_pages":50,"max_depth":5,"max_timeout":120}`
	var req CrawlRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatal(err)
	}
	if req.MaxDepth != 5 {
		t.Fatalf("expected max_depth 5, got %d", req.MaxDepth)
	}
	if req.MaxTimeout != 120 {
		t.Fatalf("expected max_timeout 120, got %d", req.MaxTimeout)
	}
}

func TestCrawlRequestOmitsZeroFields(t *testing.T) {
	req := CrawlRequest{URL: "https://example.com"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["max_depth"]; ok {
		t.Fatal("max_depth should be omitted when zero")
	}
	if _, ok := m["max_timeout"]; ok {
		t.Fatal("max_timeout should be omitted when zero")
	}
}
