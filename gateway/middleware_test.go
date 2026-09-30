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
	keys := []string{"valid-key"}
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
	keys := []string{"valid-key"}
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

func TestAPIKeyAuthRejectsInvalid(t *testing.T) {
	keys := []string{"valid-key"}
	app := fiber.New()
	app.Use(APIKeyAuth(keys))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 for invalid key, got %d", resp.StatusCode)
	}
}

func TestAPIKeyAuthSetsLocal(t *testing.T) {
	keys := []string{"my-api-key-12345"}
	app := fiber.New()
	app.Use(APIKeyAuth(keys))
	app.Get("/", func(c *fiber.Ctx) error {
		apiKey, _ := c.Locals("api_key").(string)
		if apiKey != "my-api-key-12345" {
			return fiber.NewError(500, "api_key local not set correctly")
		}
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "my-api-key-12345")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
