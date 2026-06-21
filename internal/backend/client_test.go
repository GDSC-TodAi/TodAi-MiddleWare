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
		if r.Method != http.MethodPost || r.URL.Path != "/api/internal/analysis-jobs" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		var request CreateAnalysisJobRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.SessionID != "session-1" ||
			request.ElderID != "elder-1" ||
			request.CorrelationID != "correlation-1" ||
			len(request.RequestedWorkers) != 2 {
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
		ElderID:          "elder-1",
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
		if r.URL.EscapedPath() != "/api/internal/analysis-jobs/job%2F1/events" {
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

func TestUpdateJobStatusUsesAPIInternalPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/internal/analysis-jobs/job-1/status" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var request UpdateJobStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Status != "completed" ||
			request.CorrelationID != "correlation-1" ||
			request.Message != "Both emotion and stt workers completed" {
			t.Fatalf("unexpected request body: %#v", request)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL+"/", time.Second)
	err := client.UpdateJobStatus(context.Background(), "job-1", UpdateJobStatusRequest{
		Status:        "completed",
		CorrelationID: "correlation-1",
		Message:       "Both emotion and stt workers completed",
	})
	if err != nil {
		t.Fatalf("UpdateJobStatus() error = %v", err)
	}
}

func TestSaveAnalysisResultSuccessRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/internal/analysis-jobs/job-1/result" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := body["emotion"]; ok {
			t.Fatalf("request must not include emotion: %#v", body)
		}
		if _, ok := body["emotion_sadness"]; ok {
			t.Fatalf("request must not include emotion raw score: %#v", body)
		}
		metrics, ok := body["metrics"].(map[string]any)
		if !ok {
			t.Fatalf("metrics missing or wrong type: %#v", body["metrics"])
		}
		if len(metrics) != 5 || metrics["social_isolation"] != 0.2 {
			t.Fatalf("unexpected metrics: %#v", metrics)
		}
		if body["stt_text"] != "text-1" {
			t.Fatalf("stt_text = %#v", body["stt_text"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result_id":7,"job_id":"job-1","analysis_status":"SUCCESS","adk_status":"SUCCESS","saved_metric_count":5,"updated_at":"2026-06-21T00:00:00Z"}`))
	}))
	defer server.Close()

	sttText := "text-1"
	client := NewClient(server.URL+"/", time.Second)
	response, err := client.SaveAnalysisResult(context.Background(), "job-1", SaveAnalysisResultRequest{
		SessionID:      "session-1",
		ElderID:        "",
		CorrelationID:  "correlation-1",
		JobStatus:      "completed",
		AnalysisStatus: AnalysisStatusSuccess,
		ADKStatus:      ADKStatusSuccess,
		STTText:        &sttText,
		Metrics: &MetricPayload{
			SocialIsolation: 0.2,
			HealthAnxiety:   0.4,
			DailyVitality:   0.7,
			EmotionVariance: 0.3,
			CognitiveLoad:   0.5,
		},
	})
	if err != nil {
		t.Fatalf("SaveAnalysisResult() error = %v", err)
	}
	if response.ResultID != 7 ||
		response.JobID != "job-1" ||
		response.AnalysisStatus != AnalysisStatusSuccess ||
		response.ADKStatus != ADKStatusSuccess ||
		response.SavedMetricCount != 5 {
		t.Fatalf("response = %#v", response)
	}
}

func TestSaveAnalysisResultNullFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["metrics"] != nil {
			t.Fatalf("metrics = %#v, want nil", body["metrics"])
		}
		if body["stt_text"] != nil {
			t.Fatalf("stt_text = %#v, want nil", body["stt_text"])
		}
		if body["adk_status"] != ADKStatusSkipped {
			t.Fatalf("adk_status = %#v", body["adk_status"])
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	_, err := client.SaveAnalysisResult(context.Background(), "job-1", SaveAnalysisResultRequest{
		SessionID:      "session-1",
		CorrelationID:  "correlation-1",
		JobStatus:      "timeout",
		AnalysisStatus: AnalysisStatusFailed,
		ADKStatus:      ADKStatusSkipped,
	})
	if err != nil {
		t.Fatalf("SaveAnalysisResult() error = %v", err)
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
