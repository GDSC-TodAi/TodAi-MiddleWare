package queue

import (
	"context"
	"strings"
	"testing"

	"github.com/Hyuk-II/todai-middleware/pkg/model"
)

func TestPublishToWorkersReturnsBothValidationErrors(t *testing.T) {
	publisher := &Publisher{}
	emotionReq := model.WorkerRequest{
		JobID:         "job-1",
		CorrelationID: "correlation-1",
		WorkerType:    model.WorkerTypeSTT,
	}
	sttReq := model.WorkerRequest{
		JobID:         "job-1",
		CorrelationID: "correlation-1",
		WorkerType:    model.WorkerTypeEmotion,
	}

	emotionErr, sttErr := publisher.PublishToWorkers(context.Background(), emotionReq, sttReq)
	if emotionErr == nil || sttErr == nil {
		t.Fatalf(
			"PublishToWorkers() errors = (%v, %v), want both worker type validation errors",
			emotionErr,
			sttErr,
		)
	}
	if !strings.Contains(emotionErr.Error(), "publish emotion request") {
		t.Fatalf("error %q does not contain emotion validation failure", emotionErr)
	}
	if !strings.Contains(sttErr.Error(), "publish stt request") {
		t.Fatalf("error %q does not contain stt validation failure", sttErr)
	}
}
