package main

import (
	"context"
	"fmt"
	"github.com/goccy/go-json"
	orisun "github.com/oexza/Orisun/clients/go"
	"log"
	"time"

	"github.com/google/uuid"
	eventstore "github.com/oexza/Orisun/clients/go/eventstore"
)

func main() {
	// Create a new client builder
	client, err := orisun.NewClientBuilder().
		WithHost("localhost").
		WithPort(5005).
		WithTimeout(30).
		WithBasicAuth("admin", "changeit").
		Build()

	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Ensure that client is closed when we're done
	defer client.Close()

	// Example 1: Ping
	fmt.Println("Example 1: Pinging server...")
	ctx, cancel := orisun.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		fmt.Printf("Ping failed: %v\n", err)
	} else {
		fmt.Println("Ping successful!")
	}

	// Example 2: Health check
	fmt.Println("\nExample 2: Performing health check...")
	ctx, cancel = orisun.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	healthy, err := client.HealthCheck(ctx, "orisun_admin")
	if err != nil {
		fmt.Printf("Health check failed: %v\n", err)
	} else {
		fmt.Printf("Health check result: %v\n", healthy)
	}

	// Example 3: Save events
	fmt.Println("\nExample 3: Saving events...")
	ctx, cancel = orisun.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create sample events
	eventData := map[string]interface{}{
		"userId":    "12345",
		"action":    "login",
		"timestamp": time.Now().Unix(),
	}
	eventJson, _ := json.Marshal(eventData)

	events := []*eventstore.EventToSave{
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

	saveRequest := &eventstore.SaveEventsRequest{
		Boundary: "orisun_admin",
		Events:   events,
	}

	writeResult, err := client.SaveEvents(ctx, saveRequest)
	if err != nil {
		fmt.Printf("Failed to save events: %v\n", err)
	} else {
		fmt.Printf("Successfully saved events to log position: %+v\n", writeResult.LogPosition)
	}

	// Example 4: Get events
	fmt.Println("\nExample 4: Getting events...")
	ctx, cancel = orisun.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	getRequest := &eventstore.GetEventsRequest{
		Boundary:  "orisun_admin",
		Count:     10,
		Direction: eventstore.Direction_ASC,
	}

	eventsResponse, err := client.GetEvents(ctx, getRequest)
	if err != nil {
		fmt.Printf("Failed to get events: %v\n", err)
	} else {
		fmt.Printf("Retrieved %d events\n", len(eventsResponse.Events))
		for i, event := range eventsResponse.Events {
			fmt.Printf("Event %d: %s - %s\n", i+1, event.EventId, event.EventType)
		}
	}

	// Example 5: Subscribe to events
	fmt.Println("\nExample 5: Subscribing to events...")
	ctx, cancel = orisun.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create event handler
	eventHandler := orisun.NewSimpleEventHandler().
		WithOnEvent(func(event *eventstore.Event) error {
			fmt.Printf("Received event: %s - %s\n", event.EventId, event.EventType)
			return nil
		}).
		WithOnError(func(err error) {
			fmt.Printf("Subscription error: %v\n", err)
		}).
		WithOnCompleted(func() {
			fmt.Println("Subscription completed")
		})

	subscribeRequest := &eventstore.CatchUpSubscribeToEventStoreRequest{
		Boundary:       "orisun_admin",
		SubscriberName: "example-subscriber",
	}

	subscription, err := client.SubscribeToEvents(ctx, subscribeRequest, eventHandler)
	if err != nil {
		fmt.Printf("Failed to subscribe: %v\n", err)
		return
	}

	// Let subscription run for a few seconds
	time.Sleep(5 * time.Second)

	// Close subscription
	if err := subscription.Close(); err != nil {
		fmt.Printf("Error closing subscription: %v\n", err)
	}

	// Example 6: Using retry helper
	fmt.Println("\nExample 6: Using retry helper...")
	retryConfig := orisun.DefaultRetryConfig()
	retryConfig.MaxRetries = 5
	retryConfig.InitialDelay = 100 * time.Millisecond
	retryConfig.MaxDelay = 2 * time.Second

	retryHelper := orisun.NewRetryHelper(retryConfig)

	err = retryHelper.Do(func() error {
		// Simulate a function that might fail
		fmt.Println("Attempting operation...")
		// return fmt.Errorf("simulated failure") // Uncomment to test retry logic
		return nil // Success
	})

	if err != nil {
		fmt.Printf("Operation failed after retries: %v\n", err)
	} else {
		fmt.Println("Operation succeeded!")
	}

	// Example 7: Using string helper
	fmt.Println("\nExample 7: Using string helper...")
	stringHelper := orisun.NewStringHelper()

	message := stringHelper.FormatMessage("Hello {}, you have {} new messages", "Alice", 5)
	fmt.Printf("Formatted message: %s\n", message)

	fmt.Println("\nAll examples completed successfully!")
}
