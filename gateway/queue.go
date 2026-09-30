package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const jobKeyPrefix = "job_results"
const jobTTL = 1 * time.Hour
const streamMaxLen int64 = 10000

func jobKey(jobID string) string {
	return fmt.Sprintf("%s:%s", jobKeyPrefix, jobID)
}

func ownerKey(jobID string) string {
	return fmt.Sprintf("job_owner:%s", jobID)
}

type QueueClient struct {
	rdb *redis.Client
}

func NewQueueClient(addr string) *QueueClient {
	opts, err := redis.ParseURL(addr)
	if err != nil {
		opts = &redis.Options{Addr: addr}
	}
	rdb := redis.NewClient(opts)
	return &QueueClient{rdb: rdb}
}

func (q *QueueClient) Ping(ctx context.Context) error {
	return q.rdb.Ping(ctx).Err()
}

func (q *QueueClient) Close() error {
	return q.rdb.Close()
}

func (q *QueueClient) EnqueueScrape(ctx context.Context, req ScrapeRequest, owner string) (string, error) {
	jobID := uuid.New().String()

	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal scrape request: %w", err)
	}

	err = q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "scrape_jobs",
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]interface{}{
			"job_id":  jobID,
			"url":     req.URL,
			"format":  req.Format,
			"payload": string(payload),
		},
	}).Err()
	if err != nil {
		return "", fmt.Errorf("enqueue scrape job: %w", err)
	}

	pipe := q.rdb.Pipeline()
	pipe.Set(ctx, jobKey(jobID), mustMarshal(JobStatus{
		JobID:  jobID,
		Status: "queued",
	}), jobTTL)
	pipe.Set(ctx, ownerKey(jobID), owner, jobTTL)
	if _, err = pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("set initial job status: %w", err)
	}

	return jobID, nil
}

func (q *QueueClient) EnqueueCrawl(ctx context.Context, req CrawlRequest, owner string) (string, error) {
	jobID := uuid.New().String()

	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal crawl request: %w", err)
	}

	err = q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "crawl_jobs",
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]interface{}{
			"job_id":  jobID,
			"url":     req.URL,
			"payload": string(payload),
		},
	}).Err()
	if err != nil {
		return "", fmt.Errorf("enqueue crawl job: %w", err)
	}

	pipe := q.rdb.Pipeline()
	pipe.Set(ctx, jobKey(jobID), mustMarshal(JobStatus{
		JobID:  jobID,
		Status: "queued",
	}), jobTTL)
	pipe.Set(ctx, ownerKey(jobID), owner, jobTTL)
	if _, err = pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("set initial job status: %w", err)
	}

	return jobID, nil
}

func (q *QueueClient) GetJobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	val, err := q.rdb.Get(ctx, jobKey(jobID)).Result()
	if err == redis.Nil {
		return JobStatus{}, fmt.Errorf("job not found: %s", jobID)
	}
	if err != nil {
		return JobStatus{}, fmt.Errorf("get job status: %w", err)
	}

	var status JobStatus
	if err := json.Unmarshal([]byte(val), &status); err != nil {
		return JobStatus{}, fmt.Errorf("unmarshal job status: %w", err)
	}

	owner, err := q.rdb.Get(ctx, ownerKey(jobID)).Result()
	if err == nil {
		status.Owner = owner
	}

	return status, nil
}

func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return string(b)
}
