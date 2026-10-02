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
	if resp.Header.Get("X-RateLimit-Limit") != "10" {
		t.Fatalf("expected X-RateLimit-Limit=10 (default fallback), got %s", resp.Header.Get("X-RateLimit-Limit"))
	}
}

func TestRateLimiterExhaustion(t *testing.T) {
	app := fiber.New()
	app.Use(RateLimiter())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	for i := 0; i < 11; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if i == 10 && resp.StatusCode != 429 {
			t.Fatalf("expected 429 on request 11, got %d", resp.StatusCode)
		}
	}
}

func TestRateLimiterRespectsplan(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("plan", Plan{MaxRatePerMinute: 100})
		return c.Next()
	})
	app.Use(RateLimiter())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("X-RateLimit-Limit") != "100" {
		t.Fatalf("expected X-RateLimit-Limit=100 from plan, got %s", resp.Header.Get("X-RateLimit-Limit"))
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

func TestOwnerHashMiddleware(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("api_key", "test-key-123")
		return c.Next()
	})
	app.Use(OwnerHashMiddleware())
	app.Get("/", func(c *fiber.Ctx) error {
		hash, _ := c.Locals("owner_hash").(string)
		if hash == "" {
			return fiber.NewError(500, "owner_hash not set")
		}
		if len(hash) != 64 {
			return fiber.NewError(500, "expected 64-char hex hash")
		}
		if hash == "test-key-123" {
			return fiber.NewError(500, "owner_hash should be hashed, not raw key")
		}
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
}

func TestOwnerHashMiddlewareAnonymous(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("api_key", "anonymous")
		return c.Next()
	})
	app.Use(OwnerHashMiddleware())
	app.Get("/", func(c *fiber.Ctx) error {
		hash, _ := c.Locals("owner_hash").(string)
		if hash != "" {
			return fiber.NewError(500, "anonymous should not get an owner_hash")
		}
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
}

func TestAnonymizeIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.42", "192.168.1.0"},
		{"10.0.0.1", "10.0.0.0"},
		{"0.0.0.0", "0.0.0.0"},
		{"::1", "::0"},
		{"2001:db8::1", "2001:db8::0"},
		{"weird", "redacted"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := anonymizeIP(tt.input)
			if got != tt.want {
				t.Fatalf("anonymizeIP(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
