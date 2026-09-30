package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type QueueClient struct {
	rdb *redis.Client
}

func NewQueueClient(addr string) *QueueClient {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &QueueClient{rdb: rdb}
}

func (q *QueueClient) Ping(ctx context.Context) error {
	return q.rdb.Ping(ctx).Err()
}

func (q *QueueClient) Close() error {
	return q.rdb.Close()
}

func (q *QueueClient) EnqueueScrape(ctx context.Context, req ScrapeRequest) (string, error) {
	jobID := uuid.New().String()

	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal scrape request: %w", err)
	}

	err = q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "scrape_jobs",
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

	err = q.rdb.HSet(ctx, "job_results", jobID, mustMarshal(JobStatus{
		JobID:  jobID,
		Status: "queued",
	})).Err()
	if err != nil {
		return "", fmt.Errorf("set initial job status: %w", err)
	}

	return jobID, nil
}

func (q *QueueClient) EnqueueCrawl(ctx context.Context, req CrawlRequest) (string, error) {
	jobID := uuid.New().String()

	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal crawl request: %w", err)
	}

	err = q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "crawl_jobs",
		Values: map[string]interface{}{
			"job_id":  jobID,
			"url":     req.URL,
			"payload": string(payload),
		},
	}).Err()
	if err != nil {
		return "", fmt.Errorf("enqueue crawl job: %w", err)
	}

	err = q.rdb.HSet(ctx, "job_results", jobID, mustMarshal(JobStatus{
		JobID:  jobID,
		Status: "queued",
	})).Err()
	if err != nil {
		return "", fmt.Errorf("set initial job status: %w", err)
	}

	return jobID, nil
}

func (q *QueueClient) GetJobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	val, err := q.rdb.HGet(ctx, "job_results", jobID).Result()
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

	return status, nil
}

func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return string(b)
}
