package main

import (
	"context"
	"encoding/json"
	"fmt"
	orisun "github.com/oexza/Orisun/clients/go"
	"log"
	"time"

	"github.com/google/uuid"
	eventstore "github.com/oexza/Orisun/clients/go/eventstore"
)

func standaloneExample() {
	// Create an Orisun client
	client, err := orisun.NewClientBuilder().
		WithHost("localhost").
		WithPort(5005).
		WithTimeout(30).
		WithBasicAuth("admin", "changeit").
		Build()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

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
	eventData := map[string]any{
		"userId":    "12345",
		"action":    "login",
		"timestamp": time.Now().Unix(),
	}
	eventJson, _ := json.Marshal(eventData)

	saveRequest := &eventstore.SaveEventsRequest{
		Boundary: "orisun_admin",
		Events: []*eventstore.EventToSave{
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
		},
	}

	writeResult, err := client.SaveEvents(ctx, saveRequest)
	if err != nil {
		fmt.Printf("Failed to save events: %v\n", err)
	} else {
		fmt.Printf("Successfully saved events to log position: %+v\n", writeResult.LogPosition)
	}

	// Example 4: Get events
	fmt.Println("\nExample 4: Getting events...")
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	getRequest := &eventstore.GetEventsRequest{
		Boundary: "orisun_admin",
		Count:    10,
	}

	response, err := client.GetEvents(ctx, getRequest)
	if err != nil {
		fmt.Printf("Failed to get events: %v\n", err)
	} else {
		fmt.Printf("Retrieved %d events\n", len(response.Events))
		for i, event := range response.Events {
			fmt.Printf("Event %d: %s - %s\n", i+1, event.EventId, event.EventType)
		}
	}

	// Example 5: Subscribe to events
	fmt.Println("\nExample 5: Subscribing to events...")
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create event handler
	handler := orisun.NewSimpleEventHandler().
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

	subscription, err := client.SubscribeToEvents(ctx, subscribeRequest, handler)
	if err != nil {
		fmt.Printf("Failed to subscribe to events: %v\n", err)
	} else {
		fmt.Println("Successfully subscribed to events")
		// Let it run for a bit
		time.Sleep(5 * time.Second)
		// Close subscription
		if err := subscription.Close(); err != nil {
			fmt.Printf("Error closing subscription: %v\n", err)
		}
	}

	fmt.Println("\nAll examples completed successfully!")
}

func main() {
	standaloneExample()
}
