package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDisabledClient(t *testing.T) {
	client := NewClient("", time.Second)

	if client.Enabled() {
		t.Fatal("Enabled() = true, want false")
	}
}

func TestCreateAnalysisJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/analysis-jobs" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		var request CreateAnalysisJobRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.SessionID != "session-1" || len(request.RequestedWorkers) != 2 {
			t.Fatalf("unexpected request body: %#v", request)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"job_id":"job-1","status":"queued"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL+"/", time.Second)
	response, err := client.CreateAnalysisJob(context.Background(), CreateAnalysisJobRequest{
		SessionID:        "session-1",
		CorrelationID:    "correlation-1",
		RequestedWorkers: []string{"emotion", "stt"},
	})
	if err != nil {
		t.Fatalf("CreateAnalysisJob() error = %v", err)
	}
	if response.JobID != "job-1" || response.Status != "queued" {
		t.Fatalf("response = %#v", response)
	}
}

func TestJobIDIsPathEscaped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/internal/analysis-jobs/job%2F1/events" {
			t.Fatalf("escaped path = %q", r.URL.EscapedPath())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	err := client.CreateJobEvent(context.Background(), "job/1", CreateJobEventRequest{
		EventType:     "PUBLISHED",
		CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("CreateJobEvent() error = %v", err)
	}
}

func TestNon2xxIncludesStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "backend unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	err := client.UpdateJobStatus(context.Background(), "job-1", UpdateJobStatusRequest{
		Status:        "failed",
		CorrelationID: "correlation-1",
	})
	if err == nil {
		t.Fatal("UpdateJobStatus() error = nil")
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "backend unavailable") {
		t.Fatalf("error = %q", err)
	}
}

func TestRequestRespectsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.CreateAnalysisJob(ctx, CreateAnalysisJobRequest{})
	if err == nil {
		t.Fatal("CreateAnalysisJob() error = nil")
	}
}
