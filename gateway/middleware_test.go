package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRateLimiterAllows(t *testing.T) {
	app := fiber.New()
	app.Use(RateLimiter())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-RateLimit-Limit") != "100" {
		t.Fatal("missing X-RateLimit-Limit header")
	}
}

func TestRateLimiterExhaustion(t *testing.T) {
	app := fiber.New()
	app.Use(RateLimiter())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	for i := 0; i < 101; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if i == 100 && resp.StatusCode != 429 {
			t.Fatalf("expected 429 on request 101, got %d", resp.StatusCode)
		}
	}
}

func TestAPIKeyAuthRejectsEmpty(t *testing.T) {
	keys := map[string]bool{"valid-key": true}
	app := fiber.New()
	app.Use(APIKeyAuth(keys))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAPIKeyAuthAcceptsValid(t *testing.T) {
	keys := map[string]bool{"valid-key": true}
	app := fiber.New()
	app.Use(APIKeyAuth(keys))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "valid-key")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
