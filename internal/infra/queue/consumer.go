package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	amqp "github.com/rabbitmq/amqp091-go"
)

// errConsumerRetry tells Run to drop the current channel and start a new
// consume session from the current Conn().
var errConsumerRetry = errors.New("queue: consume: retry session")

// Delivery is the application-facing view of a consumed message. It does not
// expose AMQP acknowledgement APIs; Run Acks or Nacks based on the handler result.
type Delivery struct {
	Body       []byte
	RoutingKey string
	Exchange   string
	MessageID  string
}

type Handler func(ctx context.Context, d Delivery) error

type ConsumerConfig struct {
	Queue string
	// Topology, if set, is declared at the start of every consume session.
	// Exclusive and auto-delete queues vanish with the connection, so
	// callers using those flags must set Topology for Run to survive reconnect.
	Topology *Topology
	Tag      string
	Prefetch int
	Logger   *slog.Logger
}

// Consumer consumes from a single queue. It re-opens a channel after the
// underlying QueueClient reconnects, re-declaring Topology when it was set.
// Poison-message handling (DLX, retry limits) is not implemented: handler
// errors Nack with requeue=true.
type Consumer struct {
	client   *QueueClient
	queue    string
	tag      string
	prefetch int
	logger   *slog.Logger
	topology *Topology
	notify   <-chan struct{}
	running  atomic.Bool
}

func NewConsumer(client *QueueClient, cfg ConsumerConfig) (*Consumer, error) {
	if client == nil {
		return nil, fmt.Errorf("queue: consume: client is required")
	}
	queueName := strings.TrimSpace(cfg.Queue)
	if queueName == "" {
		return nil, fmt.Errorf("queue: consume: queue is required")
	}

	topology, err := consumerTopology(queueName, cfg.Topology)
	if err != nil {
		return nil, err
	}

	prefetch := cfg.Prefetch
	if prefetch <= 0 {
		prefetch = 1
	}

	return &Consumer{
		client:   client,
		queue:    queueName,
		tag:      strings.TrimSpace(cfg.Tag),
		prefetch: prefetch,
		logger:   resolveLogger(cfg.Logger),
		topology: topology,
		notify:   client.Notify(),
	}, nil
}

func consumerTopology(queueName string, src *Topology) (*Topology, error) {
	if src == nil {
		return nil, nil
	}

	t := *src
	tQueue := strings.TrimSpace(t.Queue)
	switch {
	case tQueue == "":
		t.Queue = queueName
	case tQueue != queueName:
		return nil, fmt.Errorf("queue: consume: topology queue %q does not match %q", tQueue, queueName)
	default:
		t.Queue = tQueue
	}
	if err := validateTopology(t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Run delivers messages until ctx is cancelled. It is not safe to call Run
// concurrently on the same Consumer; a second call returns an error. After
// Run returns, it may be called again.
//
// On handler success the delivery is Acked. On handler error it is Nacked
// with requeue=true.
func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	if handler == nil {
		return fmt.Errorf("queue: consume: handler is required")
	}
	if !c.running.CompareAndSwap(false, true) {
		return fmt.Errorf("queue: consume: already running")
	}
	defer c.running.Store(false)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := c.runSession(ctx, handler)
		if errors.Is(err, errConsumerRetry) {
			continue
		}
		return err
	}
}

func (c *Consumer) runSession(ctx context.Context, handler Handler) error {
	conn, err := c.waitForConnection(ctx)
	if err != nil {
		return err
	}

	channel, err := conn.Channel()
	if err != nil {
		if connectionUnavailable(conn, err) {
			c.logger.Warn("queue: consumer channel open failed, will retry",
				"queue", c.queue, "error", err)
			return c.waitAndRetry(ctx)
		}
		return fmt.Errorf("queue: consume: open channel: %w", err)
	}
	defer channel.Close()

	sessionDone := make(chan struct{})
	defer close(sessionDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = channel.Close()
		case <-sessionDone:
		}
	}()

	if c.topology != nil {
		if err := Declare(channel, *c.topology); err != nil {
			if connectionUnavailable(conn, err) {
				c.logger.Warn("queue: consumer declare failed, will retry",
					"queue", c.queue, "error", err)
				return c.waitAndRetry(ctx)
			}
			return fmt.Errorf("queue: consume: declare: %w", err)
		}
	}

	if err := channel.Qos(c.prefetch, 0, false); err != nil {
		if connectionUnavailable(conn, err) {
			return c.waitAndRetry(ctx)
		}
		return fmt.Errorf("queue: consume: qos: %w", err)
	}

	deliveries, err := channel.Consume(c.queue, c.tag, false, false, false, false, nil)
	if err != nil {
		if connectionUnavailable(conn, err) {
			c.logger.Warn("queue: consumer consume failed, will retry",
				"queue", c.queue, "error", err)
			return c.waitAndRetry(ctx)
		}
		return fmt.Errorf("queue: consume: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-c.notify:
			if !ok {
				return fmt.Errorf("queue: consume: client closed")
			}
			c.logger.Info("queue: consumer re-establishing after reconnect", "queue", c.queue)
			return errConsumerRetry
		case d, ok := <-deliveries:
			if !ok {
				if err := ctx.Err(); err != nil {
					return err
				}
				c.logger.Warn("queue: consumer deliveries closed, will re-establish", "queue", c.queue)
				return c.waitAndRetry(ctx)
			}
			c.handleDelivery(ctx, d, handler)
		}
	}
}

func (c *Consumer) handleDelivery(ctx context.Context, d amqp.Delivery, handler Handler) {
	delivery := Delivery{
		Body:       d.Body,
		RoutingKey: d.RoutingKey,
		Exchange:   d.Exchange,
		MessageID:  d.MessageId,
	}

	if err := handler(ctx, delivery); err != nil {
		c.logger.Error("queue: consumer handler failed", "queue", c.queue, "error", err)
		if nackErr := d.Nack(false, true); nackErr != nil {
			c.logger.Warn("queue: nack failed", "queue", c.queue, "error", nackErr)
		}
		return
	}
	if ackErr := d.Ack(false); ackErr != nil {
		c.logger.Warn("queue: ack failed", "queue", c.queue, "error", ackErr)
	}
}

func (c *Consumer) waitForConnection(ctx context.Context) (*amqp.Connection, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn := c.client.Conn()
		if conn != nil && !conn.IsClosed() {
			return conn, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case _, ok := <-c.notify:
			if !ok {
				return nil, fmt.Errorf("queue: consume: client closed")
			}
		}
	}
}

func (c *Consumer) waitAndRetry(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	conn := c.client.Conn()
	if conn != nil && !conn.IsClosed() {
		return errConsumerRetry
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case _, ok := <-c.notify:
		if !ok {
			return fmt.Errorf("queue: consume: client closed")
		}
		return errConsumerRetry
	}
}

func connectionUnavailable(conn *amqp.Connection, err error) bool {
	if conn == nil || conn.IsClosed() {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, amqp.ErrClosed) {
		return true
	}
	var amqpErr *amqp.Error
	if errors.As(err, &amqpErr) {
		return amqpErr != nil && amqpErr.Code == amqp.ConnectionForced
	}
	return false
}
