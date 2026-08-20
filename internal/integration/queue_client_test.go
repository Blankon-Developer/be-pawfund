//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/queue"
)

func TestQueueClientConnectsAndDeclaresQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	client, err := queue.Open(ctx, queue.Config{URL: testQueueURL})
	if err != nil {
		t.Fatalf("queue.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	channel, err := client.Conn().Channel()
	if err != nil {
		t.Fatalf("Conn().Channel() error = %v", err)
	}
	defer channel.Close()

	// Exclusive avoids RabbitMQ 4.x's deprecated transient_nonexcl_queues
	// feature, which forbids non-durable, non-exclusive queues by default.
	if _, err := channel.QueueDeclare(
		"pawfund-test-queue-client-connect",
		false, // durable
		true,  // autoDelete
		true,  // exclusive
		false, // noWait
		nil,
	); err != nil {
		t.Fatalf("QueueDeclare() error = %v", err)
	}
}

// TestQueueClientReconnectsAfterBrokerClosesConnection simulates an
// unexpected connection drop (as opposed to a graceful client-side Close)
// by force-closing the connection from the broker side via the RabbitMQ
// management API. It then asserts that the background reconnect loop
// re-establishes a working connection and notifies subscribers via Notify.
func TestQueueClientReconnectsAfterBrokerClosesConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	connectionName := fmt.Sprintf("pawfund-integration-%d", time.Now().UnixNano())
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

	notify := client.Notify()

	name, err := waitForManagementConnectionName(t, connectionName, 5*time.Second)
	if err != nil {
		t.Fatalf("find queue client management connection: %v", err)
	}

	if err := forceCloseManagementConnection(t, name); err != nil {
		t.Fatalf("force close connection via management API: %v", err)
	}

	select {
	case _, ok := <-notify:
		if !ok {
			t.Fatal("notify channel closed before reconnect")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for reconnect notification")
	}

	if client.Conn().IsClosed() {
		t.Fatal("expected an active connection after reconnect")
	}

	channel, err := client.Conn().Channel()
	if err != nil {
		t.Fatalf("Conn().Channel() after reconnect error = %v", err)
	}
	defer channel.Close()
}

type mgmtConnection struct {
	Name             string `json:"name"`
	ClientProperties struct {
		ConnectionName string `json:"connection_name"`
	} `json:"client_properties"`
}

func listManagementConnections(ctx context.Context) ([]mgmtConnection, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testQueueMgmtURL+"/api/connections", nil)
	if err != nil {
		return nil, fmt.Errorf("build list request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list connections: unexpected status %s", resp.Status)
	}

	var connections []mgmtConnection
	if err := json.NewDecoder(resp.Body).Decode(&connections); err != nil {
		return nil, fmt.Errorf("decode connections: %w", err)
	}
	return connections, nil
}

// waitForManagementConnectionName polls the management API until the client
// connection with the requested AMQP connection_name property becomes visible.
func waitForManagementConnectionName(t *testing.T, connectionName string, timeout time.Duration) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		connections, err := listManagementConnections(ctx)
		if err != nil {
			return "", err
		}
		for _, conn := range connections {
			if conn.ClientProperties.ConnectionName == connectionName {
				return conn.Name, nil
			}
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("connection %q not observed within %s: %w", connectionName, timeout, ctx.Err())
		case <-ticker.C:
		}
	}
}

// forceCloseManagementConnection force-closes the named connection via the
// RabbitMQ management API, simulating a broker-initiated drop (e.g. broker
// restart, network blip) rather than a client-initiated shutdown.
func forceCloseManagementConnection(t *testing.T, name string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		testQueueMgmtURL+"/api/connections/"+url.PathEscape(name),
		nil,
	)
	if err != nil {
		return fmt.Errorf("build delete request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete connection: unexpected status %s", resp.Status)
	}

	return nil
}
