package main

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type Config struct {
	RedisURL string
	APIKeys  map[string]bool
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

	apiKeys := make(map[string]bool)
	keysEnv := os.Getenv("CHEETAH_API_KEYS")
	if keysEnv != "" {
		for _, key := range strings.Split(keysEnv, ",") {
			k := strings.TrimSpace(key)
			if k != "" {
				apiKeys[k] = true
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

	app.Get("/health", handleHealth(queue))

	v1 := app.Group("/v1")
	if len(cfg.APIKeys) > 0 {
		v1.Use(APIKeyAuth(cfg.APIKeys))
	}
	v1.Use(RateLimiter())

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

func handleHealth(queue *QueueClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		status := "ok"
		redisOK := true

		if err := queue.Ping(c.Context()); err != nil {
			status = "degraded"
			redisOK = false
		}

		return c.JSON(fiber.Map{
			"status": status,
			"redis":  redisOK,
		})
	}
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

		if _, err := url.ParseRequestURI(req.URL); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "invalid url",
			})
		}

		if req.Format == "" {
			req.Format = "markdown"
		}
		if req.Format != "markdown" && req.Format != "html" && req.Format != "text" {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "format must be markdown, html, or text",
			})
		}

		jobID, err := queue.EnqueueScrape(c.Context(), req)
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

		if _, err := url.ParseRequestURI(req.URL); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "invalid url",
			})
		}

		if req.MaxPages <= 0 {
			req.MaxPages = 10
		}

		if req.Format == "" {
			req.Format = "markdown"
		}

		jobID, err := queue.EnqueueCrawl(c.Context(), req)
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

		if _, err := url.ParseRequestURI(req.URL); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
				Success: false,
				Error:   "invalid url",
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
		}

		jobID, err := queue.EnqueueScrape(c.Context(), scrapeReq)
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
			if strings.Contains(err.Error(), "job not found") {
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

		return c.JSON(status)
	}
}
