package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type Config struct {
	RedisURL string
	APIKeys  []string
	Port     string
}

func LoadConfig() Config {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	var apiKeys []string
	keysEnv := os.Getenv("CHEETAH_API_KEYS")
	if keysEnv != "" {
		for _, key := range strings.Split(keysEnv, ",") {
			k := strings.TrimSpace(key)
			if k != "" {
				apiKeys = append(apiKeys, k)
			}
		}
	}

	return Config{
		RedisURL: redisURL,
		APIKeys:  apiKeys,
		Port:     port,
	}
}

func main() {
	cfg := LoadConfig()
	queue := NewQueueClient(cfg.RedisURL)
	defer queue.Close()

	app := NewApp(cfg, queue)
	log.Fatal(app.Listen(":" + cfg.Port))
}

func NewApp(cfg Config, queue *QueueClient) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "Cheetah API Gateway",
		ErrorHandler: customErrorHandler,
	})

	app.Use(RequestLogger())

	app.Get("/health", handleHealthLiveness(queue))

	v1 := app.Group("/v1")
	if len(cfg.APIKeys) > 0 {
		v1.Use(APIKeyAuth(cfg.APIKeys))
	} else {
		// When auth is disabled, tag every request as anonymous
		// so the IDOR owner check still works.
		v1.Use(func(c *fiber.Ctx) error {
			c.Locals("api_key", "anonymous")
			return c.Next()
		})
	}
	v1.Use(RateLimiter())

	v1.Get("/health", handleHealthDiagnostic(queue))
	v1.Post("/scrape", handleScrape(queue))
	v1.Post("/crawl", handleCrawl(queue))
	v1.Post("/extract", handleExtract(queue))
	v1.Get("/crawl/:id", handleCrawlStatus(queue))

	return app
}

func customErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	return c.Status(code).JSON(ErrorResponse{
		Success: false,
		Error:   err.Error(),
	})
}

func callerOwner(c *fiber.Ctx) string {
	key, _ := c.Locals("api_key").(string)
	if key == "" || key == "anonymous" {
		return "anonymous"
	}
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// validateFormat checks that format is one of the accepted values.
func validateFormat(format string) bool {
	return format == "markdown" || format == "html" || format == "text"
}

func handleScrape(queue *QueueClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req ScrapeRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "invalid request body",
			})
		}

		if req.URL == "" {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "url is required",
			})
		}

		resolvedIP, err := ValidateScrapeURL(req.URL)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   err.Error(),
			})
		}
		req.ResolvedIP = resolvedIP

		if req.Format == "" {
			req.Format = "markdown"
		}
		if !validateFormat(req.Format) {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "format must be markdown, html, or text",
			})
		}

		owner := callerOwner(c)
		jobID, err := queue.EnqueueScrape(c.Context(), req, owner)
		if err != nil {
			log.Printf("enqueue scrape error: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{
				Success: false,
				Error:   "failed to enqueue job",
			})
		}

		return c.Status(fiber.StatusAccepted).JSON(EnqueueResponse{
			Success: true,
			JobID:   jobID,
		})
	}
}

func handleCrawl(queue *QueueClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req CrawlRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "invalid request body",
			})
		}

		if req.URL == "" {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "url is required",
			})
		}

		resolvedIP, err := ValidateScrapeURL(req.URL)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   err.Error(),
			})
		}
		req.ResolvedIP = resolvedIP

		if req.MaxPages <= 0 {
			req.MaxPages = 10
		}
		if req.MaxPages > 1000 {
			req.MaxPages = 1000
		}

		if req.Format == "" {
			req.Format = "markdown"
		}
		if !validateFormat(req.Format) {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "format must be markdown, html, or text",
			})
		}

		owner := callerOwner(c)
		jobID, err := queue.EnqueueCrawl(c.Context(), req, owner)
		if err != nil {
			log.Printf("enqueue crawl error: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{
				Success: false,
				Error:   "failed to enqueue job",
			})
		}

		return c.Status(fiber.StatusAccepted).JSON(CrawlResponse{
			Success: true,
			JobID:   jobID,
			Status:  "queued",
		})
	}
}

func handleExtract(queue *QueueClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req ExtractRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "invalid request body",
			})
		}

		if req.URL == "" {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "url is required",
			})
		}

		resolvedIP, err := ValidateScrapeURL(req.URL)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   err.Error(),
			})
		}

		if req.ExtractSchema == nil || len(req.ExtractSchema) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "extract_schema is required",
			})
		}

		if !json.Valid(req.ExtractSchema) {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "extract_schema must be valid JSON",
			})
		}

		schema := req.ExtractSchema
		scrapeReq := ScrapeRequest{
			URL:           req.URL,
			Format:        "markdown",
			ExtractSchema: (*json.RawMessage)(&schema),
			WaitFor:       req.WaitFor,
			Timeout:       req.Timeout,
			Wait:          req.Wait,
			ResolvedIP:    resolvedIP,
		}

		owner := callerOwner(c)
		jobID, err := queue.EnqueueScrape(c.Context(), scrapeReq, owner)
		if err != nil {
			log.Printf("enqueue extract error: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{
				Success: false,
				Error:   "failed to enqueue job",
			})
		}

		return c.Status(fiber.StatusAccepted).JSON(EnqueueResponse{
			Success: true,
			JobID:   jobID,
		})
	}
}

func handleCrawlStatus(queue *QueueClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		jobID := c.Params("id")
		if jobID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "job id is required",
			})
		}

		status, err := queue.GetJobStatus(c.Context(), jobID)
		if err != nil {
			if errors.Is(err, ErrJobNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(ErrorResponse{
					Success: false,
					Error:   "job not found",
				})
			}
			log.Printf("get job status error: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{
				Success: false,
				Error:   "failed to get job status",
			})
		}

		// IDOR check: verify the caller owns this job.
		caller := callerOwner(c)
		if subtle.ConstantTimeCompare([]byte(status.Owner), []byte(caller)) != 1 {
			return c.Status(fiber.StatusNotFound).JSON(ErrorResponse{
				Success: false,
				Error:   "job not found",
			})
		}

		status.Owner = ""
		return c.JSON(status)
	}
}
