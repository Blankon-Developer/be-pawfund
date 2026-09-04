package queue

import (
	"context"
	"fmt"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
)

const defaultContentType = "application/octet-stream"

// Message is a single AMQP publishing. An empty Exchange routes via the
// default exchange, so RoutingKey should be the destination queue name.
type Message struct {
	Exchange    string
	RoutingKey  string
	Body        []byte
	ContentType string
	Persistent  bool
}

// Publisher publishes messages using a QueueClient. Each Publish opens a
// channel, waits for a publisher confirm, and closes the channel so it never
// holds a connection or channel across a reconnect.
type Publisher struct {
	client *QueueClient
}

func NewPublisher(client *QueueClient) *Publisher {
	return &Publisher{client: client}
}

func (p *Publisher) Publish(ctx context.Context, msg Message) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("queue: publish: client is required")
	}
	if msg.Body == nil {
		return fmt.Errorf("queue: publish: body is required")
	}
	routingKey := strings.TrimSpace(msg.RoutingKey)
	if routingKey == "" {
		return fmt.Errorf("queue: publish: routing key is required")
	}

	contentType := strings.TrimSpace(msg.ContentType)
	if contentType == "" {
		contentType = defaultContentType
	}

	conn := p.client.Conn()
	if conn == nil || conn.IsClosed() {
		return fmt.Errorf("queue: publish: connection is not available")
	}

	channel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("queue: publish: %w", err)
	}
	defer channel.Close()

	if err := channel.Confirm(false); err != nil {
		return fmt.Errorf("queue: publish: confirm mode: %w", err)
	}

	publishing := amqp.Publishing{
		ContentType:  contentType,
		Body:         msg.Body,
		DeliveryMode: amqp.Transient,
	}
	if msg.Persistent {
		publishing.DeliveryMode = amqp.Persistent
	}

	confirmation, err := channel.PublishWithDeferredConfirmWithContext(
		ctx,
		strings.TrimSpace(msg.Exchange),
		routingKey,
		false,
		false,
		publishing,
	)
	if err != nil {
		return fmt.Errorf("queue: publish: %w", err)
	}
	if confirmation == nil {
		return fmt.Errorf("queue: publish: confirm mode is not enabled")
	}

	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("queue: publish: %w", err)
	}
	if !acked {
		return fmt.Errorf("queue: publish: broker negatively acknowledged the message")
	}
	return nil
}
