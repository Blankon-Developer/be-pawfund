package queue

import (
	"strings"
	"testing"
)

func TestDeclareRejectsEmptyTopology(t *testing.T) {
	err := Declare(nil, Topology{})
	if err == nil {
		t.Fatal("Declare() expected error for empty topology")
	}
}

func TestDeclareRejectsNonDurableNonExclusiveQueue(t *testing.T) {
	err := Declare(nil, Topology{Queue: "jobs"})
	if err == nil {
		t.Fatal("Declare() expected error for non-durable non-exclusive queue")
	}
}

func TestDeclareAllowsDurableQueue(t *testing.T) {
	err := Declare(nil, Topology{Queue: "jobs", Durable: true})
	if err == nil {
		t.Fatal("Declare() expected error for nil channel")
	}
	if got, want := err.Error(), "queue: declare: channel is required"; got != want {
		t.Fatalf("Declare() error = %q, want %q", got, want)
	}
}

func TestDeclareAllowsExclusiveTransientQueue(t *testing.T) {
	err := Declare(nil, Topology{Queue: "jobs", Exclusive: true})
	if err == nil {
		t.Fatal("Declare() expected error for nil channel")
	}
	if got, want := err.Error(), "queue: declare: channel is required"; got != want {
		t.Fatalf("Declare() error = %q, want %q", got, want)
	}
}

func TestDeclareOnRequiresClient(t *testing.T) {
	err := DeclareOn(nil, Topology{Queue: "jobs", Durable: true})
	if err == nil {
		t.Fatal("DeclareOn() expected error for nil client")
	}
	if !strings.Contains(err.Error(), "client is required") {
		t.Fatalf("DeclareOn() error = %v, want client required", err)
	}
}

func TestDeclareOnRequiresConnection(t *testing.T) {
	err := DeclareOn(&QueueClient{}, Topology{Queue: "jobs", Durable: true})
	if err == nil {
		t.Fatal("DeclareOn() expected error when connection is missing")
	}
	if !strings.Contains(err.Error(), "connection is not available") {
		t.Fatalf("DeclareOn() error = %v, want connection not available", err)
	}
}

func TestValidateTopologyTrimsNames(t *testing.T) {
	if err := validateTopology(Topology{Queue: "  jobs  ", Durable: true}); err != nil {
		t.Fatalf("validateTopology() unexpected error: %v", err)
	}
	if err := validateTopology(Topology{Exchange: "  events  "}); err != nil {
		t.Fatalf("validateTopology() unexpected error: %v", err)
	}
}
