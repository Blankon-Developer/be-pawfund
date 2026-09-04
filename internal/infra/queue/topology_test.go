package queue

import "testing"

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

func TestValidateTopologyTrimsNames(t *testing.T) {
	if err := validateTopology(Topology{Queue: "  jobs  ", Durable: true}); err != nil {
		t.Fatalf("validateTopology() unexpected error: %v", err)
	}
	if err := validateTopology(Topology{Exchange: "  events  "}); err != nil {
		t.Fatalf("validateTopology() unexpected error: %v", err)
	}
}
