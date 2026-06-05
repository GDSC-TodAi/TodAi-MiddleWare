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

// PublishUtterance sends a complete spoken utterance to both Python workers via RabbitMQ.
// Called by the orchestrator after VAD confirms utterance end.
func (s *Service) PublishUtterance(ctx context.Context, sessionID string, audioData []byte) {
	req := model.WorkerRequest{
		SessionID:     sessionID,
		CorrelationID: uuid.New().String(),
		ReplyTo:       "", // TODO: set when aggregator is implemented (step 5).
		AudioData:     audioData,
		Timestamp:     time.Now().UnixMilli(),
	}

	pubCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
	defer cancel()

	if err := s.publisher.PublishToWorkers(pubCtx, req); err != nil {
		log.Printf("[%s] slow track publish failed: %v", sessionID, err)
		return
	}

	log.Printf("[%s] utterance published | correlation_id=%s size=%d bytes",
		sessionID, req.CorrelationID, len(audioData))
}
