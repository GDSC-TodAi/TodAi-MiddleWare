package slowtrack

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hyuk-II/todai-middleware/internal/backend"
	"github.com/Hyuk-II/todai-middleware/pkg/model"
)

type recordingPublisher struct {
	emotionReq model.WorkerRequest
	sttReq     model.WorkerRequest
	called     bool
	emotionErr error
	sttErr     error
}

func (p *recordingPublisher) PublishToWorkers(
	_ context.Context,
	emotionReq model.WorkerRequest,
	sttReq model.WorkerRequest,
) (error, error) {
	p.called = true
	p.emotionReq = emotionReq
	p.sttReq = sttReq
	return p.emotionErr, p.sttErr
}

type recordingBackend struct {
	enabled       bool
	createJobResp backend.CreateAnalysisJobResponse
	createJobErr  error
	createJobReq  backend.CreateAnalysisJobRequest
	events        []backend.CreateJobEventRequest
	eventErr      error
}

func (b *recordingBackend) Enabled() bool {
	return b.enabled
}

func (b *recordingBackend) CreateAnalysisJob(
	_ context.Context,
	req backend.CreateAnalysisJobRequest,
) (backend.CreateAnalysisJobResponse, error) {
	b.createJobReq = req
	return b.createJobResp, b.createJobErr
}

func (b *recordingBackend) CreateJobEvent(
	_ context.Context,
	_ string,
	req backend.CreateJobEventRequest,
) error {
	b.events = append(b.events, req)
	return b.eventErr
}

func TestPublishUtteranceBuildsWorkerRequests(t *testing.T) {
	publisher := &recordingPublisher{}
	service := NewService(publisher, nil, "todai.reply", time.Second)
	audioData := []byte{1, 2, 3, 4}

	if err := service.PublishUtterance(context.Background(), "session-1", audioData); err != nil {
		t.Fatalf("PublishUtterance() error = %v", err)
	}

	if !publisher.called {
		t.Fatal("PublishToWorkers was not called")
	}

	emotionReq := publisher.emotionReq
	sttReq := publisher.sttReq

	if emotionReq.JobID == "" || emotionReq.CorrelationID == "" {
		t.Fatal("job_id and correlation_id must be generated")
	}
	if emotionReq.JobID != sttReq.JobID {
		t.Fatalf("job_id mismatch: emotion=%q stt=%q", emotionReq.JobID, sttReq.JobID)
	}
	if emotionReq.CorrelationID != sttReq.CorrelationID {
		t.Fatalf(
			"correlation_id mismatch: emotion=%q stt=%q",
			emotionReq.CorrelationID,
			sttReq.CorrelationID,
		)
	}
	if emotionReq.SessionID != "session-1" || sttReq.SessionID != "session-1" {
		t.Fatalf("session_id was not shared: emotion=%q stt=%q", emotionReq.SessionID, sttReq.SessionID)
	}
	if emotionReq.ElderID != "" || sttReq.ElderID != "" {
		t.Fatalf("elder_id must remain empty until identity is supplied")
	}
	if emotionReq.WorkerType != model.WorkerTypeEmotion {
		t.Fatalf("emotion worker_type = %q", emotionReq.WorkerType)
	}
	if sttReq.WorkerType != model.WorkerTypeSTT {
		t.Fatalf("stt worker_type = %q", sttReq.WorkerType)
	}
	if emotionReq.ReplyTo != "todai.reply" || sttReq.ReplyTo != "todai.reply" {
		t.Fatalf("reply_to mismatch: emotion=%q stt=%q", emotionReq.ReplyTo, sttReq.ReplyTo)
	}
	if string(emotionReq.AudioData) != string(audioData) || string(sttReq.AudioData) != string(audioData) {
		t.Fatal("audio_data was not shared with both requests")
	}
	if emotionReq.Timestamp == 0 || emotionReq.Timestamp != sttReq.Timestamp {
		t.Fatalf(
			"timestamp must be non-zero and shared: emotion=%d stt=%d",
			emotionReq.Timestamp,
			sttReq.Timestamp,
		)
	}
}

func TestPublishUtteranceUsesBackendJobAndRecordsEvents(t *testing.T) {
	publisher := &recordingPublisher{}
	jobBackend := &recordingBackend{
		enabled: true,
		createJobResp: backend.CreateAnalysisJobResponse{
			JobID:  "backend-job-1",
			Status: "queued",
		},
	}
	service := NewService(publisher, jobBackend, "todai.reply", time.Second)

	if err := service.PublishUtterance(context.Background(), "session-1", []byte{1}); err != nil {
		t.Fatalf("PublishUtterance() error = %v", err)
	}

	if publisher.emotionReq.JobID != "backend-job-1" || publisher.sttReq.JobID != "backend-job-1" {
		t.Fatalf(
			"published job IDs = (%q, %q)",
			publisher.emotionReq.JobID,
			publisher.sttReq.JobID,
		)
	}
	if jobBackend.createJobReq.SessionID != "session-1" {
		t.Fatalf("create job session_id = %q", jobBackend.createJobReq.SessionID)
	}
	if len(jobBackend.createJobReq.RequestedWorkers) != 2 {
		t.Fatalf("requested workers = %#v", jobBackend.createJobReq.RequestedWorkers)
	}
	if len(jobBackend.events) != 3 {
		t.Fatalf("event count = %d, want 3", len(jobBackend.events))
	}
	if jobBackend.events[0].EventType != backend.EventTypePublishStarted {
		t.Fatalf("first event = %#v", jobBackend.events[0])
	}
	if jobBackend.events[1].EventType != backend.EventTypePublished ||
		jobBackend.events[1].WorkerType != model.WorkerTypeEmotion {
		t.Fatalf("emotion event = %#v", jobBackend.events[1])
	}
	if jobBackend.events[2].EventType != backend.EventTypePublished ||
		jobBackend.events[2].WorkerType != model.WorkerTypeSTT {
		t.Fatalf("stt event = %#v", jobBackend.events[2])
	}
}

func TestCreateJobFailureStopsPublish(t *testing.T) {
	publisher := &recordingPublisher{}
	jobBackend := &recordingBackend{
		enabled:      true,
		createJobErr: errors.New("backend down"),
	}
	service := NewService(publisher, jobBackend, "todai.reply", time.Second)

	err := service.PublishUtterance(context.Background(), "session-1", []byte{1})
	if err == nil {
		t.Fatal("PublishUtterance() error = nil")
	}
	if publisher.called {
		t.Fatal("publisher was called after job creation failure")
	}
}

func TestEventFailureDoesNotFailSuccessfulPublish(t *testing.T) {
	publisher := &recordingPublisher{}
	jobBackend := &recordingBackend{
		enabled: true,
		createJobResp: backend.CreateAnalysisJobResponse{
			JobID: "backend-job-1",
		},
		eventErr: errors.New("event API down"),
	}
	service := NewService(publisher, jobBackend, "todai.reply", time.Second)

	if err := service.PublishUtterance(context.Background(), "session-1", []byte{1}); err != nil {
		t.Fatalf("PublishUtterance() error = %v", err)
	}
}

func TestPublishFailuresCreateFailedEvents(t *testing.T) {
	publisher := &recordingPublisher{
		emotionErr: errors.New("emotion failed"),
		sttErr:     errors.New("stt failed"),
	}
	jobBackend := &recordingBackend{
		enabled: true,
		createJobResp: backend.CreateAnalysisJobResponse{
			JobID: "backend-job-1",
		},
	}
	service := NewService(publisher, jobBackend, "todai.reply", time.Second)

	if err := service.PublishUtterance(context.Background(), "session-1", []byte{1}); err == nil {
		t.Fatal("PublishUtterance() error = nil")
	}
	if len(jobBackend.events) != 3 {
		t.Fatalf("event count = %d, want 3", len(jobBackend.events))
	}
	if jobBackend.events[1].EventType != backend.EventTypePublishFailed ||
		jobBackend.events[2].EventType != backend.EventTypePublishFailed {
		t.Fatalf("publish events = %#v", jobBackend.events[1:])
	}
}
