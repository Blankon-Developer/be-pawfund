package queue

import (
	"fmt"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Topology describes the exchange, queue, and optional binding to declare.
// Declare is idempotent when the broker already has matching objects.
//
// Exclusive applies only to the queue. Non-durable queues must be exclusive:
// RabbitMQ 4.x rejects transient non-exclusive queues by default.
type Topology struct {
	Exchange     string
	ExchangeKind string // empty and Exchange set => "topic"
	Queue        string
	BindingKey   string
	Durable      bool
	Exclusive    bool
	AutoDelete   bool
}

// Declare declares the exchange and/or queue on ch and binds them when both
// are set. It does not open or close connections.
func Declare(ch *amqp.Channel, t Topology) error {
	if err := validateTopology(t); err != nil {
		return err
	}
	if ch == nil {
		return fmt.Errorf("queue: declare: channel is required")
	}

	exchange := strings.TrimSpace(t.Exchange)
	queueName := strings.TrimSpace(t.Queue)
	kind := strings.TrimSpace(t.ExchangeKind)
	if kind == "" {
		kind = amqp.ExchangeTopic
	}

	if exchange != "" {
		if err := ch.ExchangeDeclare(exchange, kind, t.Durable, t.AutoDelete, false, false, nil); err != nil {
			return fmt.Errorf("queue: declare exchange %q: %w", exchange, err)
		}
	}
	if queueName != "" {
		if _, err := ch.QueueDeclare(queueName, t.Durable, t.AutoDelete, t.Exclusive, false, nil); err != nil {
			return fmt.Errorf("queue: declare queue %q: %w", queueName, err)
		}
	}
	if exchange != "" && queueName != "" {
		if err := ch.QueueBind(queueName, t.BindingKey, exchange, false, nil); err != nil {
			return fmt.Errorf("queue: bind queue %q to exchange %q: %w", queueName, exchange, err)
		}
	}
	return nil
}

func validateTopology(t Topology) error {
	exchange := strings.TrimSpace(t.Exchange)
	queueName := strings.TrimSpace(t.Queue)
	if exchange == "" && queueName == "" {
		return fmt.Errorf("queue: declare: exchange or queue is required")
	}
	if queueName != "" && !t.Durable && !t.Exclusive {
		return fmt.Errorf("queue: declare: non-durable queues must be exclusive")
	}
	return nil
}
