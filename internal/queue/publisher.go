package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/Hyuk-II/todai-middleware/pkg/model"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher sends utterance-level slow track work to Python workers.
type Publisher struct {
	channel  *amqp.Channel
	topology Topology
	mu       sync.Mutex
}

func NewPublisher(client *Client) *Publisher {
	return &Publisher{
		channel:  client.channel,
		topology: client.topology,
	}
}

func (p *Publisher) PublishEmotion(ctx context.Context, req model.WorkerRequest) error {
	if req.WorkerType != model.WorkerTypeEmotion {
		return fmt.Errorf("publish emotion request: invalid worker_type %q", req.WorkerType)
	}
	return p.publish(ctx, p.topology.EmotionQueue, req)
}

func (p *Publisher) PublishSTT(ctx context.Context, req model.WorkerRequest) error {
	if req.WorkerType != model.WorkerTypeSTT {
		return fmt.Errorf("publish stt request: invalid worker_type %q", req.WorkerType)
	}
	return p.publish(ctx, p.topology.STTQueue, req)
}

func (p *Publisher) PublishToWorkers(
	ctx context.Context,
	emotionReq model.WorkerRequest,
	sttReq model.WorkerRequest,
) (error, error) {
	// Both workers are attempted independently so one queue failure does not
	// prevent the other worker from receiving the job.
	emotionErr := p.PublishEmotion(ctx, emotionReq)
	if emotionErr != nil {
		log.Printf(
			"emotion publish failed | job_id=%s correlation_id=%s error=%v",
			emotionReq.JobID,
			emotionReq.CorrelationID,
			emotionErr,
		)
	} else {
		log.Printf(
			"emotion publish succeeded | job_id=%s correlation_id=%s",
			emotionReq.JobID,
			emotionReq.CorrelationID,
		)
	}

	sttErr := p.PublishSTT(ctx, sttReq)
	if sttErr != nil {
		log.Printf(
			"stt publish failed | job_id=%s correlation_id=%s error=%v",
			sttReq.JobID,
			sttReq.CorrelationID,
			sttErr,
		)
	} else {
		log.Printf(
			"stt publish succeeded | job_id=%s correlation_id=%s",
			sttReq.JobID,
			sttReq.CorrelationID,
		)
	}

	// TODO: Record each publish outcome in job_event_history when DB integration is added.
	return emotionErr, sttErr
}

func (p *Publisher) publish(ctx context.Context, queueName string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal worker payload: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.channel.PublishWithContext(
		ctx,
		"",        // default exchange
		queueName, // routing key is the queue name
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	); err != nil {
		return fmt.Errorf("publish to %s: %w", queueName, err)
	}

	return nil
}
