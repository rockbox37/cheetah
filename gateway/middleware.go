package main

import (
	"crypto/subtle"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

func (tb *tokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

type rateLimiterStore struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

func newRateLimiterStore() *rateLimiterStore {
	return &rateLimiterStore{
		buckets: make(map[string]*tokenBucket),
	}
}

func (s *rateLimiterStore) getBucket(key string) *tokenBucket {
	s.mu.Lock()
	defer s.mu.Unlock()

	if b, ok := s.buckets[key]; ok {
		return b
	}

	b := &tokenBucket{
		tokens:     100,
		maxTokens:  100,
		refillRate: 100.0 / 60.0,
		lastRefill: time.Now(),
	}
	s.buckets[key] = b
	return b
}

func RateLimiter() fiber.Handler {
	store := newRateLimiterStore()

	ticker := time.NewTicker(60 * time.Second)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			store.mu.Lock()
			cutoff := time.Now().Add(-5 * time.Minute)
			for k, b := range store.buckets {
				if b.lastRefill.Before(cutoff) {
					delete(store.buckets, k)
				}
			}
			store.mu.Unlock()
		}
	}()

	return func(c *fiber.Ctx) error {
		key := c.Get("X-API-Key")
		if key == "" {
			key = c.IP()
		}

		bucket := store.getBucket(key)

		store.mu.Lock()
		allowed := bucket.allow()
		remaining := int(bucket.tokens)
		store.mu.Unlock()

		c.Set("X-RateLimit-Limit", "100")
		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if !allowed {
			c.Set("Retry-After", "60")
			return c.Status(fiber.StatusTooManyRequests).JSON(ErrorResponse{
				Success: false,
				Error:   "rate limit exceeded",
			})
		}

		return c.Next()
	}
}

// APIKeyAuth validates the X-API-Key header against the provided keys
// using constant-time comparison to prevent timing attacks.
func APIKeyAuth(validKeys []string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := c.Get("X-API-Key")
		if key == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Success: false,
				Error:   "missing API key",
			})
		}

		valid := false
		for _, vk := range validKeys {
			if subtle.ConstantTimeCompare([]byte(key), []byte(vk)) == 1 {
				valid = true
				// Continue iterating to maintain constant time across all keys.
			}
		}

		if !valid {
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Success: false,
				Error:   "invalid API key",
			})
		}

		c.Locals("api_key", key)
		return c.Next()
	}
}

// RequestLogger logs each request with method, path, status, duration,
// client IP, and a truncated API key for audit purposes.
func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		duration := time.Since(start)

		apiKey, _ := c.Locals("api_key").(string)
		keyDisplay := "-"
		if apiKey != "" && apiKey != "anonymous" {
			if len(apiKey) > 8 {
				keyDisplay = apiKey[:8] + "..."
			} else {
				keyDisplay = apiKey + "..."
			}
		} else if apiKey == "anonymous" {
			keyDisplay = "anonymous"
		}

		log.Printf("%s %s %d %s ip=%s key=%s",
			c.Method(),
			c.Path(),
			c.Response().StatusCode(),
			duration,
			c.IP(),
			keyDisplay,
		)

		return err
	}
}
