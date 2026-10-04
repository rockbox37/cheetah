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

func TestMCPDiscoveryReturnsTools(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Name     string    `json:"name"`
		Version  string    `json:"version"`
		Endpoint string    `json:"endpoint"`
		Tools    []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Name != "cheetah" {
		t.Fatalf("expected name=cheetah, got %s", result.Name)
	}
	if result.Endpoint != "/v1/mcp" {
		t.Fatalf("expected endpoint=/v1/mcp, got %s", result.Endpoint)
	}
	if len(result.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(result.Tools))
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"scrape", "crawl", "extract"} {
		if !names[want] {
			t.Fatalf("missing tool: %s", want)
		}
	}
}

func TestMCPDiscoveryNoAuth(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discovery should not require auth, got %d", resp.StatusCode)
	}
}

func mcpRequest(method string, id interface{}, params interface{}) []byte {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if id != nil {
		req["id"] = id
	}
	if params != nil {
		req["params"] = params
	}
	b, _ := json.Marshal(req)
	return b
}

func postMCP(t *testing.T, app *fiber.App, body []byte) (*http.Response, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			t.Fatalf("invalid JSON response: %v\nbody: %s", err, string(respBody))
		}
	}
	return resp, result
}

func TestMCPInitialize(t *testing.T) {
	app := testApp()
	body := mcpRequest("initialize", 1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	})

	resp, result := postMCP(t, app, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	r, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatal("missing result")
	}
	if r["protocolVersion"] != "2024-11-05" {
		t.Fatalf("expected protocolVersion=2024-11-05, got %v", r["protocolVersion"])
	}
	si, ok := r["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("missing serverInfo")
	}
	if si["name"] != "cheetah" {
		t.Fatalf("expected serverInfo.name=cheetah, got %v", si["name"])
	}
}

func TestMCPToolsList(t *testing.T) {
	app := testApp()
	body := mcpRequest("tools/list", 2, nil)

	resp, result := postMCP(t, app, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	r, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatal("missing result")
	}
	tools, ok := r["tools"].([]interface{})
	if !ok {
		t.Fatal("missing tools array")
	}
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
}

func TestMCPToolCallScrapeValidatesURL(t *testing.T) {
	app := testApp()
	body := mcpRequest("tools/call", 3, map[string]interface{}{
		"name": "scrape",
		"arguments": map[string]interface{}{
			"url":    "https://example.com",
			"format": "markdown",
		},
	})

	resp, result := postMCP(t, app, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	r, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatal("missing result")
	}
	content, ok := r["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("missing content in tool result")
	}
}

func TestMCPToolCallCrawlValidatesURL(t *testing.T) {
	app := testApp()
	body := mcpRequest("tools/call", 4, map[string]interface{}{
		"name": "crawl",
		"arguments": map[string]interface{}{
			"url":       "https://example.com",
			"max_pages": 5,
		},
	})

	resp, result := postMCP(t, app, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	r, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatal("missing result")
	}
	content, ok := r["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("missing content in tool result")
	}
}

func TestMCPToolCallExtractNoEndpoint(t *testing.T) {
	app := testApp()
	body := mcpRequest("tools/call", 5, map[string]interface{}{
		"name": "extract",
		"arguments": map[string]interface{}{
			"url":            "https://example.com",
			"extract_schema": map[string]interface{}{"type": "object"},
		},
	})

	_, result := postMCP(t, app, body)
	r := result["result"].(map[string]interface{})
	if r["isError"] != true {
		t.Fatal("expected isError=true when AI endpoint not configured")
	}
	content := r["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if text != "extraction service not configured" {
		t.Fatalf("unexpected error: %s", text)
	}
}

func TestMCPToolCallExtractBlockedOnFreePlan(t *testing.T) {
	cfg := Config{
		RedisURL:      "localhost:6379",
		APIKeys:       []string{"test-key-123"},
		Port:          "3000",
		AIEndpointURL: "https://ai.example.com/v1",
	}
	queue := NewQueueClient(cfg.RedisURL)
	app := NewApp(cfg, queue)

	body := mcpRequest("tools/call", 6, map[string]interface{}{
		"name": "extract",
		"arguments": map[string]interface{}{
			"url":            "https://example.com",
			"extract_schema": map[string]interface{}{"type": "object"},
		},
	})

	_, result := postMCP(t, app, body)
	r := result["result"].(map[string]interface{})
	if r["isError"] != true {
		t.Fatal("expected isError=true on free plan")
	}
}

func TestMCPToolCallUnknownTool(t *testing.T) {
	app := testApp()
	body := mcpRequest("tools/call", 7, map[string]interface{}{
		"name":      "unknown_tool",
		"arguments": map[string]interface{}{},
	})

	_, result := postMCP(t, app, body)
	if result["error"] == nil {
		t.Fatal("expected JSON-RPC error for unknown tool")
	}
	e := result["error"].(map[string]interface{})
	if e["code"].(float64) != -32602 {
		t.Fatalf("expected error code -32602, got %v", e["code"])
	}
}

func TestMCPMethodNotFound(t *testing.T) {
	app := testApp()
	body := mcpRequest("nonexistent/method", 8, nil)

	_, result := postMCP(t, app, body)
	if result["error"] == nil {
		t.Fatal("expected JSON-RPC error for unknown method")
	}
	e := result["error"].(map[string]interface{})
	if e["code"].(float64) != -32601 {
		t.Fatalf("expected error code -32601, got %v", e["code"])
	}
}

func TestMCPParseError(t *testing.T) {
	app := testApp()
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	if result["error"] == nil {
		t.Fatal("expected JSON-RPC error for parse error")
	}
	e := result["error"].(map[string]interface{})
	if e["code"].(float64) != -32700 {
		t.Fatalf("expected error code -32700, got %v", e["code"])
	}
}

func TestMCPNotification(t *testing.T) {
	app := testApp()
	body := mcpRequest("notifications/initialized", nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 for notification, got %d", resp.StatusCode)
	}
}

func TestMCPScrapeRejectsSSRF(t *testing.T) {
	app := testApp()
	body := mcpRequest("tools/call", 9, map[string]interface{}{
		"name": "scrape",
		"arguments": map[string]interface{}{
			"url": "http://169.254.169.254/latest/meta-data/",
		},
	})

	_, result := postMCP(t, app, body)
	r := result["result"].(map[string]interface{})
	if r["isError"] != true {
		t.Fatal("expected isError=true for SSRF attempt")
	}
}

func TestMCPScrapeInvalidFormat(t *testing.T) {
	app := testApp()
	body := mcpRequest("tools/call", 10, map[string]interface{}{
		"name": "scrape",
		"arguments": map[string]interface{}{
			"url":    "https://example.com",
			"format": "pdf",
		},
	})

	_, result := postMCP(t, app, body)
	r := result["result"].(map[string]interface{})
	if r["isError"] != true {
		t.Fatal("expected isError=true for invalid format")
	}
}

func TestMCPRequiresAuth(t *testing.T) {
	app := testApp()
	body := mcpRequest("tools/list", 1, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without API key, got %d", resp.StatusCode)
	}
}

func TestMCPToolCallExtractRequiresSchema(t *testing.T) {
	cfg := Config{
		RedisURL:      "localhost:6379",
		APIKeys:       []string{"test-key-123"},
		Port:          "3000",
		AIEndpointURL: "https://ai.example.com/v1",
	}
	queue := NewQueueClient(cfg.RedisURL)
	proPlan := Plan{Name: "pro", CanExtract: true, MaxRatePerMinute: 100}
	app := NewApp(cfg, queue, WithPlanLoader(&stubPlanLoader{plan: proPlan}))

	body := mcpRequest("tools/call", 11, map[string]interface{}{
		"name": "extract",
		"arguments": map[string]interface{}{
			"url": "https://example.com",
		},
	})

	_, result := postMCP(t, app, body)
	r := result["result"].(map[string]interface{})
	if r["isError"] != true {
		t.Fatal("expected isError=true when extract_schema missing")
	}
}
