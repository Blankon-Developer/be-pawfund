//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/infra/queue"
)

func TestPublisherConsumerRoundTrip(t *testing.T) {
	client := openTestQueueClient(t, "")
	publisher := queue.NewPublisher(client)

	exchange := uniqueQueueName("exchange")
	queueName := uniqueQueueName("queue")
	bindingKey := "campaign.created"
	declareTopology(t, client, queue.Topology{
		Exchange:   exchange,
		Queue:      queueName,
		BindingKey: bindingKey,
		Exclusive:  true,
		AutoDelete: true,
	})

	consumer, err := queue.NewConsumer(client, queue.ConsumerConfig{Queue: queueName})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	got := make(chan queue.Delivery, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- consumer.Run(ctx, func(_ context.Context, d queue.Delivery) error {
			got <- d
			return nil
		})
	}()

	body := []byte(`{"id":"round-trip"}`)
	if err := publisher.Publish(ctx, queue.Message{
		Exchange:    exchange,
		RoutingKey:  bindingKey,
		Body:        body,
		ContentType: "application/json",
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	delivery := waitDelivery(t, got, 10*time.Second)
	if string(delivery.Body) != string(body) {
		t.Fatalf("body = %q, want %q", delivery.Body, body)
	}
	if delivery.RoutingKey != bindingKey {
		t.Fatalf("routing key = %q, want %q", delivery.RoutingKey, bindingKey)
	}
	if delivery.Exchange != exchange {
		t.Fatalf("exchange = %q, want %q", delivery.Exchange, exchange)
	}

	cancel()
	waitConsumerStop(t, errCh)
}

func TestConsumerRunReturnsOnCancel(t *testing.T) {
	client := openTestQueueClient(t, "")
	queueName := uniqueQueueName("cancel")
	declareTopology(t, client, queue.Topology{
		Queue:      queueName,
		Exclusive:  true,
		AutoDelete: true,
	})

	consumer, err := queue.NewConsumer(client, queue.ConsumerConfig{Queue: queueName})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		errCh <- consumer.Run(ctx, func(context.Context, queue.Delivery) error { return nil })
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()
	waitConsumerStop(t, errCh)
}

func TestPublisherPublishesAfterReconnect(t *testing.T) {
	connectionName := fmt.Sprintf("pawfund-integration-pub-%d", time.Now().UnixNano())
	client := openTestQueueClient(t, connectionName)
	publisher := queue.NewPublisher(client)
	notify := client.Notify()

	queueName := uniqueQueueName("pub-reconnect")
	declareTopology(t, client, queue.Topology{Queue: queueName, Durable: true})
	cleanupDurableTopology(t, client, queueName, "")

	name, err := waitForManagementConnectionName(t, connectionName, 5*time.Second)
	if err != nil {
		t.Fatalf("find queue client management connection: %v", err)
	}
	if err := forceCloseManagementConnection(t, name); err != nil {
		t.Fatalf("force close connection via management API: %v", err)
	}
	waitReconnect(t, notify)

	body := []byte("after-reconnect")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := publisher.Publish(ctx, queue.Message{RoutingKey: queueName, Body: body}); err != nil {
		t.Fatalf("Publish() after reconnect error = %v", err)
	}

	channel, err := client.Conn().Channel()
	if err != nil {
		t.Fatalf("Conn().Channel() after reconnect error = %v", err)
	}
	defer channel.Close()

	msg, ok, err := channel.Get(queueName, true)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() returned no message")
	}
	if string(msg.Body) != string(body) {
		t.Fatalf("body = %q, want %q", msg.Body, body)
	}
}

func TestConsumerResumesAfterReconnect(t *testing.T) {
	connectionName := fmt.Sprintf("pawfund-integration-con-%d", time.Now().UnixNano())
	client := openTestQueueClient(t, connectionName)
	publisher := queue.NewPublisher(client)
	notify := client.Notify()

	queueName := uniqueQueueName("con-reconnect")
	declareTopology(t, client, queue.Topology{Queue: queueName, Durable: true})
	cleanupDurableTopology(t, client, queueName, "")

	consumer, err := queue.NewConsumer(client, queue.ConsumerConfig{Queue: queueName})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	got := make(chan queue.Delivery, 2)
	errCh := make(chan error, 1)
	go func() {
		errCh <- consumer.Run(ctx, func(_ context.Context, d queue.Delivery) error {
			got <- d
			return nil
		})
	}()

	before := []byte("before-drop")
	if err := publisher.Publish(ctx, queue.Message{RoutingKey: queueName, Body: before}); err != nil {
		t.Fatalf("Publish() before drop error = %v", err)
	}
	delivery := waitDelivery(t, got, 10*time.Second)
	if string(delivery.Body) != string(before) {
		t.Fatalf("body = %q, want %q", delivery.Body, before)
	}

	name, err := waitForManagementConnectionName(t, connectionName, 5*time.Second)
	if err != nil {
		t.Fatalf("find queue client management connection: %v", err)
	}
	if err := forceCloseManagementConnection(t, name); err != nil {
		t.Fatalf("force close connection via management API: %v", err)
	}
	waitReconnect(t, notify)

	after := []byte("after-reconnect")
	if err := publisher.Publish(ctx, queue.Message{RoutingKey: queueName, Body: after}); err != nil {
		t.Fatalf("Publish() after reconnect error = %v", err)
	}
	delivery = waitDelivery(t, got, 20*time.Second)
	if string(delivery.Body) != string(after) {
		t.Fatalf("body = %q, want %q", delivery.Body, after)
	}

	cancel()
	waitConsumerStop(t, errCh)
}

func TestConsumerResumesAfterReconnectExclusiveQueue(t *testing.T) {
	connectionName := fmt.Sprintf("pawfund-integration-excl-%d", time.Now().UnixNano())
	client := openTestQueueClient(t, connectionName)
	publisher := queue.NewPublisher(client)
	notify := client.Notify()

	exchange := uniqueQueueName("excl-exchange")
	queueName := uniqueQueueName("excl-queue")
	bindingKey := "campaign.updated"
	topology := queue.Topology{
		Exchange:   exchange,
		Queue:      queueName,
		BindingKey: bindingKey,
		Exclusive:  true,
		AutoDelete: true,
	}
	declareTopology(t, client, topology)

	consumer, err := queue.NewConsumer(client, queue.ConsumerConfig{
		Queue:    queueName,
		Topology: &topology,
	})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	got := make(chan queue.Delivery, 8)
	errCh := make(chan error, 1)
	go func() {
		errCh <- consumer.Run(ctx, func(_ context.Context, d queue.Delivery) error {
			got <- d
			return nil
		})
	}()

	before := []byte("before-drop")
	if err := publisher.Publish(ctx, queue.Message{
		Exchange:   exchange,
		RoutingKey: bindingKey,
		Body:       before,
	}); err != nil {
		t.Fatalf("Publish() before drop error = %v", err)
	}
	delivery := waitDelivery(t, got, 10*time.Second)
	if string(delivery.Body) != string(before) {
		t.Fatalf("body = %q, want %q", delivery.Body, before)
	}

	name, err := waitForManagementConnectionName(t, connectionName, 5*time.Second)
	if err != nil {
		t.Fatalf("find queue client management connection: %v", err)
	}
	if err := forceCloseManagementConnection(t, name); err != nil {
		t.Fatalf("force close connection via management API: %v", err)
	}
	waitReconnect(t, notify)

	delivery = publishUntilDelivered(t, publisher, ctx, queue.Message{
		Exchange:   exchange,
		RoutingKey: bindingKey,
	}, got, 20*time.Second)
	if !strings.HasPrefix(string(delivery.Body), "after-reconnect-") {
		t.Fatalf("body = %q, want after-reconnect prefix", delivery.Body)
	}

	cancel()
	waitConsumerStop(t, errCh)
}

func openTestQueueClient(t *testing.T, connectionName string) *queue.QueueClient {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	if connectionName == "" {
		connectionName = fmt.Sprintf("pawfund-integration-%d", time.Now().UnixNano())
	}
	client, err := queue.Open(ctx, queue.Config{
		URL:            testQueueURL,
		ConnectionName: connectionName,
	})
	if err != nil {
		t.Fatalf("queue.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return client
}

func declareTopology(t *testing.T, client *queue.QueueClient, topology queue.Topology) {
	t.Helper()

	channel, err := client.Conn().Channel()
	if err != nil {
		t.Fatalf("Conn().Channel() error = %v", err)
	}
	defer channel.Close()

	if err := queue.Declare(channel, topology); err != nil {
		t.Fatalf("Declare() error = %v", err)
	}
}

func cleanupDurableTopology(t *testing.T, client *queue.QueueClient, queueName, exchange string) {
	t.Helper()
	t.Cleanup(func() {
		conn := client.Conn()
		if conn == nil || conn.IsClosed() {
			return
		}
		channel, err := conn.Channel()
		if err != nil {
			return
		}
		defer channel.Close()
		if queueName != "" {
			_, _ = channel.QueueDelete(queueName, false, false, false)
		}
		if exchange != "" {
			_ = channel.ExchangeDelete(exchange, false, false)
		}
	})
}

func uniqueQueueName(prefix string) string {
	return fmt.Sprintf("pawfund-test-%s-%d", prefix, time.Now().UnixNano())
}

func publishUntilDelivered(t *testing.T, publisher *queue.Publisher, ctx context.Context, msg queue.Message, got <-chan queue.Delivery, timeout time.Duration) queue.Delivery {
	t.Helper()

	deadline := time.Now().Add(timeout)
	outstanding := make(map[string]struct{})
	for n := 1; time.Now().Before(deadline); n++ {
		body := fmt.Sprintf("after-reconnect-%d", n)
		outstanding[body] = struct{}{}
		msg.Body = []byte(body)
		if err := publisher.Publish(ctx, msg); err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		select {
		case d := <-got:
			if _, ok := outstanding[string(d.Body)]; ok {
				return d
			}
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for delivery after reconnect")
	return queue.Delivery{}
}

func waitDelivery(t *testing.T, got <-chan queue.Delivery, timeout time.Duration) queue.Delivery {
	t.Helper()
	select {
	case d := <-got:
		return d
	case <-time.After(timeout):
		t.Fatal("timed out waiting for delivery")
	}
	return queue.Delivery{}
}

func waitReconnect(t *testing.T, notify <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-notify:
		if !ok {
			t.Fatal("notify channel closed before reconnect")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for reconnect notification")
	}
}

func waitConsumerStop(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context canceled or nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for consumer to stop")
	}
}
