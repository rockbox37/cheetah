package main

import (
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

func APIKeyAuth(validKeys map[string]bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := c.Get("X-API-Key")
		if key == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Success: false,
				Error:   "missing API key",
			})
		}

		if !validKeys[key] {
			return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{
				Success: false,
				Error:   "invalid API key",
			})
		}

		return c.Next()
	}
}

func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		duration := time.Since(start)
		log.Printf("%s %s %d %s",
			c.Method(),
			c.Path(),
			c.Response().StatusCode(),
			duration,
		)

		return err
	}
}
