package queue

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewConsumerRequiresClient(t *testing.T) {
	_, err := NewConsumer(nil, ConsumerConfig{Queue: "jobs"})
	if err == nil {
		t.Fatal("NewConsumer() expected error for nil client")
	}
}

func TestNewConsumerRequiresQueue(t *testing.T) {
	_, err := NewConsumer(&QueueClient{}, ConsumerConfig{Queue: "  "})
	if err == nil {
		t.Fatal("NewConsumer() expected error for empty queue")
	}
}

func TestNewConsumerDefaultsPrefetch(t *testing.T) {
	consumer, err := NewConsumer(&QueueClient{}, ConsumerConfig{Queue: "jobs"})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	if consumer.prefetch != 1 {
		t.Fatalf("prefetch = %d, want 1", consumer.prefetch)
	}

	consumer, err = NewConsumer(&QueueClient{}, ConsumerConfig{Queue: "jobs", Prefetch: -3})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	if consumer.prefetch != 1 {
		t.Fatalf("prefetch = %d, want 1", consumer.prefetch)
	}

	consumer, err = NewConsumer(&QueueClient{}, ConsumerConfig{Queue: "jobs", Prefetch: 8})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	if consumer.prefetch != 8 {
		t.Fatalf("prefetch = %d, want 8", consumer.prefetch)
	}
}

func TestNewConsumerRejectsTopologyQueueMismatch(t *testing.T) {
	_, err := NewConsumer(&QueueClient{}, ConsumerConfig{
		Queue:    "jobs",
		Topology: &Topology{Queue: "other", Exclusive: true},
	})
	if err == nil {
		t.Fatal("NewConsumer() expected error for topology queue mismatch")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("NewConsumer() error = %v, want queue mismatch", err)
	}
}

func TestNewConsumerFillsTopologyQueueFromConfig(t *testing.T) {
	consumer, err := NewConsumer(&QueueClient{}, ConsumerConfig{
		Queue:    "jobs",
		Topology: &Topology{Exclusive: true},
	})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	if consumer.topology == nil || consumer.topology.Queue != "jobs" {
		t.Fatalf("topology queue = %v, want jobs", consumer.topology)
	}
}

func TestNewConsumerRejectsInvalidTopology(t *testing.T) {
	_, err := NewConsumer(&QueueClient{}, ConsumerConfig{
		Queue:    "jobs",
		Topology: &Topology{},
	})
	if err == nil {
		t.Fatal("NewConsumer() expected error for non-durable non-exclusive topology")
	}
}

func TestNewConsumerSubscribesToNotifyOnce(t *testing.T) {
	client := &QueueClient{done: make(chan struct{}), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	consumer, err := NewConsumer(client, ConsumerConfig{
		Queue:  "jobs",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	if got := len(client.notifySubs); got != 1 {
		t.Fatalf("notify subscribers after NewConsumer = %d, want 1", got)
	}

	runOnce := func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		go func() {
			done <- consumer.Run(ctx, func(context.Context, Delivery) error { return nil })
		}()
		waitForRunning(t, consumer)
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Run() error = %v, want context canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for Run to return")
		}
	}

	runOnce()
	runOnce()

	if got := len(client.notifySubs); got != 1 {
		t.Fatalf("notify subscribers after restarted Run = %d, want 1", got)
	}
}

func TestRunRequiresHandler(t *testing.T) {
	consumer, err := NewConsumer(&QueueClient{}, ConsumerConfig{Queue: "jobs"})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	err = consumer.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("Run() expected error for nil handler")
	}
}

func TestRunRejectsConcurrentCalls(t *testing.T) {
	client := &QueueClient{done: make(chan struct{}), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	consumer, err := NewConsumer(client, ConsumerConfig{
		Queue:  "jobs",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- consumer.Run(ctx, func(context.Context, Delivery) error { return nil })
	}()
	<-started

	waitForRunning(t, consumer)

	err = consumer.Run(ctx, func(context.Context, Delivery) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("Run() error = %v, want already running", err)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Run to return")
	}
}

func waitForRunning(t *testing.T, consumer *Consumer) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if consumer.running.Load() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for consumer to start")
}
