package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthLiveness(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	status := result["status"]
	if status != "healthy" && status != "unhealthy" {
		t.Fatalf("unexpected liveness status: %s", status)
	}

	if status == "unhealthy" && resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unhealthy should return 503, got %d", resp.StatusCode)
	}
	if status == "healthy" && resp.StatusCode != http.StatusOK {
		t.Fatalf("healthy should return 200, got %d", resp.StatusCode)
	}
}

func TestHealthLivenessNoAuth(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("liveness endpoint should not require auth")
	}
}

func TestHealthDiagnosticRequiresAuth(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/v1/health should require auth, got %d", resp.StatusCode)
	}
}

func TestHealthDiagnosticWithAuth(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("X-API-Key", "test-key-123")
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

	if result.Redis.Connected && result.Redis.Error != "" {
		t.Fatal("connected redis should not have error")
	}
}

func TestHealthDiagnosticMasksError(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("X-API-Key", "test-key-123")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(resp.Body)
	var result HealthResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if !result.Redis.Connected && result.Redis.Error != "redis connection failed" {
		t.Fatalf("expected masked error, got %q", result.Redis.Error)
	}
}
