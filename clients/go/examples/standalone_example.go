package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Mock event types for standalone example
type Event struct {
	EventId   string `json:"eventId"`
	EventType string `json:"eventType"`
	Data      string `json:"data"`
	Metadata  string `json:"metadata"`
}

type EventToSave struct {
	EventId   string `json:"eventId"`
	EventType string `json:"eventType"`
	Data      string `json:"data"`
	Metadata  string `json:"metadata"`
}

type Position struct {
	CommitPosition  int64 `json:"commitPosition"`
	PreparePosition int64 `json:"preparePosition"`
}

type WriteResult struct {
	LogPosition Position `json:"logPosition"`
}

// Simple client interface for standalone example
type SimpleClient interface {
	SaveEvents(ctx context.Context, events []*EventToSave, boundary, stream string) (*WriteResult, error)
	GetEvents(ctx context.Context, boundary, stream string, count int32) ([]*Event, error)
	Ping(ctx context.Context) error
	HealthCheck(ctx context.Context, boundary string) (bool, error)
	Close() error
}

func main() {
	// Create a simple client (in real usage, this would be orisun.NewClientBuilder())
	client := &mockClient{}
	
	// Example 1: Ping
	fmt.Println("Example 1: Pinging server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		fmt.Printf("Ping failed: %v\n", err)
	} else {
		fmt.Println("Ping successful!")
	}

	// Example 2: Health check
	fmt.Println("\nExample 2: Performing health check...")
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	healthy, err := client.HealthCheck(ctx, "orisun_admin")
	if err != nil {
		fmt.Printf("Health check failed: %v\n", err)
	} else {
		fmt.Printf("Health check result: %v\n", healthy)
	}

	// Example 3: Save events
	fmt.Println("\nExample 3: Saving events...")
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create sample events
	eventData := map[string]interface{}{
		"userId": "12345",
		"action": "login",
		"timestamp": time.Now().Unix(),
	}
	eventJson, _ := json.Marshal(eventData)

	events := []*EventToSave{
		{
			EventId:   uuid.New().String(),
			EventType: "UserLoggedIn",
			Data:      string(eventJson),
			Metadata:  `{"source": "web-app"}`,
		},
		{
			EventId:   uuid.New().String(),
			EventType: "UserAction",
			Data:      `{"page": "/dashboard", "action": "view"}`,
			Metadata:  `{"source": "web-app"}`,
		},
	}

	writeResult, err := client.SaveEvents(ctx, events, "orisun_admin", "user-stream")
	if err != nil {
		fmt.Printf("Failed to save events: %v\n", err)
	} else {
		fmt.Printf("Successfully saved events to log position: %+v\n", writeResult.LogPosition)
	}

	// Example 4: Get events
	fmt.Println("\nExample 4: Getting events...")
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := client.GetEvents(ctx, "orisun_admin", "user-stream", 10)
	if err != nil {
		fmt.Printf("Failed to get events: %v\n", err)
	} else {
		fmt.Printf("Retrieved %d events\n", len(events))
		for i, event := range events {
			fmt.Printf("Event %d: %s - %s\n", i+1, event.EventId, event.EventType)
		}
	}

	fmt.Println("\nAll examples completed successfully!")
}

// Mock client implementation
type mockClient struct{}

func (c *mockClient) SaveEvents(ctx context.Context, events []*EventToSave, boundary, stream string) (*WriteResult, error) {
	fmt.Printf("Mock: Saving %d events to stream '%s' in boundary '%s'\n", len(events), stream, boundary)
	return &WriteResult{
		LogPosition: Position{
			CommitPosition:  12345,
			PreparePosition: 12345,
		},
	}, nil
}

func (c *mockClient) GetEvents(ctx context.Context, boundary, stream string, count int32) ([]*Event, error) {
	fmt.Printf("Mock: Getting %d events from stream '%s' in boundary '%s'\n", count, stream, boundary)
	
	events := make([]*Event, count)
	for i := int32(0); i < count; i++ {
		events[i] = &Event{
			EventId:   uuid.New().String(),
			EventType: fmt.Sprintf("Event%d", i),
			Data:      fmt.Sprintf(`{"index": %d}`, i),
			Metadata:  `{"source": "mock"}`,
		}
	}
	
	return events, nil
}

func (c *mockClient) Ping(ctx context.Context) error {
	fmt.Println("Mock: Ping called")
	return nil
}

func (c *mockClient) HealthCheck(ctx context.Context, boundary string) (bool, error) {
	fmt.Printf("Mock: Health check called for boundary '%s'\n", boundary)
	return true, nil
}

func (c *mockClient) Close() error {
	fmt.Println("Mock: Close called")
	return nil
}