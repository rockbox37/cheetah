package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func anonymizeIP(ip string) string {
	if parts := strings.Split(ip, "."); len(parts) == 4 {
		return parts[0] + "." + parts[1] + "." + parts[2] + ".0"
	}
	if i := strings.LastIndex(ip, ":"); i >= 0 {
		return ip[:i] + ":0"
	}
	return "redacted"
}

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

type rateLimiterStore struct {
	mu          sync.Mutex
	buckets     map[string]*tokenBucket
	lastCleanup time.Time
}

func newRateLimiterStore() *rateLimiterStore {
	return &rateLimiterStore{
		buckets:     make(map[string]*tokenBucket),
		lastCleanup: time.Now(),
	}
}

func (s *rateLimiterStore) allowRequest(key string, maxRate int) (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	if now.Sub(s.lastCleanup) > 60*time.Second {
		cutoff := now.Add(-5 * time.Minute)
		for k, b := range s.buckets {
			if b.lastRefill.Before(cutoff) {
				delete(s.buckets, k)
			}
		}
		s.lastCleanup = now
	}

	rate := float64(maxRate)
	b, ok := s.buckets[key]
	if !ok {
		b = &tokenBucket{
			tokens:     rate,
			maxTokens:  rate,
			refillRate: rate / 60.0,
			lastRefill: now,
		}
		s.buckets[key] = b
	} else if b.maxTokens != rate {
		b.maxTokens = rate
		b.refillRate = rate / 60.0
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true, int(b.tokens)
	}
	return false, 0
}

func OwnerHashMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		key, _ := c.Locals("api_key").(string)
		if key != "" && key != "anonymous" {
			c.Locals("owner_hash", hashAPIKey(key))
		}
		return c.Next()
	}
}

func RateLimiter() fiber.Handler {
	store := newRateLimiterStore()

	return func(c *fiber.Ctx) error {
		ownerHash, _ := c.Locals("owner_hash").(string)
		if ownerHash == "" {
			ownerHash = c.IP()
		}

		plan, _ := c.Locals("plan").(Plan)
		maxRate := plan.MaxRatePerMinute
		if maxRate <= 0 {
			maxRate = 10
		}

		allowed, remaining := store.allowRequest(ownerHash, maxRate)

		c.Set("X-RateLimit-Limit", strconv.Itoa(maxRate))
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

func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		duration := time.Since(start)

		ownerHash, _ := c.Locals("owner_hash").(string)
		keyDisplay := "-"
		if ownerHash != "" {
			keyDisplay = ownerHash[:8] + "..."
		} else {
			apiKey, _ := c.Locals("api_key").(string)
			if apiKey == "anonymous" {
				keyDisplay = "anonymous"
			}
		}

		log.Printf("%s %s %d %s ip=%s key=%s",
			c.Method(),
			c.Path(),
			c.Response().StatusCode(),
			duration,
			anonymizeIP(c.IP()),
			keyDisplay,
		)

		return err
	}
}

func PlanMiddleware(loader PlanLoader) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if trusted, _ := c.Locals("plan_trusted").(bool); trusted {
			return c.Next()
		}
		ownerHash, _ := c.Locals("owner_hash").(string)
		plan, err := loader.LoadPlan(c.Context(), ownerHash)
		if err != nil {
			log.Printf("plan loader: falling back to free plan")
			plan = FreePlan
		}
		c.Locals("plan", plan)
		return c.Next()
	}
}
