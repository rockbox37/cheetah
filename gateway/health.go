package main

import (
	"context"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

const healthTimeout = 3 * time.Second

var monitoredStreams = []struct {
	stream string
	tier   string
}{
	{"scrape_jobs", "fast"},
	{"browser_jobs", "browser"},
	{"stealth_jobs", "stealth"},
	{"crawl_jobs", "crawl"},
}

type HealthResponse struct {
	Status  string                  `json:"status"`
	Redis   RedisHealth             `json:"redis"`
	Queues  map[string]QueueHealth  `json:"queues,omitempty"`
	Workers map[string]WorkerHealth `json:"workers,omitempty"`
}

type RedisHealth struct {
	Connected bool   `json:"connected"`
	LatencyMs *int64 `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

type QueueHealth struct {
	Length          int64  `json:"length"`
	Consumers       int    `json:"consumers"`
	OldestPendingMs *int64 `json:"oldest_pending_ms,omitempty"`
}

type WorkerHealth struct {
	ActiveConsumers int    `json:"active_consumers"`
	IdleMs          *int64 `json:"idle_ms,omitempty"`
}

func (q *QueueClient) PingLatency(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	err := q.rdb.Ping(ctx).Err()
	return time.Since(start), err
}

func (q *QueueClient) StreamInfo(ctx context.Context, stream string) (length int64, groups []redis.XInfoGroup, err error) {
	length, err = q.rdb.XLen(ctx, stream).Result()
	if err != nil {
		return 0, nil, err
	}

	groups, err = q.rdb.XInfoGroups(ctx, stream).Result()
	if err != nil {
		if isNoStreamErr(err) {
			return length, nil, nil
		}
		return length, nil, err
	}

	return length, groups, nil
}

func (q *QueueClient) OldestPending(ctx context.Context, stream, group string) (*int64, error) {
	pending, err := q.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stream,
		Group:  group,
		Start:  "-",
		End:    "+",
		Count:  1,
	}).Result()
	if err != nil || len(pending) == 0 {
		return nil, err
	}
	ms := pending[0].Idle.Milliseconds()
	return &ms, nil
}

func isNoStreamErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == "ERR no such key" || msg == "ERR no such key or consumer group"
}

func handleHealthLiveness(queue *QueueClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
		defer cancel()

		_, pingErr := queue.PingLatency(ctx)
		status := "healthy"
		code := fiber.StatusOK
		if pingErr != nil {
			status = "unhealthy"
			code = fiber.StatusServiceUnavailable
		}

		return c.Status(code).JSON(fiber.Map{"status": status})
	}
}

func handleHealthDiagnostic(queue *QueueClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
		defer cancel()

		latency, pingErr := queue.PingLatency(ctx)
		redisHealth := RedisHealth{Connected: pingErr == nil}
		if pingErr == nil {
			ms := latency.Milliseconds()
			redisHealth.LatencyMs = &ms
		} else {
			redisHealth.Error = "redis connection failed"
			log.Printf("health check: redis ping failed: %v", pingErr)
		}

		queues := make(map[string]QueueHealth)
		workers := make(map[string]WorkerHealth)

		if pingErr == nil {
			for _, s := range monitoredStreams {
				length, groups, err := queue.StreamInfo(ctx, s.stream)
				qh := QueueHealth{Length: length}
				wh := WorkerHealth{}

				if err == nil {
					for _, g := range groups {
						n := int(g.Consumers)
						qh.Consumers += n
						wh.ActiveConsumers += n

						oldest, pendErr := queue.OldestPending(ctx, s.stream, g.Name)
						if pendErr == nil && oldest != nil {
							if qh.OldestPendingMs == nil || *oldest > *qh.OldestPendingMs {
								qh.OldestPendingMs = oldest
							}
						}
					}

					if wh.ActiveConsumers > 0 {
						idleMs := queue.minConsumerIdle(ctx, s.stream, groups)
						if idleMs != nil {
							wh.IdleMs = idleMs
						}
					}
				}

				queues[s.stream] = qh
				workers[s.tier] = wh
			}
		}

		overall := computeOverallStatus(redisHealth, workers)

		resp := HealthResponse{
			Status:  overall,
			Redis:   redisHealth,
			Queues:  queues,
			Workers: workers,
		}

		code := fiber.StatusOK
		if overall == "unhealthy" {
			code = fiber.StatusServiceUnavailable
		}

		return c.Status(code).JSON(resp)
	}
}

func (q *QueueClient) minConsumerIdle(ctx context.Context, stream string, groups []redis.XInfoGroup) *int64 {
	var globalMin int64 = -1
	for _, g := range groups {
		consumers, err := q.rdb.XInfoConsumers(ctx, stream, g.Name).Result()
		if err != nil {
			continue
		}
		for _, con := range consumers {
			idle := con.Idle.Milliseconds()
			if globalMin < 0 || idle < globalMin {
				globalMin = idle
			}
		}
	}
	if globalMin >= 0 {
		return &globalMin
	}
	return nil
}

func computeOverallStatus(r RedisHealth, workers map[string]WorkerHealth) string {
	if !r.Connected {
		return "unhealthy"
	}

	fastWorker, hasFast := workers["fast"]
	if !hasFast || fastWorker.ActiveConsumers == 0 {
		return "degraded"
	}

	return "healthy"
}
