package models

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestImportRunString(t *testing.T) {
	ir := ImportRun{
		SourceCount: 10,
		Status:      "running",
	}

	out := ir.String()

	var got ImportRun
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if got.SourceCount != 10 {
		t.Errorf("expected SourceCount 10, got %d", got.SourceCount)
	}
	if got.Status != "running" {
		t.Errorf("expected Status %q, got %q", "running", got.Status)
	}
}

func TestImportRunsString(t *testing.T) {
	irs := ImportRuns{
		{Status: "completed"},
		{Status: "failed"},
	}

	out := irs.String()

	var got ImportRuns
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(got))
	}
	if got[1].Status != "failed" {
		t.Errorf("expected second Status %q, got %q", "failed", got[1].Status)
	}
}

func TestImportRunMarkRunning(t *testing.T) {
	ir := &ImportRun{Status: "pending"}
	before := time.Now()

	ir.MarkRunning()

	if ir.Status != "running" {
		t.Errorf("expected Status %q, got %q", "running", ir.Status)
	}
	if ir.StartedAt.Before(before) {
		t.Error("expected StartedAt to be set to ~now")
	}
}

func TestImportRunMarkCompleted(t *testing.T) {
	ir := &ImportRun{Status: "running"}

	ir.MarkCompleted(100, 95)

	if ir.Status != "completed" {
		t.Errorf("expected Status %q, got %q", "completed", ir.Status)
	}
	if ir.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
	if ir.EventsImported != 100 {
		t.Errorf("expected EventsImported 100, got %d", ir.EventsImported)
	}
	if ir.EventsProcessed != 95 {
		t.Errorf("expected EventsProcessed 95, got %d", ir.EventsProcessed)
	}
}

func TestImportRunMarkFailed(t *testing.T) {
	ir := &ImportRun{Status: "running"}
	boom := errors.New("database exploded")

	ir.MarkFailed(boom)

	if ir.Status != "failed" {
		t.Errorf("expected Status %q, got %q", "failed", ir.Status)
	}
	if ir.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
	if ir.ErrorMessage == nil {
		t.Fatal("expected ErrorMessage to be set")
	}
	if *ir.ErrorMessage != "database exploded" {
		t.Errorf("expected ErrorMessage %q, got %q", "database exploded", *ir.ErrorMessage)
	}
}
