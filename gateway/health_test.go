package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(resp.Body)
	var result HealthResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if result.Status != "healthy" && result.Status != "degraded" && result.Status != "unhealthy" {
		t.Fatalf("unexpected status: %s", result.Status)
	}

	if result.Status == "unhealthy" {
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("unhealthy should return 503, got %d", resp.StatusCode)
		}
	} else {
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("healthy/degraded should return 200, got %d", resp.StatusCode)
		}
	}

	if result.Queues == nil {
		if result.Redis.Connected {
			t.Fatal("expected queues map when redis is connected")
		}
	}
	if result.Workers == nil {
		if result.Redis.Connected {
			t.Fatal("expected workers map when redis is connected")
		}
	}
}

func TestHealthV1Alias(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(resp.Body)
	var result HealthResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("/v1/health should return valid JSON: %v", err)
	}
}

func TestHealthNoAuth(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("health endpoint should not require auth")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 200 or 503, got %d", resp.StatusCode)
	}
}
