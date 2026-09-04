package queue

import (
	"context"
	"strings"
	"testing"
)

func TestPublishRequiresClient(t *testing.T) {
	var publisher *Publisher
	err := publisher.Publish(context.Background(), Message{RoutingKey: "q", Body: []byte("x")})
	if err == nil {
		t.Fatal("Publish() expected error for nil publisher")
	}

	publisher = NewPublisher(nil)
	err = publisher.Publish(context.Background(), Message{RoutingKey: "q", Body: []byte("x")})
	if err == nil {
		t.Fatal("Publish() expected error for nil client")
	}
}

func TestPublishRequiresBody(t *testing.T) {
	publisher := NewPublisher(&QueueClient{})
	err := publisher.Publish(context.Background(), Message{RoutingKey: "q"})
	if err == nil {
		t.Fatal("Publish() expected error for nil body")
	}
	if !strings.Contains(err.Error(), "body is required") {
		t.Fatalf("Publish() error = %v, want body required", err)
	}
}

func TestPublishRequiresRoutingKey(t *testing.T) {
	publisher := NewPublisher(&QueueClient{})
	err := publisher.Publish(context.Background(), Message{RoutingKey: "  ", Body: []byte{}})
	if err == nil {
		t.Fatal("Publish() expected error for empty routing key")
	}
	if !strings.Contains(err.Error(), "routing key is required") {
		t.Fatalf("Publish() error = %v, want routing key required", err)
	}
}

func TestPublishRequiresAvailableConnection(t *testing.T) {
	publisher := NewPublisher(&QueueClient{})
	err := publisher.Publish(context.Background(), Message{RoutingKey: "q", Body: []byte("x")})
	if err == nil {
		t.Fatal("Publish() expected error when connection is missing")
	}
	if !strings.Contains(err.Error(), "connection is not available") {
		t.Fatalf("Publish() error = %v, want connection not available", err)
	}
}
