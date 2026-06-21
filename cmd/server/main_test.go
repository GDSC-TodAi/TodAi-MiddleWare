package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hyuk-II/todai-middleware/internal/adk"
	"github.com/Hyuk-II/todai-middleware/internal/aggregator"
	"github.com/Hyuk-II/todai-middleware/internal/backend"
	"github.com/Hyuk-II/todai-middleware/pkg/model"
)

func TestFinalStatusHandlerSavesADKSuccessResult(t *testing.T) {
	var statusCalled bool
	var resultBody map[string]any

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/analysis-jobs/job-1/status":
			statusCalled = true
			if r.Method != http.MethodPatch {
				t.Fatalf("status method = %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/internal/analysis-jobs/job-1/result":
			if r.Method != http.MethodPost {
				t.Fatalf("result method = %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&resultBody); err != nil {
				t.Fatalf("decode result body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"result_id":1,"job_id":"job-1","analysis_status":"SUCCESS","adk_status":"SUCCESS","saved_metric_count":5,"updated_at":"2026-06-21T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected backend path: %s", r.URL.Path)
		}
	}))
	defer backendServer.Close()

	adkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/analyze" {
			t.Fatalf("unexpected adk request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"social_isolation":0.2,"health_anxiety":0.4,"daily_vitality":0.7,"emotion_variance":0.3,"cognitive_load":0.5}`))
	}))
	defer adkServer.Close()

	handler := buildFinalStatusHandler(
		backend.NewClient(backendServer.URL, time.Second),
		adk.NewClient(adkServer.URL, time.Second),
	)
	err := handler(context.Background(), aggregator.FinalResult{
		JobID:         "job-1",
		SessionID:     "session-1",
		ElderID:       "",
		CorrelationID: "correlation-1",
		Status:        aggregator.StatusCompleted,
		Message:       "Both emotion and stt workers completed",
		EmotionResult: &model.EmotionResult{Sadness: 0.1, Anxiety: 0.2, Neutral: 0.6, Joy: 0.1},
		STTText:       "text-1",
	})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if !statusCalled {
		t.Fatal("status update was not called")
	}
	if resultBody["analysis_status"] != backend.AnalysisStatusSuccess ||
		resultBody["adk_status"] != backend.ADKStatusSuccess ||
		resultBody["stt_text"] != "text-1" {
		t.Fatalf("unexpected result body: %#v", resultBody)
	}
	if _, ok := resultBody["emotion"]; ok {
		t.Fatalf("result body must not include emotion: %#v", resultBody)
	}
	metrics, ok := resultBody["metrics"].(map[string]any)
	if !ok || len(metrics) != 5 {
		t.Fatalf("metrics = %#v", resultBody["metrics"])
	}
}

func TestFinalStatusHandlerSavesSkippedResult(t *testing.T) {
	var resultBody map[string]any

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/analysis-jobs/job-1/status":
			w.WriteHeader(http.StatusNoContent)
		case "/api/internal/analysis-jobs/job-1/result":
			if err := json.NewDecoder(r.Body).Decode(&resultBody); err != nil {
				t.Fatalf("decode result body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected backend path: %s", r.URL.Path)
		}
	}))
	defer backendServer.Close()

	handler := buildFinalStatusHandler(
		backend.NewClient(backendServer.URL+"/", time.Second),
		adk.NewClient("", time.Second),
	)
	err := handler(context.Background(), aggregator.FinalResult{
		JobID:         "job-1",
		SessionID:     "session-1",
		CorrelationID: "correlation-1",
		Status:        aggregator.StatusPartialTimeout,
		Message:       "Timed out waiting for one worker response",
		STTText:       "text-1",
	})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if resultBody["analysis_status"] != backend.AnalysisStatusPartial ||
		resultBody["adk_status"] != backend.ADKStatusSkipped ||
		resultBody["job_status"] != aggregator.StatusPartialTimeout {
		t.Fatalf("unexpected result body: %#v", resultBody)
	}
	if resultBody["metrics"] != nil {
		t.Fatalf("metrics = %#v, want nil", resultBody["metrics"])
	}
	if resultBody["error_reason"] != "one or more workers timed out" {
		t.Fatalf("error_reason = %#v", resultBody["error_reason"])
	}
}

func TestFinalStatusHandlerSavesADKFailedResult(t *testing.T) {
	var resultBody map[string]any

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/analysis-jobs/job-1/status":
			w.WriteHeader(http.StatusNoContent)
		case "/api/internal/analysis-jobs/job-1/result":
			if err := json.NewDecoder(r.Body).Decode(&resultBody); err != nil {
				t.Fatalf("decode result body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected backend path: %s", r.URL.Path)
		}
	}))
	defer backendServer.Close()

	adkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "adk unavailable", http.StatusServiceUnavailable)
	}))
	defer adkServer.Close()

	handler := buildFinalStatusHandler(
		backend.NewClient(backendServer.URL, time.Second),
		adk.NewClient(adkServer.URL, time.Second),
	)
	err := handler(context.Background(), aggregator.FinalResult{
		JobID:         "job-1",
		SessionID:     "session-1",
		CorrelationID: "correlation-1",
		Status:        aggregator.StatusCompleted,
		Message:       "Both emotion and stt workers completed",
		EmotionResult: &model.EmotionResult{Sadness: 0.1, Anxiety: 0.2, Neutral: 0.6, Joy: 0.1},
		STTText:       "text-1",
	})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if resultBody["analysis_status"] != backend.AnalysisStatusPartial ||
		resultBody["adk_status"] != backend.ADKStatusFailed {
		t.Fatalf("unexpected result body: %#v", resultBody)
	}
	if resultBody["metrics"] != nil {
		t.Fatalf("metrics = %#v, want nil", resultBody["metrics"])
	}
	if resultBody["adk_error_reason"] == nil {
		t.Fatalf("adk_error_reason was not set: %#v", resultBody)
	}
}
