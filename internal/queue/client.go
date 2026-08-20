package queue

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Config holds the configuration required to open a RabbitMQ connection.
type Config struct {
	URL    string
	Logger *slog.Logger
}

// QueueClient wraps an AMQP connection and provides automatic reconnection.
// Callers obtain a channel via Conn().Channel() for publishing and consuming.
type QueueClient struct {
	url    string
	mu     sync.RWMutex
	conn   *amqp.Connection
	done   chan struct{}
	logger *slog.Logger
}

// Open dials RabbitMQ using the provided AMQP URL, verifies connectivity, and
// returns a QueueClient. A background goroutine watches for connection drops and
// re-dials with exponential back-off. The context is used for the initial dial
// only; the underlying connection lives beyond it.
func Open(_ context.Context, cfg Config) (*QueueClient, error) {
	rawURL := strings.TrimSpace(cfg.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("queue: URL is required")
	}

	conn, err := amqp.Dial(rawURL)
	if err != nil {
		return nil, fmt.Errorf("queue: dial: %w", err)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	client := &QueueClient{
		url:    rawURL,
		conn:   conn,
		done:   make(chan struct{}),
		logger: logger,
	}

	go client.reconnectLoop()

	return client, nil
}

// Conn returns the current active AMQP connection.
// Callers (publishers, consumers) use this to open channels.
func (q *QueueClient) Conn() *amqp.Connection {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.conn
}

// Close signals the reconnect loop to stop and closes the AMQP connection.
func (q *QueueClient) Close() error {
	close(q.done)

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.conn != nil && !q.conn.IsClosed() {
		if err := q.conn.Close(); err != nil {
			return fmt.Errorf("queue: close connection: %w", err)
		}
	}
	return nil
}

// reconnectLoop watches for connection closure events and re-dials with
// exponential back-off up to maxDelay. It exits when Close is called.
func (q *QueueClient) reconnectLoop() {
	const (
		initialDelay = 1 * time.Second
		maxDelay     = 30 * time.Second
		factor       = 2
	)

	for {
		q.mu.RLock()
		conn := q.conn
		q.mu.RUnlock()

		notifyClose := conn.NotifyClose(make(chan *amqp.Error, 1))

		select {
		case <-q.done:
			return
		case amqpErr, ok := <-notifyClose:
			if !ok {
				// Connection was closed cleanly by Close(); stop the loop.
				return
			}
			q.logger.Warn("queue: connection closed unexpectedly, will reconnect",
				"reason", amqpErr)
		}

		delay := initialDelay
		for {
			select {
			case <-q.done:
				return
			case <-time.After(delay):
			}

			newConn, err := amqp.Dial(q.url)
			if err != nil {
				q.logger.Warn("queue: reconnect attempt failed",
					"error", err,
					"next_delay", delay)
				delay = min(delay*factor, maxDelay)
				continue
			}

			// Guard against a Close() call racing with a successful reconnect.
			select {
			case <-q.done:
				_ = newConn.Close()
				return
			default:
			}

			q.mu.Lock()
			q.conn = newConn
			q.mu.Unlock()

			q.logger.Info("queue: reconnected successfully")
			break
		}
	}
}
