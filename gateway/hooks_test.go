package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestFreePlanLoaderReturnsFreePlan(t *testing.T) {
	loader := FreePlanLoader{}
	plan, err := loader.LoadPlan(context.Background(), "any-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Name != "free" {
		t.Fatalf("expected plan name 'free', got %q", plan.Name)
	}
	if plan.MaxCreditsPerMonth != 500 {
		t.Fatalf("expected 500 credits, got %d", plan.MaxCreditsPerMonth)
	}
	if len(plan.AllowedTiers) != 1 || plan.AllowedTiers[0] != "fast" {
		t.Fatalf("expected [fast], got %v", plan.AllowedTiers)
	}
	if plan.CanExtract {
		t.Fatal("free plan should not allow extraction")
	}
	if plan.MaxCrawlPages != 10 {
		t.Fatalf("expected max_crawl_pages 10, got %d", plan.MaxCrawlPages)
	}
}

func TestNoOpUsageReporterDoesNotPanic(t *testing.T) {
	reporter := NoOpUsageReporter{}
	reporter.ReportUsage(context.Background(), "hash", 5, 1024, true)
	reporter.ReportUsage(context.Background(), "hash", 0, 0, false)
}

type stubPlanLoader struct {
	plan Plan
	err  error
}

func (s *stubPlanLoader) LoadPlan(_ context.Context, _ string) (Plan, error) {
	return s.plan, s.err
}

func planTestApp(loader PlanLoader) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("api_key", "test-key")
		return c.Next()
	})
	app.Use(PlanMiddleware(loader))
	app.Get("/check", func(c *fiber.Ctx) error {
		p, ok := c.Locals("plan").(Plan)
		if !ok {
			return fiber.NewError(500, "plan not in context")
		}
		return c.JSON(p)
	})
	return app
}

var proPlan = Plan{
	Name:               "pro",
	MaxCreditsPerMonth: 50000,
	AllowedTiers:       []string{"fast", "browser", "stealth"},
	CanExtract:         true,
	MaxRatePerMinute:      100,
	MaxConcurrency:     50,
	MaxCrawlPages:      500,
	ResultRetentionHours:   168,
	CanWebhook:         true,
	CanOverage:         true,
	MaxBandwidthGB:     50,
}

func TestPlanMiddlewareLoadsPlan(t *testing.T) {
	app := planTestApp(&stubPlanLoader{plan: proPlan})

	req := httptest.NewRequest(http.MethodGet, "/check", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var got Plan
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Name != "pro" {
		t.Fatalf("expected plan 'pro', got %q", got.Name)
	}
	if got.MaxCreditsPerMonth != 50000 {
		t.Fatalf("expected 50000 credits, got %d", got.MaxCreditsPerMonth)
	}
	if !got.CanExtract {
		t.Fatal("pro plan should allow extraction")
	}
}

func TestPlanMiddlewareFallsBackOnError(t *testing.T) {
	app := planTestApp(&stubPlanLoader{err: errors.New("db down")})

	req := httptest.NewRequest(http.MethodGet, "/check", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got Plan
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Name != "free" {
		t.Fatalf("expected fallback to 'free' plan, got %q", got.Name)
	}
}

func TestCustomPlanLoaderInNewApp(t *testing.T) {
	starterPlan := Plan{
		Name:               "starter",
		MaxCreditsPerMonth: 10000,
		AllowedTiers:       []string{"fast", "browser"},
		MaxRatePerMinute:      50,
		MaxConcurrency:     10,
		MaxCrawlPages:      100,
		ResultRetentionHours:   24,
		MaxBandwidthGB:     10,
		CanOverage:         true,
	}

	cfg := Config{
		RedisURL: "localhost:6379",
		APIKeys:  []string{"test-key-123"},
		Port:     "3000",
	}
	queue := NewQueueClient(cfg.RedisURL)
	app := NewApp(cfg, queue, WithPlanLoader(&stubPlanLoader{plan: starterPlan}))

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("X-API-Key", "test-key-123")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("should not get 401 with valid key")
	}
}

func TestDefaultAppUsesFreePlan(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("X-API-Key", "test-key-123")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("should not get 401 with valid key")
	}
}

func TestPlanResultTTL(t *testing.T) {
	cases := []struct {
		hours int
		want  time.Duration
	}{
		{-1, 0}, {0, 0}, {24, 24 * time.Hour},
		{maxResultRetentionHours, maxResultRetentionHours * time.Hour},
		{1 << 40, maxResultRetentionHours * time.Hour},
	}
	for _, c := range cases {
		if got := planResultTTL(Plan{ResultRetentionHours: c.hours}); got != c.want {
			t.Errorf("hours=%d: got %v, want %v", c.hours, got, c.want)
		}
	}
}

func TestCrawlRequestResultTTLSurvivesPayload(t *testing.T) {
	b, err := json.Marshal(CrawlRequest{URL: "https://example.com", ResultTTL: 48 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	var out CrawlRequest
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ResultTTL != 48*time.Hour {
		t.Errorf("ResultTTL lost in payload round-trip: %v", out.ResultTTL)
	}
}
