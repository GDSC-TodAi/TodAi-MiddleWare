package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Hyuk-II/todai-middleware/pkg/model"
	amqp "github.com/rabbitmq/amqp091-go"
)

const replyConsumerTag = "todai-reply-consumer"

type ResponseHandler func(ctx context.Context, response model.WorkerResponse) error

// Consumer receives Python worker responses from the configured reply queue.
type Consumer struct {
	channel    *amqp.Channel
	replyQueue string
}

func NewConsumer(client *Client, topology Topology) (*Consumer, error) {
	channel, err := client.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open reply consumer channel: %w", err)
	}

	return &Consumer{
		channel:    channel,
		replyQueue: topology.ReplyQueue,
	}, nil
}

func (c *Consumer) Close() error {
	if c == nil || c.channel == nil {
		return nil
	}
	if err := c.channel.Close(); err != nil {
		return fmt.Errorf("close reply consumer channel: %w", err)
	}
	return nil
}

func (c *Consumer) ConsumeReplies(ctx context.Context, handler ResponseHandler) error {
	if handler == nil {
		return fmt.Errorf("reply response handler is required")
	}

	deliveries, err := c.channel.ConsumeWithContext(
		ctx,
		c.replyQueue,
		replyConsumerTag,
		false, // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("consume reply queue %s: %w", c.replyQueue, err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("reply queue delivery channel closed")
			}

			var response model.WorkerResponse
			if err := json.Unmarshal(delivery.Body, &response); err != nil {
				log.Printf("reject invalid worker response: %v", err)
				// Invalid JSON will remain invalid on retry, so discard it.
				if rejectErr := delivery.Reject(false); rejectErr != nil {
					return fmt.Errorf("reject invalid worker response: %w", rejectErr)
				}
				continue
			}

			if err := handler(ctx, response); err != nil {
				log.Printf(
					"worker response handler failed | job_id=%s correlation_id=%s worker_type=%s error=%v",
					response.JobID,
					response.CorrelationID,
					response.WorkerType,
					err,
				)
				// Handler failures may be transient. Requeue until a later
				// retry/DLQ policy replaces this initial behavior.
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					return fmt.Errorf("nack worker response: %w", nackErr)
				}
				continue
			}

			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("ack worker response: %w", err)
			}
		}
	}
}
