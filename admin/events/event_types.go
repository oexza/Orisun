package events

type Event struct {
	EventType string `json:"event_type"`
	Data      any    `json:"data"`
}
