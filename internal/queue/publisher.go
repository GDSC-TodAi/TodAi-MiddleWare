package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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

func (p *Publisher) PublishEmotion(ctx context.Context, payload any) error {
	return p.publish(ctx, p.topology.EmotionQueue, payload)
}

func (p *Publisher) PublishSTT(ctx context.Context, payload any) error {
	return p.publish(ctx, p.topology.STTQueue, payload)
}

func (p *Publisher) PublishToWorkers(ctx context.Context, payload any) error {
	if err := p.PublishEmotion(ctx, payload); err != nil {
		return err
	}
	if err := p.PublishSTT(ctx, payload); err != nil {
		return err
	}

	return nil
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
