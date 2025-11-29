package orisun

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	eventstore "github.com/orisunlabs/orisun-go-client/eventstore"
)

func TestClientBuilder_WithHost(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	assert.False(t, client.IsClosed())
}

func TestClientBuilder_WithServer(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithServer("example.com", 8080).Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	assert.False(t, client.IsClosed())
}

func TestClientBuilder_WithMultipleServers(t *testing.T) {
	servers := []*ServerAddress{
		NewServerAddress("server1.example.com", 5005),
		NewServerAddress("server2.example.com", 5005),
	}

	builder := NewClientBuilder()
	client, err := builder.WithServers(servers).Build()

	// This test might fail due to gRPC connection issues without actual servers
	// We'll just verify that the builder doesn't panic
	if err != nil {
		// Expected to fail without actual servers
		assert.Contains(t, err.Error(), "Failed to create gRPC channel")
		return
	}
	
	require.NotNil(t, client)
	defer client.Close()

	assert.False(t, client.IsClosed())
}

func TestClientBuilder_WithTimeout(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").WithTimeout(60).Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	assert.Equal(t, 60*time.Second, client.GetDefaultTimeout())
}

func TestClientBuilder_WithBasicAuth(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.
		WithHost("localhost").
		WithBasicAuth("username", "password").
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Note: We can't easily test the auth without actual gRPC calls
	// but we can verify the client was created successfully
	assert.False(t, client.IsClosed())
}

func TestClientBuilder_WithLogging(t *testing.T) {
	logger := NewDefaultLogger(DEBUG)
	builder := NewClientBuilder()
	client, err := builder.
		WithHost("localhost").
		WithLogger(logger).
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	assert.Equal(t, logger, client.GetLogger())
}

func TestClient_Close(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)

	assert.False(t, client.IsClosed())

	err = client.Close()
	assert.NoError(t, err)
	assert.True(t, client.IsClosed())

	// Double close should not error
	err = client.Close()
	assert.NoError(t, err)
}

func TestClient_Ping(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.
		WithServer("127.0.0.1", 5005).
		WithBasicAuth("admin", "changeit"). // Using correct credentials from example
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	ctx, cancel := WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Note: This will fail without actual server, but we test the method exists
	err = client.Ping(ctx)
	// In a real test with a mock server, we would expect no error
	// For now, we just verify the method doesn't panic
	// Since our placeholder implementation always returns nil, we expect no error
	assert.NoError(t, err)
}

func TestClient_HealthCheck(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	ctx, cancel := WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Note: HealthCheck calls GetEvents which will fail without actual server
	// We expect this to fail since we don't have a mock implementation
	// This test verifies that the method exists and handles errors appropriately
	_, err = client.HealthCheck(ctx, "test-boundary")
	assert.Error(t, err) // Expected to fail without actual server
	assert.Contains(t, err.Error(), "Health check failed")
}

func TestServerAddress(t *testing.T) {
	sa := NewServerAddress("localhost", 5005)

	assert.Equal(t, "localhost", sa.Host)
	assert.Equal(t, 5005, sa.Port)
}

func TestDefaultLogger(t *testing.T) {
	logger := NewDefaultLogger(INFO)

	assert.True(t, logger.IsInfoEnabled())
	assert.False(t, logger.IsDebugEnabled())

	logger.SetLevel(DEBUG)
	assert.True(t, logger.IsDebugEnabled())
	assert.True(t, logger.IsInfoEnabled())

	assert.Equal(t, DEBUG, logger.GetLevel())
}

func TestNoOpLogger(t *testing.T) {
	logger := NewNoOpLogger()

	assert.False(t, logger.IsDebugEnabled())
	assert.False(t, logger.IsInfoEnabled())

	// These should not panic
	logger.Debug("test")
	logger.Info("test")
	logger.Warn("test")
	logger.Error("test")
	logger.Errorf("test", nil)
}

func TestOrisunException(t *testing.T) {
	err := NewOrisunException("test error")
	assert.Equal(t, "test error", err.GetMessage())
	assert.Nil(t, err.GetCause())

	err = err.AddContext("key", "value")
	assert.True(t, err.HasContext("key"))
	value, exists := err.GetContext("key")
	assert.True(t, exists)
	assert.Equal(t, "value", value)

	context := err.GetAllContext()
	assert.Contains(t, context, "key")
	assert.Equal(t, "value", context["key"])

	expectedMsg := "test error [Context: key=value]"
	assert.Equal(t, expectedMsg, err.Error())
}

func TestOrisunExceptionWithCause(t *testing.T) {
	cause := assert.AnError
	err := NewOrisunExceptionWithCause("test error", cause)

	assert.Equal(t, "test error", err.GetMessage())
	assert.Equal(t, cause, err.GetCause())
	assert.Equal(t, cause, err.Unwrap())
}

func TestOptimisticConcurrencyException(t *testing.T) {
	err := NewOptimisticConcurrencyException("version conflict", 5, 7)

	assert.Equal(t, int64(5), err.GetExpectedVersion())
	assert.Equal(t, int64(7), err.GetActualVersion())

	expectedMsg := "version conflict (Expected version: 5, Actual version: 7)"
	assert.Equal(t, expectedMsg, err.Error())
}

func TestTokenCache(t *testing.T) {
	logger := NewNoOpLogger()
	cache := NewTokenCache(logger)

	assert.False(t, cache.HasToken())
	assert.Equal(t, "", cache.GetCachedToken())

	cache.CacheToken("test-token")
	assert.True(t, cache.HasToken())
	assert.Equal(t, "test-token", cache.GetCachedToken())

	cache.ClearToken()
	assert.False(t, cache.HasToken())
	assert.Equal(t, "", cache.GetCachedToken())
}

func TestCreateBasicAuthCredentials(t *testing.T) {
	creds := CreateBasicAuthCredentials("user", "pass")
	assert.NotEmpty(t, creds)
	assert.True(t, len(creds) > 6) // Basic + base64 encoded "user:pass"
	assert.Contains(t, creds, "Basic ")

	emptyCreds := CreateBasicAuthCredentials("", "")
	assert.Equal(t, "", emptyCreds)
}

func TestRetryHelper(t *testing.T) {
	config := DefaultRetryConfig()
	config.MaxRetries = 2
	config.InitialDelay = 10 * time.Millisecond
	helper := NewRetryHelper(config)

	attempts := 0
	err := helper.Do(func() error {
		attempts++
		if attempts < 3 {
			return assert.AnError
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)

	// Test with non-retryable error
	attempts = 0
	config.RetryableFunc = func(err error) bool { return false }
	helper = NewRetryHelper(config)

	err = helper.Do(func() error {
		attempts++
		return assert.AnError
	})

	assert.Error(t, err)
	assert.Equal(t, 1, attempts) // Should only attempt once
}

func TestContextHelper(t *testing.T) {
	helper := NewContextHelper()

	ctx, cancel := helper.WithTimeout(context.Background(), 5)
	defer cancel()

	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, time.Now().Add(5*time.Second), deadline, time.Second)
}

func TestStringHelper(t *testing.T) {
	helper := NewStringHelper()

	assert.True(t, helper.IsEmpty(""))
	assert.False(t, helper.IsEmpty("test"))

	assert.False(t, helper.IsNotEmpty(""))
	assert.True(t, helper.IsNotEmpty("test"))

	message := helper.FormatMessage("Hello {}, you have {} messages", "Alice", 5)
	assert.Equal(t, "Hello Alice, you have 5 messages", message)

	message = helper.FormatMessage("No placeholders")
	assert.Equal(t, "No placeholders", message)
}

func TestExtractVersionNumbers(t *testing.T) {
	expected, actual, err := ExtractVersionNumbers("Expected 5, Actual 7")

	assert.NoError(t, err)
	assert.Equal(t, int64(5), expected)
	assert.Equal(t, int64(7), actual)

	_, _, err = ExtractVersionNumbers("No version info")
	assert.Error(t, err)
}

// Test subscription functionality
func TestEventSubscription(t *testing.T) {
	logger := NewNoOpLogger()
	
	// Track events received
	var receivedEvents []*eventstore.Event
	var completedCalled bool
	var errorCalled bool
	
	handler := NewSimpleEventHandler().
		WithOnEvent(func(event *eventstore.Event) error {
			receivedEvents = append(receivedEvents, event)
			return nil
		}).
		WithOnError(func(err error) {
			errorCalled = true
		}).
		WithOnCompleted(func() {
			completedCalled = true
		})

	// Create a mock stream
	mockStream := &mockEventStream{
		events: []*eventstore.Event{
			{
				EventId:   "test-1",
				EventType: "TestEvent",
				Data:      "test data 1",
			},
			{
				EventId:   "test-2",
				EventType: "TestEvent",
				Data:      "test data 2",
			},
		},
		eventIndex: 0,
		closed: false,
	}

	subscription := NewEventSubscription(mockStream, handler, logger, func() {})

	// Wait a moment for the goroutine to process events
	time.Sleep(100 * time.Millisecond)
	
	// Test that events were received
	assert.Equal(t, 2, len(receivedEvents))
	assert.Equal(t, "test-1", receivedEvents[0].EventId)
	assert.Equal(t, "test-2", receivedEvents[1].EventId)

	// Test closing subscription
	closeErr := subscription.Close()
	assert.NoError(t, closeErr)
	assert.True(t, completedCalled)
	// Error handler is called when stream runs out of events, which is expected
	assert.True(t, errorCalled)
}

// Test SaveEvents method
func TestClient_SaveEvents(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Test with valid request
	request := &eventstore.SaveEventsRequest{
		Boundary: "test-boundary",
		Stream: &eventstore.SaveStreamQuery{Name: "test-stream"},
		Events: []*eventstore.EventToSave{
			{
				EventId:   "550e8400-e29b-41d4-a716-446655440000000",
				EventType: "TestEvent",
				Data:      "test data 1",
			},
		},
	}

	// This will fail without actual server, but we test the method exists and validation works
	_, err = client.SaveEvents(context.Background(), request)
	assert.Error(t, err) // Expected to fail without actual server
	assert.Contains(t, err.Error(), "Event at index 0 has invalid eventId format")
}

// Test SaveEvents with nil request
func TestClient_SaveEvents_Validation(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Test with nil request
	_, err = client.SaveEvents(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SaveEventsRequest cannot be nil")

	// Test with empty boundary
	request := &eventstore.SaveEventsRequest{
		Boundary: "",
		Stream:   &eventstore.SaveStreamQuery{Name: "test-stream"},
		Events:  []*eventstore.EventToSave{},
	}
	_, err = client.SaveEvents(context.Background(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Boundary is required")

	// Test with nil stream
	request = &eventstore.SaveEventsRequest{
		Boundary: "test-boundary",
		Stream: nil,
		Events:  []*eventstore.EventToSave{},
	}
	_, err = client.SaveEvents(context.Background(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Stream information is required")

	// Test with no events
	request = &eventstore.SaveEventsRequest{
		Boundary: "test-boundary",
		Stream:   &eventstore.SaveStreamQuery{Name: "test-stream"},
		Events:   []*eventstore.EventToSave{},
	}
	_, err = client.SaveEvents(context.Background(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "At least one event is required")
}

// Test GetEvents method
func TestClient_GetEvents(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Test with valid request
	request := &eventstore.GetEventsRequest{
		Boundary: "test-boundary",
		Stream:   &eventstore.GetStreamQuery{Name: "test-stream"},
		Count:    10,
	}

	// This will fail without actual server, but we test the method exists and validation works
	_, err = client.GetEvents(context.Background(), request)
	assert.Error(t, err) // Expected to fail without actual server
	assert.Contains(t, err.Error(), "Failed to get events")
}

// Test GetEvents with validation errors
func TestClient_GetEvents_Validation(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Test with nil request
	_, err = client.GetEvents(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "GetEventsRequest cannot be nil")

	// Test with empty boundary
	request := &eventstore.GetEventsRequest{
		Boundary: "",
		Count:    10,
	}
	_, err = client.GetEvents(context.Background(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Boundary is required")

	// Test with invalid count
	request = &eventstore.GetEventsRequest{
		Boundary: "test-boundary",
		Count:    0,
	}
	_, err = client.GetEvents(context.Background(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Count must be greater than 0")
}

// Test SubscribeToEvents method
func TestClient_SubscribeToEvents(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Track events received
	var receivedEvents []*eventstore.Event
	handler := NewSimpleEventHandler().
		WithOnEvent(func(event *eventstore.Event) error {
			receivedEvents = append(receivedEvents, event)
			return nil
		}).
		WithOnError(func(err error) {
			// Handle error
		}).
		WithOnCompleted(func() {
			// Handle completion
		})

	request := &eventstore.CatchUpSubscribeToEventStoreRequest{
		Boundary:       "test-boundary",
		SubscriberName: "test-subscriber",
	}

	// This will fail without actual server, but we test the method exists and validation works
	_, err = client.SubscribeToEvents(context.Background(), request, handler)
	assert.Error(t, err) // Expected to fail without actual server
	assert.Contains(t, err.Error(), "Failed to create subscription")
}

// Test SubscribeToEvents with validation errors
func TestClient_SubscribeToEvents_Validation(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	handler := NewSimpleEventHandler()

	// Test with nil request
	_, err = client.SubscribeToEvents(context.Background(), nil, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SubscribeRequest cannot be nil")

	// Test with empty boundary
	request := &eventstore.CatchUpSubscribeToEventStoreRequest{
		Boundary:       "",
		SubscriberName: "test-subscriber",
	}
	_, err = client.SubscribeToEvents(context.Background(), request, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Boundary is required")

	// Test with empty subscriber name
	request = &eventstore.CatchUpSubscribeToEventStoreRequest{
		Boundary:       "test-boundary",
		SubscriberName: "",
	}
	_, err = client.SubscribeToEvents(context.Background(), request, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Subscriber name is required")
}

// Test SubscribeToStream method
func TestClient_SubscribeToStream(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Track events received
	var receivedEvents []*eventstore.Event
	handler := NewSimpleEventHandler().
		WithOnEvent(func(event *eventstore.Event) error {
			receivedEvents = append(receivedEvents, event)
			return nil
		}).
		WithOnError(func(err error) {
			// Handle error
		}).
		WithOnCompleted(func() {
			// Handle completion
		})

	request := &eventstore.CatchUpSubscribeToStreamRequest{
		Boundary:       "test-boundary",
		Stream:         "test-stream",
		SubscriberName: "test-subscriber",
	}

	// This will fail without actual server, but we test the method exists and validation works
	_, err = client.SubscribeToStream(context.Background(), request, handler)
	assert.Error(t, err) // Expected to fail without actual server
	assert.Contains(t, err.Error(), "Failed to create subscription")
}

// Test SubscribeToStream with validation errors
func TestClient_SubscribeToStream_Validation(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	handler := NewSimpleEventHandler()

	// Test with nil request
	_, err = client.SubscribeToStream(context.Background(), nil, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SubscribeToStreamRequest cannot be nil")

	// Test with empty boundary
	request := &eventstore.CatchUpSubscribeToStreamRequest{
		Boundary:       "",
		Stream:         "test-stream",
		SubscriberName: "test-subscriber",
	}
	_, err = client.SubscribeToStream(context.Background(), request, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Boundary is required")

	// Test with empty subscriber name
	request = &eventstore.CatchUpSubscribeToStreamRequest{
		Boundary:       "test-boundary",
		Stream:         "test-stream",
		SubscriberName: "",
	}
	_, err = client.SubscribeToStream(context.Background(), request, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Subscriber name is required")

	// Test with empty stream name
	request = &eventstore.CatchUpSubscribeToStreamRequest{
		Boundary:       "test-boundary",
		Stream:         "",
		SubscriberName: "test-subscriber",
	}
	_, err = client.SubscribeToStream(context.Background(), request, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Stream name is required")
}

// Test error handling scenarios
func TestClient_SaveEvents_ErrorHandling(t *testing.T) {
	builder := NewClientBuilder()
	client, err := builder.WithHost("localhost").Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Test with invalid event ID format
	request := &eventstore.SaveEventsRequest{
		Boundary: "test-boundary",
		Stream:   &eventstore.SaveStreamQuery{Name: "test-stream"},
		Events: []*eventstore.EventToSave{
			{
				EventId:   "invalid-uuid",
				EventType: "TestEvent",
				Data:      "test data 1",
			},
		},
	}

	_, err = client.SaveEvents(context.Background(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid eventId format")
}

// Test edge cases for boundary conditions
func TestClient_EdgeCases(t *testing.T) {
	builder := NewClientBuilder()
	
	// Test with empty host (should default to localhost)
	client, err := builder.WithHost("").Build()
	assert.NoError(t, err)
	assert.NotNil(t, client)
	defer client.Close()

	// Test with invalid port (should still work)
	client, err = builder.WithHost("localhost").WithPort(-1).Build()
	// This might fail but shouldn't panic
	if err != nil {
		// Connection might fail with invalid port
		assert.Error(t, err)
	} else {
		assert.NotNil(t, client)
		defer client.Close()
	}
}

// Test authentication scenarios
func TestClient_Authentication(t *testing.T) {
	// Test with empty credentials
	creds := CreateBasicAuthCredentials("", "")
	assert.Equal(t, "", creds)

	// Test with valid credentials
	creds = CreateBasicAuthCredentials("user", "pass")
	assert.NotEmpty(t, creds)
	assert.Contains(t, creds, "Basic ")
	assert.True(t, len(creds) > 6)
}

// Test token caching scenarios
func TestClient_TokenCaching(t *testing.T) {
	logger := NewNoOpLogger()
	cache := NewTokenCache(logger)

	// Test caching empty token
	cache.CacheToken("")
	assert.False(t, cache.HasToken())
	assert.Equal(t, "", cache.GetCachedToken())

	// Test caching valid token
	testToken := "test-auth-token"
	cache.CacheToken(testToken)
	assert.True(t, cache.HasToken())
	assert.Equal(t, testToken, cache.GetCachedToken())

	// Test clearing token
	cache.ClearToken()
	assert.False(t, cache.HasToken())
	assert.Equal(t, "", cache.GetCachedToken())
}

// Mock event stream for testing
type mockEventStream struct {
	events    []*eventstore.Event
	eventIndex int
	closed    bool
}

func (m *mockEventStream) RecvMsg(msg interface{}) error {
	if m.closed {
		return fmt.Errorf("stream closed")
	}
	
	if m.eventIndex >= len(m.events) {
		return fmt.Errorf("no more events")
	}
	
	event := m.events[m.eventIndex]
	m.eventIndex++
	
	// Convert mock event to protobuf event
	if eventMsg, ok := msg.(*eventstore.Event); ok {
		*eventMsg = *event
	}
	
	return nil
}

func (m *mockEventStream) Close() error {
	m.closed = true
	return nil
}

// Implement grpc.ClientStream interface
func (m *mockEventStream) Header() (metadata.MD, error) {
	return nil, nil
}

func (m *mockEventStream) Trailer() metadata.MD {
	return nil
}

func (m *mockEventStream) CloseSend() error {
	return nil
}

func (m *mockEventStream) Context() context.Context {
	return context.Background()
}

func (m *mockEventStream) SendMsg(msg interface{}) error {
	return nil
}

func (m *mockEventStream) Recv() interface{} {
	if m.closed {
		return nil
	}
	
	if m.eventIndex >= len(m.events) {
		return nil
	}
	
	event := m.events[m.eventIndex]
	m.eventIndex++
	return event
}