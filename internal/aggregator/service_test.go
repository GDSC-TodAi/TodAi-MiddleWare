package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/Hyuk-II/todai-middleware/pkg/model"
)

func TestEmotionResponseCreatesState(t *testing.T) {
	service := NewService(time.Second)
	defer service.Close()

	response := workerResponse(model.WorkerTypeEmotion, model.WorkerStatusSuccess)
	if err := service.HandleWorkerResponse(context.Background(), response); err != nil {
		t.Fatalf("HandleWorkerResponse() error = %v", err)
	}

	service.mu.Lock()
	state := service.states[stateKey(response.JobID, response.CorrelationID)]
	service.mu.Unlock()

	if state == nil {
		t.Fatal("job state was not created")
	}
	if state.EmotionResult == nil {
		t.Fatal("emotion result was not stored")
	}
	if state.STTResult != nil {
		t.Fatal("stt result must be empty")
	}
}

func TestEmotionThenSTTCompletes(t *testing.T) {
	assertCompletionOrder(t, model.WorkerTypeEmotion, model.WorkerTypeSTT)
}

func TestSTTThenEmotionCompletes(t *testing.T) {
	assertCompletionOrder(t, model.WorkerTypeSTT, model.WorkerTypeEmotion)
}

func TestMixedWorkerStatusesProducePartialFailed(t *testing.T) {
	result := finalizeResponses(
		t,
		model.WorkerStatusSuccess,
		model.WorkerStatusFailed,
	)
	if result.Status != StatusPartialFailed {
		t.Fatalf("final status = %q, want %q", result.Status, StatusPartialFailed)
	}
}

func TestBothWorkerFailuresProduceFailed(t *testing.T) {
	result := finalizeResponses(
		t,
		model.WorkerStatusFailed,
		model.WorkerStatusFailed,
	)
	if result.Status != StatusFailed {
		t.Fatalf("final status = %q, want %q", result.Status, StatusFailed)
	}
}

func TestUnknownWorkerTypeReturnsError(t *testing.T) {
	service := NewService(time.Second)
	defer service.Close()

	response := workerResponse("unknown", model.WorkerStatusSuccess)
	if err := service.HandleWorkerResponse(context.Background(), response); err == nil {
		t.Fatal("HandleWorkerResponse() error = nil, want unknown worker type error")
	}
}

func TestDuplicateWorkerResponseIsIgnored(t *testing.T) {
	service := NewService(time.Second)
	defer service.Close()

	first := workerResponse(model.WorkerTypeEmotion, model.WorkerStatusSuccess)
	second := first
	second.Status = model.WorkerStatusFailed

	if err := service.HandleWorkerResponse(context.Background(), first); err != nil {
		t.Fatalf("first HandleWorkerResponse() error = %v", err)
	}
	if err := service.HandleWorkerResponse(context.Background(), second); err != nil {
		t.Fatalf("duplicate HandleWorkerResponse() error = %v", err)
	}

	service.mu.Lock()
	state := service.states[stateKey(first.JobID, first.CorrelationID)]
	service.mu.Unlock()

	if state == nil || state.EmotionResult == nil {
		t.Fatal("emotion state was not retained")
	}
	if state.EmotionResult.Status != model.WorkerStatusSuccess {
		t.Fatalf("duplicate overwrote first response: status=%q", state.EmotionResult.Status)
	}
}

func TestPartialTimeoutFinalizesAndDeletesState(t *testing.T) {
	service := NewService(10 * time.Millisecond)
	defer service.Close()

	finalized := make(chan FinalResult, 1)
	service.onFinalized = func(result FinalResult) {
		finalized <- result
	}

	response := workerResponse(model.WorkerTypeEmotion, model.WorkerStatusSuccess)
	if err := service.HandleWorkerResponse(context.Background(), response); err != nil {
		t.Fatalf("HandleWorkerResponse() error = %v", err)
	}

	select {
	case result := <-finalized:
		if result.Status != StatusPartialTimeout {
			t.Fatalf("final status = %q, want %q", result.Status, StatusPartialTimeout)
		}
		if result.WorkerType != model.WorkerTypeEmotion {
			t.Fatalf("worker type = %q, want %q", result.WorkerType, model.WorkerTypeEmotion)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout finalization did not run")
	}

	service.mu.Lock()
	_, exists := service.states[stateKey(response.JobID, response.CorrelationID)]
	service.mu.Unlock()
	if exists {
		t.Fatal("timed out state was not deleted")
	}
}

func assertCompletionOrder(t *testing.T, firstWorker, secondWorker string) {
	t.Helper()

	service := NewService(time.Second)
	defer service.Close()

	finalized := make(chan FinalResult, 1)
	service.onFinalized = func(result FinalResult) {
		finalized <- result
	}

	first := workerResponse(firstWorker, model.WorkerStatusSuccess)
	second := workerResponse(secondWorker, model.WorkerStatusSuccess)

	if err := service.HandleWorkerResponse(context.Background(), first); err != nil {
		t.Fatalf("first HandleWorkerResponse() error = %v", err)
	}
	if err := service.HandleWorkerResponse(context.Background(), second); err != nil {
		t.Fatalf("second HandleWorkerResponse() error = %v", err)
	}

	select {
	case result := <-finalized:
		if result.Status != StatusCompleted {
			t.Fatalf("final status = %q, want %q", result.Status, StatusCompleted)
		}
		if result.JobID != "job-1" || result.CorrelationID != "correlation-1" {
			t.Fatalf("final identifiers = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("completion finalization did not run")
	}

	service.mu.Lock()
	stateCount := len(service.states)
	service.mu.Unlock()
	if stateCount != 0 {
		t.Fatalf("state count = %d, want 0 after completion", stateCount)
	}
}

func TestFinalStatusHandlerFailureDoesNotFailAggregation(t *testing.T) {
	handlerCalled := false
	service := NewService(time.Second, func(_ context.Context, result FinalResult) error {
		handlerCalled = true
		if result.Status != StatusCompleted {
			t.Fatalf("handler status = %q", result.Status)
		}
		return context.DeadlineExceeded
	})
	defer service.Close()

	emotion := workerResponse(model.WorkerTypeEmotion, model.WorkerStatusSuccess)
	stt := workerResponse(model.WorkerTypeSTT, model.WorkerStatusSuccess)
	if err := service.HandleWorkerResponse(context.Background(), emotion); err != nil {
		t.Fatalf("emotion HandleWorkerResponse() error = %v", err)
	}
	if err := service.HandleWorkerResponse(context.Background(), stt); err != nil {
		t.Fatalf("stt HandleWorkerResponse() error = %v", err)
	}
	if !handlerCalled {
		t.Fatal("final status handler was not called")
	}
}

func finalizeResponses(t *testing.T, emotionStatus, sttStatus string) FinalResult {
	t.Helper()

	service := NewService(time.Second)
	defer service.Close()

	finalized := make(chan FinalResult, 1)
	service.onFinalized = func(result FinalResult) {
		finalized <- result
	}

	emotion := workerResponse(model.WorkerTypeEmotion, emotionStatus)
	stt := workerResponse(model.WorkerTypeSTT, sttStatus)
	if err := service.HandleWorkerResponse(context.Background(), emotion); err != nil {
		t.Fatalf("emotion HandleWorkerResponse() error = %v", err)
	}
	if err := service.HandleWorkerResponse(context.Background(), stt); err != nil {
		t.Fatalf("stt HandleWorkerResponse() error = %v", err)
	}

	select {
	case result := <-finalized:
		return result
	case <-time.After(time.Second):
		t.Fatal("finalization did not run")
		return FinalResult{}
	}
}

func workerResponse(workerType, status string) model.WorkerResponse {
	return model.WorkerResponse{
		JobID:         "job-1",
		SessionID:     "session-1",
		ElderID:       "elder-1",
		CorrelationID: "correlation-1",
		WorkerType:    workerType,
		Status:        status,
		Timestamp:     time.Now().UnixMilli(),
	}
}
