package queue

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestOpenRequiresURL(t *testing.T) {
	_, err := Open(context.Background(), Config{URL: "   "})
	if err == nil {
		t.Fatal("Open() expected error for empty URL")
	}
}

func TestResolveLogger(t *testing.T) {
	if got := resolveLogger(nil); got != slog.Default() {
		t.Error("resolveLogger(nil) did not fall back to slog.Default()")
	}

	custom := slog.New(slog.NewTextHandler(io.Discard, nil))
	if got := resolveLogger(custom); got != custom {
		t.Error("resolveLogger(custom) did not return the provided logger")
	}
}

func TestDialContextCancellationInterruptsHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	serverDone := make(chan struct{})
	defer close(serverDone)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-serverDone
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err = dialContext(ctx, "amqp://guest:guest@"+listener.Addr().String()+"/", "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dialContext() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("dialContext() returned after %s, want at most 1s", elapsed)
	}
}

func TestConnectionConfigSetsClientConnectionName(t *testing.T) {
	config := connectionConfig("pawfund-test")
	if got := config.Properties["connection_name"]; got != "pawfund-test" {
		t.Fatalf("connection_name = %v, want pawfund-test", got)
	}
}

func TestQueueClientCloseIsIdempotent(t *testing.T) {
	client := &QueueClient{
		done:   make(chan struct{}),
		logger: slog.Default(),
	}

	if err := client.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestQueueClientCloseClosesNotifySubscribers(t *testing.T) {
	client := &QueueClient{
		done:   make(chan struct{}),
		logger: slog.Default(),
	}
	sub := client.Notify()

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case _, ok := <-sub:
		if ok {
			t.Fatal("expected notify channel to be closed, got a value instead")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notify channel to close")
	}
}

func TestQueueClientNotifyAfterCloseReturnsClosedChannel(t *testing.T) {
	client := &QueueClient{
		done:   make(chan struct{}),
		logger: slog.Default(),
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	sub := client.Notify()
	select {
	case _, ok := <-sub:
		if ok {
			t.Fatal("expected notify channel to be closed")
		}
	default:
		t.Fatal("expected notify channel obtained after Close to already be closed")
	}
}

func TestQueueClientConcurrentNotifyAndCloseClosesEverySubscriber(t *testing.T) {
	client := &QueueClient{done: make(chan struct{}), logger: slog.Default()}

	type notifyChannel <-chan struct{}

	const subscriberCount = 100
	start := make(chan struct{})
	subs := make(chan notifyChannel, subscriberCount)
	for range subscriberCount {
		go func() {
			<-start
			subs <- client.Notify()
		}()
	}

	close(start)
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for range subscriberCount {
		sub := <-subs
		select {
		case _, ok := <-sub:
			if ok {
				t.Fatal("expected notify channel to be closed")
			}
		case <-time.After(time.Second):
			t.Fatal("notify channel remained open after concurrent Close")
		}
	}
}

func TestQueueClientNotifyReconnectBroadcastsToSubscribers(t *testing.T) {
	client := &QueueClient{done: make(chan struct{}), logger: slog.Default()}

	subs := map[string]<-chan struct{}{
		"subA": client.Notify(),
		"subB": client.Notify(),
	}

	client.notifyReconnect()

	for name, sub := range subs {
		select {
		case <-sub:
		default:
			t.Fatalf("%s did not receive a reconnect notification", name)
		}
	}
}

func TestQueueClientNotifyReconnectDropsWhenSubscriberBufferFull(t *testing.T) {
	client := &QueueClient{done: make(chan struct{}), logger: slog.Default()}
	sub := client.Notify()

	client.notifyReconnect() // fills the size-1 buffer

	done := make(chan struct{})
	go func() {
		client.notifyReconnect() // must not block despite the full buffer
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notifyReconnect blocked while a subscriber's buffer was full")
	}

	<-sub // drain the single buffered notification
}
