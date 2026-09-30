package main

import (
	"encoding/json"
	"testing"
)

func TestMustMarshal(t *testing.T) {
	status := JobStatus{
		JobID:  "test-123",
		Status: "queued",
	}
	result := mustMarshal(status)
	if result == "" {
		t.Fatal("empty result")
	}

	var decoded JobStatus
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.JobID != "test-123" {
		t.Fatalf("expected test-123, got %s", decoded.JobID)
	}
}

func TestNewQueueClient(t *testing.T) {
	q := NewQueueClient("localhost:6379")
	if q == nil {
		t.Fatal("expected non-nil client")
	}
	defer q.Close()
}

func TestJobKey(t *testing.T) {
	got := jobKey("abc-123")
	if got != "job_results:abc-123" {
		t.Fatalf("expected job_results:abc-123, got %s", got)
	}
}

func TestOwnerKey(t *testing.T) {
	got := ownerKey("abc-123")
	if got != "job_owner:abc-123" {
		t.Fatalf("expected job_owner:abc-123, got %s", got)
	}
}
