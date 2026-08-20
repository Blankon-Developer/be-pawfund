package queue

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Config holds the configuration required to open a RabbitMQ connection.
type Config struct {
	URL            string
	Logger         *slog.Logger
	ConnectionName string
}

// QueueClient wraps an AMQP connection and provides automatic reconnection.
//
// Contract for callers (publishers/consumers built on top of this client):
//   - Always fetch the connection via Conn() right before opening a channel;
//     never cache the returned *amqp.Connection, or a *amqp.Channel derived
//     from it, across a reconnect. Both become permanently unusable once the
//     connection they belong to is replaced.
//   - Never call Close() on the connection returned by Conn() directly. A
//     clean AMQP close is indistinguishable from an intentional shutdown, so
//     the reconnect loop will not redial after it. Call QueueClient.Close to
//     shut the client down.
//   - Use Notify to be woken up when a new connection becomes available
//     after a drop, so channels/consumers can be re-established.
type QueueClient struct {
	url            string
	connectionName string
	logger         *slog.Logger

	mu         sync.RWMutex
	conn       *amqp.Connection
	closed     bool
	notifySubs []chan struct{}

	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// Open dials RabbitMQ using the provided AMQP URL, verifies connectivity, and
// returns a QueueClient. A background goroutine watches for connection drops
// and re-dials with exponential back-off for the lifetime of the client.
//
// ctx bounds only the initial dial performed by Open (e.g. an application
// startup timeout); it has no effect on later reconnect attempts.
func Open(ctx context.Context, cfg Config) (*QueueClient, error) {
	rawURL := strings.TrimSpace(cfg.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("queue: URL is required")
	}

	connectionName := strings.TrimSpace(cfg.ConnectionName)
	conn, err := dialContext(ctx, rawURL, connectionName)
	if err != nil {
		return nil, fmt.Errorf("queue: dial: %w", err)
	}

	client := &QueueClient{
		url:            rawURL,
		connectionName: connectionName,
		conn:           conn,
		done:           make(chan struct{}),
		logger:         resolveLogger(cfg.Logger),
	}

	go client.reconnectLoop()

	return client, nil
}

// resolveLogger returns logger, falling back to slog.Default() when nil.
func resolveLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

const defaultConnectionTimeout = 30 * time.Second

// dialContext dials rawURL while applying ctx and the AMQP connection timeout
// to both TCP establishment and the TLS/AMQP handshake.
func dialContext(ctx context.Context, rawURL, connectionName string) (*amqp.Connection, error) {
	timeout, err := connectionTimeout(rawURL)
	if err != nil {
		return nil, err
	}

	config := connectionConfig(connectionName)
	var rawConn net.Conn
	stopWatching := func() {} // replaced with a real stopper once Dial succeeds

	config.Dial = func(network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		if err := conn.SetDeadline(handshakeDeadline(ctx, timeout)); err != nil {
			_ = conn.Close()
			return nil, err
		}

		rawConn = conn
		stopWatching = watchForCancel(ctx, conn)
		return conn, nil
	}

	conn, err := amqp.DialConfig(rawURL, config)
	stopWatching()

	if err != nil {
		if rawConn != nil {
			_ = rawConn.Close()
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = conn.Close()
		return nil, ctxErr
	}
	return conn, nil
}

// handshakeDeadline returns the deadline to apply to the raw connection while
// the TLS/AMQP handshake is in flight: timeout from now, capped by ctx's own
// deadline if that arrives sooner.
func handshakeDeadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}

// watchForCancel spawns a goroutine that forces conn's deadline to expire the
// instant ctx is canceled, so a canceled dial doesn't have to wait out the
// full handshake deadline set by the caller. The returned stop func must be
// called exactly once, after the dial finishes, to release the goroutine.
func watchForCancel(ctx context.Context, conn net.Conn) func() {
	stopped := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-stopped:
		}
	}()

	return func() {
		close(stopped)
		<-done
	}
}

func connectionTimeout(rawURL string) (time.Duration, error) {
	uri, err := amqp.ParseURI(rawURL)
	if err != nil {
		return 0, err
	}
	if uri.ConnectionTimeout > 0 {
		return time.Duration(uri.ConnectionTimeout) * time.Millisecond, nil
	}
	return defaultConnectionTimeout, nil
}

func connectionConfig(connectionName string) amqp.Config {
	config := amqp.Config{}
	if connectionName != "" {
		config.Properties = amqp.NewConnectionProperties()
		config.Properties.SetClientConnectionName(connectionName)
	}
	return config
}

// Conn returns the current active AMQP connection. Callers (publishers,
// consumers) use this to open channels; see the QueueClient contract above.
func (q *QueueClient) Conn() *amqp.Connection {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.conn
}

// Notify returns a channel that receives a value each time the client
// successfully re-establishes a connection after an unexpected drop. It
// never fires for the initial connection established by Open. The channel
// is closed once Close is called.
//
// The channel is buffered with size 1 and delivery is non-blocking: if a
// reconnect happens while a previous notification is still unread, the new
// one is dropped rather than blocking the reconnect loop. Treat a received
// value as "check Conn() again", not as a guaranteed one-notification-per-
// reconnect delivery.
//
// Notify is meant to be called once per long-lived subscriber (e.g. once by
// each consumer goroutine at startup); every call registers a subscription
// that lives for the lifetime of the client.
func (q *QueueClient) Notify() <-chan struct{} {
	ch := make(chan struct{}, 1)

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		close(ch)
		return ch
	}

	q.notifySubs = append(q.notifySubs, ch)

	return ch
}

// Close signals the reconnect loop to stop and closes the AMQP connection.
// It is safe to call multiple times: only the first call does any work, and
// every call (including the first) returns the same result.
func (q *QueueClient) Close() error {
	q.closeOnce.Do(func() {
		q.mu.Lock()
		q.closed = true
		conn := q.conn
		for _, sub := range q.notifySubs {
			close(sub)
		}
		q.notifySubs = nil
		q.mu.Unlock()

		close(q.done)

		if conn != nil && !conn.IsClosed() {
			if err := conn.Close(); err != nil {
				q.closeErr = fmt.Errorf("queue: close connection: %w", err)
			}
		}
	})
	return q.closeErr
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

			newConn, err := amqp.DialConfig(q.url, connectionConfig(q.connectionName))
			if err != nil {
				q.logger.Warn("queue: reconnect attempt failed",
					"error", err,
					"next_delay", delay)
				delay = min(delay*factor, maxDelay)
				continue
			}

			// Atomically check the shutdown flag and swap in the new
			// connection under the same lock, so a concurrent Close() can
			// never race with this assignment and leave newConn dangling.
			q.mu.Lock()
			if q.closed {
				q.mu.Unlock()
				_ = newConn.Close()
				return
			}
			q.conn = newConn
			q.mu.Unlock()

			q.logger.Info("queue: reconnected successfully")
			q.notifyReconnect()
			break
		}
	}
}

// notifyReconnect delivers a non-blocking notification to every subscriber
// registered via Notify.
func (q *QueueClient) notifyReconnect() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}
	for _, sub := range q.notifySubs {
		select {
		case sub <- struct{}{}:
		default:
		}
	}
}
