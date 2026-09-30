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
