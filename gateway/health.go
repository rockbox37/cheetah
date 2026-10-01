package main

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

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
	Status  string                       `json:"status"`
	Redis   RedisHealth                  `json:"redis"`
	Queues  map[string]QueueHealth       `json:"queues"`
	Workers map[string]WorkerHealth      `json:"workers"`
}

type RedisHealth struct {
	Connected bool   `json:"connected"`
	LatencyMs *int64 `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type QueueHealth struct {
	Pending        int64  `json:"pending"`
	Consumers      int    `json:"consumers"`
	OldestPendingMs *int64 `json:"oldest_pending_ms"`
}

type WorkerHealth struct {
	ActiveConsumers int    `json:"active_consumers"`
	IdleMs          *int64 `json:"idle_ms"`
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

func handleHealthDiagnostic(queue *QueueClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := c.Context()

		latency, pingErr := queue.PingLatency(ctx)
		redisHealth := RedisHealth{Connected: pingErr == nil}
		if pingErr == nil {
			ms := latency.Milliseconds()
			redisHealth.LatencyMs = &ms
		} else {
			redisHealth.Error = pingErr.Error()
		}

		queues := make(map[string]QueueHealth)
		workers := make(map[string]WorkerHealth)

		if pingErr == nil {
			for _, s := range monitoredStreams {
				length, groups, err := queue.StreamInfo(ctx, s.stream)
				qh := QueueHealth{Pending: length}
				wh := WorkerHealth{}

				if err == nil {
					for _, g := range groups {
						qh.Consumers += int(g.Consumers)
						wh.ActiveConsumers += int(g.Consumers)

						oldest, pendErr := queue.OldestPending(ctx, s.stream, g.Name)
						if pendErr == nil && oldest != nil {
							if qh.OldestPendingMs == nil || *oldest > *qh.OldestPendingMs {
								qh.OldestPendingMs = oldest
							}
						}
					}

					if wh.ActiveConsumers > 0 {
						lastSeen, lastErr := queue.lastConsumerActivity(ctx, s.stream, groups)
						if lastErr == nil && lastSeen != nil {
							wh.IdleMs = lastSeen
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

func (q *QueueClient) lastConsumerActivity(ctx context.Context, stream string, groups []redis.XInfoGroup) (*int64, error) {
	for _, g := range groups {
		consumers, err := q.rdb.XInfoConsumers(ctx, stream, g.Name).Result()
		if err != nil {
			continue
		}
		var minIdle int64 = -1
		for _, con := range consumers {
			idle := con.Idle.Milliseconds()
			if minIdle < 0 || idle < minIdle {
				minIdle = idle
			}
		}
		if minIdle >= 0 {
			return &minIdle, nil
		}
	}
	return nil, nil
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
