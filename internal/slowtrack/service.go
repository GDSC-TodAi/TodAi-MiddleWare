package slowtrack

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Hyuk-II/todai-middleware/internal/backend"
	"github.com/Hyuk-II/todai-middleware/pkg/model"
	"github.com/google/uuid"
)

// Publisher is the queue capability required by the slow track service.
type Publisher interface {
	PublishToWorkers(
		ctx context.Context,
		emotionReq model.WorkerRequest,
		sttReq model.WorkerRequest,
	) (emotionErr error, sttErr error)
}

type JobBackend interface {
	Enabled() bool
	CreateAnalysisJob(
		ctx context.Context,
		req backend.CreateAnalysisJobRequest,
	) (backend.CreateAnalysisJobResponse, error)
	CreateJobEvent(
		ctx context.Context,
		jobID string,
		req backend.CreateJobEventRequest,
	) error
}

// Service prepares and publishes audio work without coupling WebSocket handling
// to RabbitMQ request construction or execution policy.
type Service struct {
	publisher      Publisher
	jobBackend     JobBackend
	replyQueue     string
	publishTimeout time.Duration
}

func NewService(
	publisher Publisher,
	jobBackend JobBackend,
	replyQueue string,
	publishTimeout time.Duration,
) *Service {
	return &Service{
		publisher:      publisher,
		jobBackend:     jobBackend,
		replyQueue:     replyQueue,
		publishTimeout: publishTimeout,
	}
}

// PublishUtterance sends a complete spoken utterance to both Python workers via RabbitMQ.
// Called by the orchestrator after VAD confirms utterance end.
func (s *Service) PublishUtterance(
	ctx context.Context,
	sessionID string,
	audioData []byte,
) error {
	correlationID := uuid.New().String()
	timestamp := time.Now().UnixMilli()
	// TODO: Populate ElderID when the WebSocket connection carries elder identity.
	elderID := ""

	jobID, err := s.createJob(ctx, sessionID, elderID, correlationID)
	if err != nil {
		return err
	}

	s.recordEvent(ctx, jobID, backend.CreateJobEventRequest{
		EventType:     backend.EventTypePublishStarted,
		CorrelationID: correlationID,
		Message:       "Worker request publishing started",
	})

	baseReq := model.WorkerRequest{
		JobID:         jobID,
		SessionID:     sessionID,
		ElderID:       elderID,
		CorrelationID: correlationID,
		ReplyTo:       s.replyQueue,
		AudioData:     audioData,
		Timestamp:     timestamp,
	}
	emotionReq := baseReq
	emotionReq.WorkerType = model.WorkerTypeEmotion
	sttReq := baseReq
	sttReq.WorkerType = model.WorkerTypeSTT

	pubCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
	defer cancel()

	emotionErr, sttErr := s.publisher.PublishToWorkers(pubCtx, emotionReq, sttReq)
	s.recordPublishResult(ctx, jobID, correlationID, model.WorkerTypeEmotion, emotionErr)
	s.recordPublishResult(ctx, jobID, correlationID, model.WorkerTypeSTT, sttErr)

	if err := errors.Join(emotionErr, sttErr); err != nil {
		log.Printf(
			"[%s] slow track publish incomplete | job_id=%s correlation_id=%s error=%v",
			sessionID,
			jobID,
			correlationID,
			err,
		)
		return err
	}

	log.Printf(
		"[%s] utterance published | job_id=%s correlation_id=%s size=%d bytes",
		sessionID,
		jobID,
		correlationID,
		len(audioData),
	)
	return nil
}

func (s *Service) createJob(
	ctx context.Context,
	sessionID string,
	elderID string,
	correlationID string,
) (string, error) {
	if s.jobBackend == nil || !s.jobBackend.Enabled() {
		return uuid.New().String(), nil
	}

	response, err := s.jobBackend.CreateAnalysisJob(ctx, backend.CreateAnalysisJobRequest{
		SessionID:        sessionID,
		ElderID:          elderID,
		CorrelationID:    correlationID,
		RequestedWorkers: []string{model.WorkerTypeEmotion, model.WorkerTypeSTT},
	})
	if err != nil {
		return "", fmt.Errorf("create analysis job before publish: %w", err)
	}
	return response.JobID, nil
}

func (s *Service) recordPublishResult(
	ctx context.Context,
	jobID string,
	correlationID string,
	workerType string,
	publishErr error,
) {
	eventType := backend.EventTypePublished
	message := workerType + " worker request published"
	if publishErr != nil {
		eventType = backend.EventTypePublishFailed
		message = workerType + " worker request publish failed: " + publishErr.Error()
	}

	s.recordEvent(ctx, jobID, backend.CreateJobEventRequest{
		EventType:     eventType,
		WorkerType:    workerType,
		CorrelationID: correlationID,
		Message:       message,
	})
}

func (s *Service) recordEvent(
	ctx context.Context,
	jobID string,
	req backend.CreateJobEventRequest,
) {
	if s.jobBackend == nil || !s.jobBackend.Enabled() {
		return
	}
	if err := s.jobBackend.CreateJobEvent(ctx, jobID, req); err != nil {
		log.Printf(
			"backend job event failed | job_id=%s correlation_id=%s worker_type=%s event_type=%s error=%v",
			jobID,
			req.CorrelationID,
			req.WorkerType,
			req.EventType,
			err,
		)
	}
}
