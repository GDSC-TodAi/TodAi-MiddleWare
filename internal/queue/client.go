package queue

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Client owns the RabbitMQ connection/channel used by the slow track publisher.
type Client struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	topology Topology
}

func NewClient(url string, topology Topology) (*Client, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open rabbitmq channel: %w", err)
	}

	client := &Client{
		conn:     conn,
		channel:  ch,
		topology: topology,
	}
	if err := client.DeclareQueues(); err != nil {
		_ = client.Close()
		return nil, err
	}

	return client, nil
}

func (c *Client) DeclareQueues() error {
	for _, queueName := range c.topology.Queues() {
		if _, err := c.channel.QueueDeclare(
			queueName,
			true,  // durable
			false, // autoDelete
			false, // exclusive
			false, // noWait
			nil,   // args
		); err != nil {
			return fmt.Errorf("declare queue %s: %w", queueName, err)
		}
	}

	return nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	var firstErr error
	if c.channel != nil {
		if err := c.channel.Close(); err != nil {
			firstErr = fmt.Errorf("close rabbitmq channel: %w", err)
		}
	}
	if c.conn != nil {
		if err := c.conn.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close rabbitmq connection: %w", err)
		}
	}

	return firstErr
}
