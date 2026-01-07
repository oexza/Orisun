//go:build integration
// +build integration

package orisun

import (
	"context"
	"testing"
	"time"

	eventstore "github.com/oexza/Orisun/clients/go/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests require Orisun to be running on localhost:5005
// Run with: go test -tags=integration ./...

func TestClientIntegration_Ping(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.
		WithServer("127.0.0.1", 5005).
		WithBasicAuth("admin", "changeit").
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Ping(ctx)
	assert.NoError(t, err, "Ping should succeed with running server")
}

func TestClientIntegration_HealthCheck(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.
		WithServer("127.0.0.1", 5005).
		WithBasicAuth("admin", "changeit").
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	healthy, err := client.HealthCheck(ctx, "orisun_test_1")
	assert.NoError(t, err, "HealthCheck should succeed with running server")
	assert.True(t, healthy, "Server should be healthy")
}

func TestClientIntegration_SaveAndGetEvents(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.
		WithServer("127.0.0.1", 5005).
		WithBasicAuth("admin", "changeit").
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Save an event
	saveRequest := &eventstore.SaveEventsRequest{
		Boundary: "orisun_test_1",
		Query: &eventstore.Query{
			ExpectedPosition: &eventstore.ExpectedPosition{
				TransactionId: -1,
				GlobalId:      -1,
			},
		},
		Events: []*eventstore.EventToSave{
			{
				EventId:   "integration-test-001",
				EventType: "TestEvent",
				Data:      "{\"message\": \"Integration test event\"}",
				Metadata:  "{\"source\": \"integration-test\"}",
			},
		},
	}

	result, err := client.SaveEvents(ctx, saveRequest)
	require.NoError(t, err, "SaveEvents should succeed")
	require.NotNil(t, result)

	// Get the event back
	getRequest := &eventstore.GetEventsRequest{
		Boundary: "orisun_test_1",
		Count:    10,
	}

	response, err := client.GetEvents(ctx, getRequest)
	require.NoError(t, err, "GetEvents should succeed")
	assert.NotEmpty(t, response.Events, "Should return at least one event")
	assert.Equal(t, "integration-test-001", response.Events[0].EventId)
}

func TestClientIntegration_Subscribe(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.
		WithServer("127.0.0.1", 5005).
		WithBasicAuth("admin", "changeit").
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Track events received
	var receivedEvents []*eventstore.Event

	handler := NewSimpleEventHandler().
		WithOnEvent(func(event *eventstore.Event) error {
			receivedEvents = append(receivedEvents, event)
			// Cancel after first event
			cancel()
			return nil
		}).
		WithOnError(func(err error) {
			t.Logf("Subscription error: %v", err)
		})

	subRequest := &eventstore.CatchUpSubscribeToEventStoreRequest{
		Boundary:       "orisun_test_1",
		SubscriberName: "integration-test-subscriber",
		AfterPosition: &eventstore.Position{
			TransactionId: -1,
			GlobalId:      -1,
		},
	}

	subscription, err := client.SubscribeToEvents(ctx, subRequest, handler)
	require.NoError(t, err, "SubscribeToEvents should succeed")
	defer subscription.Close()

	// Wait a bit for events
	time.Sleep(2 * time.Second)

	// We should receive at least some events (or connection established)
	assert.NotNil(t, subscription)
}
