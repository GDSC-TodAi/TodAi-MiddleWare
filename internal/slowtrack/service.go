package slowtrack

import (
	"context"
	"log"
	"time"

	"github.com/Hyuk-II/todai-middleware/pkg/model"
	"github.com/google/uuid"
)

const defaultPublishTimeout = 3 * time.Second

// Publisher is the queue capability required by the slow track service.
type Publisher interface {
	PublishToWorkers(ctx context.Context, payload any) error
}

// Service prepares and publishes audio work without coupling WebSocket handling
// to RabbitMQ request construction or execution policy.
type Service struct {
	publisher      Publisher
	publishTimeout time.Duration
}

func NewService(publisher Publisher) *Service {
	return &Service{
		publisher:      publisher,
		publishTimeout: defaultPublishTimeout,
	}
}

// HandleAudioChunk temporarily treats each received chunk as a publish unit.
// TODO: Replace this entry point with utterance-level audio after VAD and
// utterance assembly policies are finalized.
func (s *Service) HandleAudioChunk(ctx context.Context, sessionID string, audioData []byte) {
	if s == nil || s.publisher == nil {
		log.Printf("[%s] slow track publisher not configured, skipping audio chunk", sessionID)
		return
	}

	audioCopy := append([]byte(nil), audioData...)
	req := model.WorkerRequest{
		SessionID:     sessionID,
		CorrelationID: uuid.New().String(),
		ReplyTo:       "", // TODO: set when reply queue/aggregator is implemented.
		AudioData:     audioCopy,
		Timestamp:     time.Now().UnixMilli(),
	}

	go s.publish(context.WithoutCancel(ctx), req)
}

func (s *Service) publish(parent context.Context, req model.WorkerRequest) {
	ctx, cancel := context.WithTimeout(parent, s.publishTimeout)
	defer cancel()

	if err := s.publisher.PublishToWorkers(ctx, req); err != nil {
		log.Printf("[%s] slow track publish failed: %v", req.SessionID, err)
		return
	}

	log.Printf("[%s] slow track chunk published | correlation_id %s", req.SessionID, req.CorrelationID)
}
