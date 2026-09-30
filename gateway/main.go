package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New(fiber.Config{
		AppName: "Cheetah API Gateway",
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	v1 := app.Group("/v1")
	v1.Post("/scrape", handleScrape)
	v1.Post("/crawl", handleCrawl)
	v1.Post("/extract", handleExtract)

	log.Fatal(app.Listen(":3000"))
}

func handleScrape(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "not_implemented"})
}

func handleCrawl(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "not_implemented"})
}

func handleExtract(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "not_implemented"})
}
